// Package healthhttp is the registry's outbound probe of a plugin frontend.
//
// It is a second, narrower cousin of manifesthttp and stays separate on
// purpose: drift reads a document and compares it, health asks one bounded
// question and accepts one bounded answer. Sharing a client would mean one
// body limit and one timeout serving two jobs with different appetites, and
// the health limit is the small one.
package healthhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

// A status endpoint answers in bytes. The cap is here because this runs every
// five seconds per target, so an origin that answered with megabytes would be
// spending this service's memory on a schedule.
const maxBody = 64 * 1024

type Client struct{ http *http.Client }

func New() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Deployment config names the address. An ambient proxy must not turn
	// that into a different egress route (BR-AS61).
	transport.Proxy = nil
	return &Client{http: &http.Client{
		Transport: transport,
		Timeout:   domain.HealthProbeTimeout,
		// Not followed, and not an error either: the response comes back as
		// the redirect itself, which is not a 200, which is not health. The
		// configured address answers or nothing does.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// healthzResponse is the whole contract (Q7). Small and predictable on
// purpose: anything a plugin might want to add is something the registry
// would then have to decide how to trust.
type healthzResponse struct {
	Status string `json:"status"`
}

// Probe asks once and answers in the closed cause vocabulary. `at` is the
// time the probe was STARTED, which is what the freshness window is measured
// from — stamping it on return would make a slow probe look newer than it is.
func (c *Client) Probe(ctx context.Context, target string, at time.Time) domain.HealthProbe {
	ctx, cancel := context.WithTimeout(ctx, domain.HealthProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return domain.HealthProbeFailed("invalid-target", at)
	}
	req.Header.Set("Accept", "application/json")

	response, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return domain.HealthProbeFailed("timeout", at)
		}
		// Deliberately not err.Error(): a transport error names the host and
		// port that were dialled, and that is deployment topology (BR-AS60).
		return domain.HealthProbeFailed("unreachable", at)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return domain.HealthProbeFailed("http-status", at)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return domain.HealthProbeFailed("timeout", at)
		}
		return domain.HealthProbeFailed("unreachable", at)
	}
	if len(body) > maxBody {
		return domain.HealthProbeFailed("body-too-large", at)
	}

	var parsed healthzResponse
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Status != "ok" {
		// A 200 is not the answer; the agreed word in the agreed field is.
		// An origin serving an index page on every path would otherwise be
		// reported as a healthy plugin.
		return domain.HealthProbeFailed("invalid-response", at)
	}
	return domain.HealthProbeOK(at)
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}
