package announcer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

/*
	The plugin's own health publisher (BR-AS61, BR-AS63, Phase 15 decision
	14). Nobody outside the plugin probes it; this is the only thing that
	looks.

	Split the same way the registry's worker is: every DECISION — whether a
	failure is the second one, what state that makes, what goes on the wire —
	is in a struct that is STEPPED with a `now` a spec supplies, and the only
	thing left in the loop is a ticker. What is not fakeable is the loopback
	GET, and that is a separate seam so the decision specs never open a
	socket.

	The check is a genuine bounded request against this process's own
	/healthz, not a flag the publisher sets about itself. A report that did
	not cost a real request would attest that the publisher is running, which
	is not what is being reported.
*/

// maxHealthBody bounds what the local server can make this process read. It
// is a health endpoint answering a fixed small object; anything larger is a
// misconfiguration or a hostile local process, and neither earns memory.
const maxHealthBody = 4 << 10

// selfCheck returns "" for a healthy check, or one cause from the closed
// vocabulary. It returns a cause rather than an error because "we could not
// tell" is an outcome here, not an exception.
type selfCheck func(context.Context) string

// healthBus is the one thing the reporter needs from a NATS connection.
type healthBus interface {
	Publish(subject string, payload []byte) error
}

type healthReporter struct {
	pluginID string
	subject  string
	check    selfCheck
	bus      healthBus
	log      *slog.Logger

	failures int
	// state is what was last concluded, which is not the same as what the
	// last check returned: one failure after a healthy run stays healthy.
	state string
	cause string
}

func newHealthReporter(pluginID string, check selfCheck, bus healthBus, log *slog.Logger) *healthReporter {
	return &healthReporter{
		pluginID: pluginID,
		subject:  mferegistry.FrontendHealth(pluginID),
		check:    check,
		bus:      bus,
		log:      log,
	}
}

// Step runs one check and publishes the conclusion.
//
// It publishes every step, not only on a change. That is deliberate and it
// is what makes the registry's freshness window a detection mechanism rather
// than a guess: a report that only arrived on a transition would leave a
// healthy, quiet plugin indistinguishable from a dead one.
func (r *healthReporter) Step(ctx context.Context, now time.Time) {
	checkCtx, cancel := context.WithTimeout(ctx, mferegistry.HealthSelfCheckTimeout)
	cause := r.check(checkCtx)
	cancel()

	r.fold(cause)

	payload, err := json.Marshal(mferegistry.HealthReport{
		PluginID: r.pluginID,
		State:    r.state,
		Cause:    r.cause,
		At:       now.UnixMilli(),
	})
	if err != nil {
		// Unreachable with a fixed struct of scalars, but a silent drop here
		// would look exactly like a dead plugin to the registry.
		r.log.Warn("encode health report failed", "plugin", r.pluginID, "error", err)
		return
	}
	if err := r.bus.Publish(r.subject, payload); err != nil {
		// A failed publish is not a reason to stop reporting. The registry
		// will conclude absent on its own, which is the correct reading of a
		// plugin whose bus connection is down.
		r.log.Warn("publish health report failed", "plugin", r.pluginID, "error", err)
	}
}

// fold turns one check's cause into the state that is reported (BR-AS63).
func (r *healthReporter) fold(cause string) {
	if cause == "" {
		r.failures = 0
		r.state, r.cause = mferegistry.HealthReportHealthy, ""
		return
	}
	r.failures++
	if r.failures >= mferegistry.HealthFailureThreshold {
		r.state, r.cause = mferegistry.HealthReportUnhealthy, cause
		return
	}
	// One failure. A plugin that was healthy stays healthy — the registry's
	// freshness window is what stops that being indefinite. A plugin nothing
	// has ever proved healthy does NOT get the benefit of the doubt: a first
	// failure is not evidence of a previous good state, and claiming healthy
	// here would be the one thing this rule exists to prevent.
	if r.state != mferegistry.HealthReportHealthy {
		r.state, r.cause = mferegistry.HealthReportUnhealthy, cause
	}
}

// Run owns the ticker and nothing else. The first report is sent before this
// returns to its caller's first tick, which is what lets the caller publish
// health before it announces.
func (r *healthReporter) Run(ctx context.Context) {
	ticker := time.NewTicker(mferegistry.HealthHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.Step(ctx, now.UTC())
		}
	}
}

// newLoopbackCheck builds the real check against this process's own
// /healthz.
//
// Loopback only, and that is the security property: this is the one outbound
// HTTP capability a plugin has, and confining it to its own address means it
// can never become arbitrary egress or a way to read a service the plugin
// does not own.
func newLoopbackCheck(target string) (selfCheck, error) {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return nil, errors.New("health self-check URL is not a URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("health self-check URL must be HTTP(S)")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("health self-check URL must carry no credentials, query or fragment")
	}
	host := parsed.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("health self-check URL must address loopback")
		}
	}

	client := &http.Client{
		Timeout: mferegistry.HealthSelfCheckTimeout,
		Transport: &http.Transport{
			// No proxy. A proxy variable in the environment must not be able
			// to move a loopback check off the loopback.
			Proxy: nil,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return func(ctx context.Context) string {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return mferegistry.HealthCauseInvalidResponse
		}
		response, err := client.Do(request)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
				return mferegistry.HealthCauseTimeout
			}
			return mferegistry.HealthCauseUnreachable
		}
		defer func() { _, _ = io.Copy(io.Discard, response.Body); _ = response.Body.Close() }()
		if response.StatusCode < 200 || response.StatusCode > 299 {
			return mferegistry.HealthCauseHTTPStatus
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maxHealthBody))
		if err != nil {
			return mferegistry.HealthCauseInvalidResponse
		}
		var decoded struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil || decoded.Status != "ok" {
			return mferegistry.HealthCauseInvalidResponse
		}
		return ""
	}, nil
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
