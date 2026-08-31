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
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/kvcache"
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

// Startup migrates the registry schema and wires the module.
//
// nc may be nil and js may be nil: without a NATS connection the module
// still serves reads and writes, it just has no read cache and announces
// nothing. That degradation is deliberate: it is what lets a store-level test
// compose the module without a broker.
func Startup(ctx context.Context, db *sql.DB, js jetstream.JetStream, nc *nats.Conn, allowlist domain.Allowlist, log *slog.Logger) (*Module, error) {
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
	if nc != nil {
		adapter, err := browserrpc.Mount(nc, browserrpc.New(m.Service, store), log)
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
	return m, nil
}

func (m *Module) Stop() error { return errors.Join(m.adapter.Stop(), m.announcements.Stop()) }
