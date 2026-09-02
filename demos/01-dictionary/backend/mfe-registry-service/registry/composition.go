// Package registry is the application shell's curated frontend plugin
// registry: which micro-frontends the platform will let a shell load, from
// which origins, at which revision.
//
// It was a bounded context inside accounts-service until the split into
// mfe-registry-service. Its schema shared no table and no foreign key with
// accounts (decision 39), which is exactly why the move was a deployment
// change rather than an untangling.
//
// The module's whole interface is Read, Curated and Apply. Revision
// assignment, the origin check, the KV write-through, the audit append and
// the notify publish all happen behind them, because each is only correct
// relative to the revision the write is keyed on (decision 35).
package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/healthnats"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/manifesthttp"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/preload"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/servicerpc"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
)

// Module is the composed registry.
type Module struct {
	Service       *application.Service
	Preload       domain.PreloadResult
	store         *postgres.Store
	adapter       *browserrpc.Adapter
	announcements *servicerpc.Adapter
	driftCancel   context.CancelFunc
	driftDone     chan struct{}
	driftHTTP     *manifesthttp.Client
	healthCancel  context.CancelFunc
	healthDone    chan struct{}
	healthSub     *nats.Subscription
}

// healthPublisher publishes the health plane's snapshot. A tiny adapter rather than
// handing the checker a Notifier, so the checker keeps knowing nothing about
// NATS and a deployment without a broker simply has no publisher (BR-AS64).
type healthPublisher struct{ notifier *natsnotify.Notifier }

func (h healthPublisher) HealthChanged(ctx context.Context, snapshot application.HealthSnapshot) {
	payload, _ := json.Marshal(snapshot) // this closed struct contains no fallible JSON values
	h.notifier.Publish(ctx, notify.HealthChanged(), payload)
}

// MountHTTP mounts the registry's HTTP surface, which is exhaustively empty:
// discovery and curation are api.* subjects and there is no HTTP fallback
// (BR-AS21/AS24). The call exists so a reintroduced route is a test failure.
func MountHTTP(mux *http.ServeMux) []string { return rest.Mount(mux) }

// Subjects is the module's exhaustive browser-facing API surface.
func Subjects() []string { return browserrpc.Subjects() }

// ParseAllowedOrigins reads REGISTRY_ALLOWED_ORIGINS — a comma-separated
// list of scheme://host[:port]. Configuration, never a stored row: the
// allowlist is the envelope the registry itself sits inside, so a compromised
// write path must not be able to widen it (decisions 28, 43).
func ParseAllowedOrigins(raw string) domain.Allowlist {
	return domain.NewAllowlist(strings.Split(raw, ","))
}

// ParseFetchOrigins reads REGISTRY_FETCH_ORIGINS, a JSON object mapping each
// browser origin to its service-reachable origin. Warnings are returned as
// data so parsing stays pure; main logs them once at startup. Invalid config
// checks nothing, and never falls back to the browser's localhost address.
func ParseFetchOrigins(raw string, allowed domain.Allowlist) (domain.FetchOrigins, []string) {
	mappings := map[string]string{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &mappings); err != nil || mappings == nil {
			origins, _ := domain.NewFetchOrigins(allowed, nil)
			return origins, []string{"ignored fetch mappings: expected a JSON object of browser origins to service origins"}
		}
	}
	return domain.NewFetchOrigins(allowed, mappings)
}

// REGISTRY_HEALTH_ORIGINS is gone (Phase 15c). It told the registry which
// address to DIAL for each plugin's frontend, and nothing dials one any more:
// a plugin reports itself on a subject derived from its own id. Its sibling
// REGISTRY_FETCH_ORIGINS is untouched — that one serves the manifest drift
// check (BR-AS45), which really does fetch, and the two were always read
// separately.

// ParseHealthTargets reads REGISTRY_HEALTH_TARGETS, a JSON object mapping a
// plugin id to the backend service ids it depends on. An absent plugin is not
// configured; a plugin mapped to an empty list is frontend-only. Both are
// states, not health (BR-AS62).
func ParseHealthTargets(raw string) (domain.HealthTargets, []string) {
	mappings := map[string][]string{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &mappings); err != nil || mappings == nil {
			targets, _ := domain.NewHealthTargets(nil)
			return targets, []string{"ignored health targets: expected a JSON object of plugin ids to service ids"}
		}
	}
	return domain.NewHealthTargets(mappings)
}

// Startup migrates the registry schema and wires the module.
//
// nc may be nil and js may be nil: without a NATS connection the module
// still serves reads and writes, it just has no read cache and announces
// nothing. That degradation is deliberate: it is what lets a store-level test
// compose the module without a broker.
func Startup(lifetime context.Context, db *sql.DB, js jetstream.JetStream, nc *nats.Conn, allowlist domain.Allowlist, log *slog.Logger, fetchOrigins ...domain.FetchOrigins) (*Module, error) {
	// Startup is bounded separately from the worker's lifetime. Passing a
	// startup deadline to Run would quietly stop checks after one minute.
	ctx, cancel := context.WithTimeout(lifetime, 60*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, db); err != nil {
		return nil, err
	}
	store := postgres.NewStore(db, allowlist)

	var cache application.Cache
	if js != nil {
		kv := kvcache.New(js)
		if err := kv.Ensure(ctx); err != nil {
			// A bucket that cannot be provisioned costs the fallback, not
			// the service.
			log.Warn("registry: read cache unavailable", "error", err)
		} else {
			cache = kv
		}
	}

	notifier := natsnotify.New(nc, log)
	m := &Module{Service: application.New(store, cache, allowlist, notifier, log), store: store}
	var err error
	m.Preload, err = preload.Run(ctx, os.Getenv("REGISTRY_PRELOAD_FILE"), m.Service, log)
	if err != nil {
		return nil, err
	}
	origins, _ := domain.NewFetchOrigins(allowlist, nil)
	if len(fetchOrigins) > 0 {
		origins = fetchOrigins[0]
	}
	healthTargets, targetWarnings := ParseHealthTargets(os.Getenv("REGISTRY_HEALTH_TARGETS"))
	for _, w := range targetWarnings {
		log.Warn("registry health: " + w)
	}
	// What is still deployment-owned is the BACKEND service ids: they come
	// from configuration and never from a manifest, so no publisher can point
	// the registry at a service it does not own (BR-AS62/AS65). The frontend
	// side is not configured at all any more — there is nothing to point.
	var health *application.HealthChecker
	if nc != nil {
		health = application.NewHealthChecker(m.Service, healthTargets, healthnats.New(nc), healthPublisher{notifier: notifier})
	} else {
		health = application.NewHealthChecker(m.Service, healthTargets, healthnats.New(nil))
	}

	m.driftHTTP = manifesthttp.New()
	checker := application.NewDriftChecker(m.Service, origins, m.driftHTTP, application.DriftSchedule{})
	// The operator view is joined in the application layer, so the checker is
	// wired to the service rather than to a transport.
	m.Service.WithDrift(checker)
	if nc != nil {
		adapter, err := browserrpc.Mount(nc, browserrpc.NewWithHealth(m.Service, store, health), log)
		if err != nil {
			return nil, err
		}
		m.adapter = adapter
		// Real verification from Phase 7c. The trust anchor is the operator's
		// publisher table, not this line — an empty table refuses everything,
		// which is the same fail-closed behaviour NoVerifier gave, arrived at
		// by policy instead of by placeholder.
		m.announcements, err = servicerpc.Mount(nc, servicerpc.New(m.Service, store, domain.NKeyVerifier{}), log)
		if err != nil {
			_ = m.adapter.Stop()
			return nil, err
		}
	}
	workerCtx, workerCancel := context.WithCancel(lifetime)
	m.driftCancel, m.driftDone = workerCancel, make(chan struct{})
	go func() { defer close(m.driftDone); checker.Run(workerCtx) }()

	if nc != nil {
		// One subscription for every plugin at once. It is started before the
		// checker runs, so a report that arrives during start-up is kept
		// rather than dropped into a window where nothing was listening.
		sub, err := healthnats.SubscribeFrontend(nc, health, log)
		if err != nil {
			return nil, err
		}
		m.healthSub = sub
	}

	healthCtx, healthCancel := context.WithCancel(lifetime)
	m.healthCancel, m.healthDone = healthCancel, make(chan struct{})
	go func() { defer close(m.healthDone); health.Run(healthCtx) }()
	return m, nil
}

func (m *Module) Stop() error {
	if m.driftCancel != nil {
		m.driftCancel()
		<-m.driftDone
		m.driftHTTP.Close()
	}
	if m.healthCancel != nil {
		m.healthCancel()
		<-m.healthDone
	}
	if m.healthSub != nil {
		_ = m.healthSub.Unsubscribe()
	}
	return errors.Join(m.adapter.Stop(), m.announcements.Stop())
}
