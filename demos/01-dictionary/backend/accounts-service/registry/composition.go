// Package registry is the application shell's curated frontend plugin
// registry: which micro-frontends the platform will let a shell load, from
// which origins, at which revision.
//
// It is its own bounded context inside accounts-service, not part of the
// accounts module: its schema shares no table and no foreign key with
// accounts (decision 39), so the day it wants its own service the move is a
// deployment change rather than an untangling.
//
// The module's whole interface is Read, Curated and Apply. Revision
// assignment, the origin check, the KV write-through, the audit append and
// the notify publish all happen behind them, because each is only correct
// relative to the revision the write is keyed on (decision 35).
package registry

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
)

// Module is the composed registry.
type Module struct {
	Service *application.Service
	store   *postgres.Store
	adapter *browserrpc.Adapter
}

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
// nothing. That degradation is deliberate — accounts-service's own platform
// connection is already optional.
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
	if nc != nil {
		adapter, err := browserrpc.Mount(nc, browserrpc.New(m.Service, store), log)
		if err != nil {
			return nil, err
		}
		m.adapter = adapter
	}
	return m, nil
}

func (m *Module) Stop() error { return m.adapter.Stop() }
