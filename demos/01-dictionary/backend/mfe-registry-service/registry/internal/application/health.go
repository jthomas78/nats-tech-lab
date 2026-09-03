package application

import (
	"context"
	"sync"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

/*
	The health checker joins the two planes health arrives on, which since
	Phase 15 decision 14 are not the same shape at all.

	BACKEND readiness is still PULLED: domain.HealthWorker decides when a
	service is due and whether a failure is the second one, and this runner
	turns those decisions into request/reply probes (BR-AS62).

	FRONTEND health is PUSHED and is never asked for. A plugin checks itself
	and publishes; a subscriber hands the report to domain.HealthInbox; and
	the only thing left for a pass to do is read it. There is no frontend
	prober, no map of origins, and no outbound HTTP anywhere in this service.

	Everything with a decision in it still lives in the domain and is driven
	by a `now` a spec supplies. What is left here is what cannot be faked: a
	ticker, one adapter, and a mutex.

	It reads the catalogue and never writes it (BR-AS65). The only thing that
	crosses back is a Snapshot the transport decorates a reply with, so a probe
	cannot move a revision, touch signed bytes, change approval or add an audit
	row — the code to do any of that is not reachable from here.
*/

// HealthCatalog is the read the checker needs and no more.
type HealthCatalog interface {
	Curated(context.Context) (domain.Document, error)
}

// BackendProber is the one remaining adapter. It returns a probe rather than
// an error: "we could not tell" is an outcome with a cause, not an exception,
// and every path through it ends in the same closed cause vocabulary.
type BackendProber interface {
	Probe(ctx context.Context, serviceID string, at time.Time) domain.HealthProbe
}

// PluginHealth is one plugin's two decorations. Separate fields because they
// are separate facts (BR-AS60): a plugin whose UI is served fine by a CDN
// while its API is down is exactly the case an operator needs to see, and one
// merged verdict would hide it.
type PluginHealth struct {
	Frontend domain.HealthSignal `json:"frontend"`
	Backend  domain.HealthSignal `json:"backend"`
}

// HealthSnapshot is the one browser-facing shape used by both the initial
// request and the pushed observation stream. It contains health and nothing
// from the catalogue: no revision, entries, signed bytes or approval state.
type HealthSnapshot struct {
	OK      bool                    `json:"ok"`
	Plugins map[string]PluginHealth `json:"plugins"`
	// AsOf is milliseconds since the epoch. Millisecond precision lets a
	// browser reject a duplicate push without refreshing its freshness lease.
	AsOf int64 `json:"asOf"`
}

// HealthPublisher publishes completed observations. Core NATS is not durable,
// so the shell still takes an initial/reconnect snapshot; between those reads
// this one central publication keeps every browser current without one poll
// per browser every five seconds (BR-AS64/AS65).
type HealthPublisher interface {
	HealthChanged(ctx context.Context, snapshot HealthSnapshot)
}

type HealthChecker struct {
	catalog HealthCatalog
	backend BackendProber

	mu      sync.RWMutex
	worker  *domain.HealthWorker
	inbox   *domain.HealthInbox
	plugins map[string][]string // plugin id -> backend service ids, nil when unmapped

	publisher HealthPublisher
}

// NewHealthChecker takes the publisher last and optionally: a deployment with
// no broker still probes and still answers reads, it just publishes nothing.
//
// There is no target map argument any more. What to probe is a property of
// each catalogue entry — declared by the plugin, approved by an operator
// (BR-AS62) — so it is re-read from the catalogue on every pass along with
// which plugins exist at all, and a deployment has nothing to be told.
func NewHealthChecker(catalog HealthCatalog, backend BackendProber, publisher ...HealthPublisher) *HealthChecker {
	c := &HealthChecker{
		catalog: catalog,
		backend: backend,
		worker:  domain.NewHealthWorker(nil),
		inbox:   domain.NewHealthInbox(),
		plugins: map[string][]string{},
	}
	if len(publisher) > 0 {
		c.publisher = publisher[0]
	}
	return c
}

// Snapshot is every plugin's pair of signals as of now. Read-only and cheap:
// it touches memory, so a browser read never waits on a probe (BR-AS65).
func (c *HealthChecker) Snapshot(now time.Time) map[string]PluginHealth {
	c.mu.RLock()
	defer c.mu.RUnlock()

	observed := c.worker.Snapshot(now)
	out := map[string]PluginHealth{}
	for id, services := range c.plugins {
		// Frontend health is whatever the plugin last said about itself,
		// aged against the freshness window. There is no "not configured"
		// frontend any more: nothing has to be told how to reach a plugin, so
		// nothing can be left out of a map. A plugin nothing has been heard
		// from is UNKNOWN, which is a true statement about a plugin that may
		// simply be starting.
		health := PluginHealth{Frontend: c.inbox.Signal(id, now)}
		if services == nil {
			health.Backend = domain.SummarizeBackend(nil)
		} else {
			signals := make([]domain.HealthSignal, 0, len(services))
			for _, service := range services {
				signals = append(signals, observed[backendKey(service)])
			}
			health.Backend = domain.SummarizeBackend(signals)
		}
		out[id] = health
	}
	return out
}

// Run owns the ticker and nothing else. One pass per tick, and a pass only
// starts probes the worker offers — which is what keeps probes from
// overlapping without this function having to remember anything.
func (c *HealthChecker) Run(ctx context.Context) {
	defer c.stop()
	ticker := time.NewTicker(domain.HealthProbeInterval)
	defer ticker.Stop()
	c.Step(ctx, time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			c.Step(ctx, now.UTC())
		}
	}
}

// Step is one pass, at a time the caller supplies. Exported and clock-free
// for the same reason the domain worker is (BR-AS63): every decision in a
// pass — due, threshold, freshness and the served snapshot — belongs to a spec that
// can state the time, and the only thing left in Run is the ticker.
func (c *HealthChecker) Step(ctx context.Context, now time.Time) {
	c.refreshTargets(ctx)

	c.mu.Lock()
	due := c.worker.Due(now)
	c.mu.Unlock()

	// Probes run concurrently and are joined before the pass ends, so a
	// shutdown cannot leave one writing into a stopped worker (BR-AS63).
	var wait sync.WaitGroup
	for _, key := range due {
		wait.Add(1)
		go func(key string) {
			defer wait.Done()
			probe := c.run(ctx, key, now)
			c.mu.Lock()
			c.worker.Record(key, probe)
			c.mu.Unlock()
		}(key)
	}
	wait.Wait()

	c.announce(ctx, now)
}

// announce sends one full snapshot after a completed pass. A transition-only
// hint is not enough: successful probes advance LastCheckAt even when their
// state stays healthy, and without that observation a browser would correctly
// age the last pushed value to stale after fifteen seconds. One broadcast per
// pass replaces one request per browser per pass.
func (c *HealthChecker) announce(ctx context.Context, now time.Time) {
	if c.publisher == nil {
		return
	}
	c.publisher.HealthChanged(ctx, HealthSnapshot{
		OK:      true,
		Plugins: c.Snapshot(now),
		AsOf:    now.UnixMilli(),
	})
}

// AcceptFrontendReport hands one pushed report to the inbox and says whether
// it was believed. It is the ONLY way frontend health enters this service,
// and the transport that calls it holds no decision of its own: what to
// believe is the domain's business (BR-AS61).
func (c *HealthChecker) AcceptFrontendReport(subjectPluginID string, report mferegistry.HealthReport, at time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inbox.Accept(subjectPluginID, report, at)
}

func (c *HealthChecker) run(ctx context.Context, key string, at time.Time) domain.HealthProbe {
	return c.backend.Probe(ctx, backendOf(key), at)
}

// refreshTargets re-reads the catalogue so a curated plugin starts being
// watched and a removed one stops. Rebuilding the worker would throw away
// every failure count on every pass, so the worker keeps the targets it has
// and only genuinely new ones are added.
func (c *HealthChecker) refreshTargets(ctx context.Context) {
	readCtx, cancel := context.WithTimeout(ctx, domain.HealthProbeTimeout)
	doc, err := c.catalog.Curated(readCtx)
	cancel()
	if err != nil {
		// A catalogue read that failed says nothing about any plugin's
		// health. Keep watching what we were watching; the freshness window
		// is what stops the readings looking current forever.
		return
	}

	plugins := map[string][]string{}
	keys := []string{}
	for _, entry := range doc.Entries {
		if entry.Withheld || !entry.Enabled {
			continue
		}
		services := entry.EffectiveBackendServices()
		plugins[entry.ID] = services
		for _, service := range services {
			keys = append(keys, backendKey(service))
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.plugins = plugins
	c.worker.Watch(keys)
	// A reading must never outlive the plugin it is about (BR-AS65). The
	// worker drops backend targets it is no longer watching; the inbox is
	// swept here for the same reason, so a removed plugin cannot come back
	// green if it is re-added later.
	keep := make(map[string]bool, len(plugins))
	for id := range plugins {
		keep[id] = true
	}
	c.inbox.Forget(keep)
}

func (c *HealthChecker) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.worker.Stop()
}

/*
The worker's keys stay prefixed even though only backend probes use them
now. The prefix costs nothing, and dropping it would silently give a service
id and a plugin id one namespace again the day anything else is scheduled
here — a plugin called "shipping-service" must never share a failure count
with the service of that name.
*/
func backendKey(serviceID string) string { return "backend:" + serviceID }

func backendOf(key string) string {
	if len(key) > 8 && key[:8] == "backend:" {
		return key[8:]
	}
	return key
}
