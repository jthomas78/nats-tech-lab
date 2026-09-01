package application

import (
	"context"
	"sync"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

/*
	The health checker is the runner the domain's stepped worker was written
	for. Everything with a decision in it — when a target is due, whether a
	failure is the second one, whether a reading has gone stale — is in
	domain.HealthWorker and is driven by a `now` that a spec supplies. What is
	left here is the parts that cannot be faked: a ticker, two adapters, and a
	mutex.

	It reads the catalogue and never writes it (BR-AS65). The only thing that
	crosses back is a Snapshot the transport decorates a reply with, so a probe
	cannot move a revision, touch signed bytes, change approval or add an audit
	row — the code to do any of that is not reachable from here.
*/

// HealthCatalog is the read the checker needs and no more.
type HealthCatalog interface {
	Curated(context.Context) (domain.Document, error)
}

// FrontendProber and BackendProber are the two adapters. Both return a probe
// rather than an error: "we could not tell" is an outcome with a cause, not
// an exception, and every path through them ends in the same closed cause
// vocabulary.
type FrontendProber interface {
	Probe(ctx context.Context, target string, at time.Time) domain.HealthProbe
}

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

// HealthHinter is told that a snapshot moved. It is handed no state on
// purpose (BR-AS64): a hint means "read the health subject again", and a
// delivery is proof that a message arrived, never proof that a service was
// alive.
type HealthHinter interface {
	HealthChanged(ctx context.Context)
}

type HealthChecker struct {
	catalog  HealthCatalog
	origins  domain.HealthOrigins
	targets  domain.HealthTargets
	frontend FrontendProber
	backend  BackendProber

	mu       sync.RWMutex
	worker   *domain.HealthWorker
	plugins  map[string][]string // plugin id -> backend service ids, nil when unmapped
	mapped   map[string]string   // plugin id -> frontend target, "" when unmapped
	revision int64

	hint HealthHinter
	// published is the last snapshot a hint was sent for, flattened to the
	// facts a shell would notice. Kept so a pass where nothing moved says
	// nothing: a hint every five seconds would train every shell to ignore
	// the one that mattered.
	published map[string]string
}

// NewHealthChecker takes the hinter last and optionally: a deployment with no
// broker still probes and still answers reads, it just tells nobody.
func NewHealthChecker(catalog HealthCatalog, origins domain.HealthOrigins, targets domain.HealthTargets, frontend FrontendProber, backend BackendProber, hint ...HealthHinter) *HealthChecker {
	c := &HealthChecker{
		catalog:  catalog,
		origins:  origins,
		targets:  targets,
		frontend: frontend,
		backend:  backend,
		worker:   domain.NewHealthWorker(nil),
		plugins:  map[string][]string{},
		mapped:   map[string]string{},
	}
	if len(hint) > 0 {
		c.hint = hint[0]
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
		health := PluginHealth{Frontend: domain.HealthSignal{State: domain.HealthNotConfigured}}
		if target := c.mapped[id]; target != "" {
			health.Frontend = observed[frontendKey(id)]
		}
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
// pass — due, threshold, freshness, whether to hint — belongs to a spec that
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

// announce sends at most one hint per pass, and only when a shell would see a
// different answer than last time. Comparing the SERVED snapshot rather than
// the raw probes is deliberate: two failures that have not yet crossed the
// threshold are the same news as none, and a shell asked to re-read for them
// would learn nothing.
func (c *HealthChecker) announce(ctx context.Context, now time.Time) {
	if c.hint == nil {
		return
	}
	current := map[string]string{}
	for id, health := range c.Snapshot(now) {
		current[id] = health.Frontend.State + "/" + health.Frontend.Cause + "|" + health.Backend.State + "/" + health.Backend.Cause
	}

	c.mu.Lock()
	changed := len(current) != len(c.published)
	for id, state := range current {
		if c.published[id] != state {
			changed = true
		}
	}
	c.published = current
	c.mu.Unlock()

	if changed {
		c.hint.HealthChanged(ctx)
	}
}

func (c *HealthChecker) run(ctx context.Context, key string, at time.Time) domain.HealthProbe {
	if id, ok := frontendOf(key); ok {
		c.mu.RLock()
		target := c.mapped[id]
		c.mu.RUnlock()
		if target == "" {
			return domain.HealthProbeFailed("not-configured", at)
		}
		return c.frontend.Probe(ctx, target, at)
	}
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
	mapped := map[string]string{}
	keys := []string{}
	for _, entry := range doc.Entries {
		if entry.Withheld || !entry.Enabled {
			continue
		}
		target, _ := c.origins.Target(entry)
		mapped[entry.ID] = target
		if target != "" {
			keys = append(keys, frontendKey(entry.ID))
		}
		services := c.targets.Dependencies(entry.ID)
		plugins[entry.ID] = services
		for _, service := range services {
			keys = append(keys, backendKey(service))
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.plugins, c.mapped = plugins, mapped
	c.worker.Watch(keys)
}

func (c *HealthChecker) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.worker.Stop()
}

/*
The worker keys frontend and backend probes in one namespace, prefixed so
they cannot collide: a plugin id and a service id are different vocabularies
chosen by different people, and a plugin called "shipping-service" must not
share a failure count with the service of that name.
*/
func frontendKey(pluginID string) string { return "frontend:" + pluginID }
func backendKey(serviceID string) string { return "backend:" + serviceID }

func frontendOf(key string) (string, bool) {
	if len(key) > 9 && key[:9] == "frontend:" {
		return key[9:], true
	}
	return "", false
}

func backendOf(key string) string {
	if len(key) > 8 && key[:8] == "backend:" {
		return key[8:]
	}
	return key
}
