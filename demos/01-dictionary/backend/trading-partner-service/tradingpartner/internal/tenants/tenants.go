// Package tenants wires trading-partner-service's per-tenant resource — a
// refdataclient.Client for BR-TP14 validation, plus a browserrpc.Adapter
// mounted in a second pass — onto shared/natstenants' connection-lifecycle
// machinery (Phase 35). The two-pass wiring exists because
// composition.Startup needs this package's Manager to satisfy
// domain.VehicleTypeValidator before the command handlers it needs for the
// adapter exist yet: connections come up first (MountTenants, so refdata
// validation works), then MountAPI backfills adapters onto them, using
// shared/natstenants.Manager's Update method for tenants already connected
// and a captured apiDeps pointer (read by the provision callback) for any
// tenant that connects afterward.
package tenants

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/refdataclient"
	"github.com/jthomas78/nats-tech-lab/shared/natstenants"
)

// resource is one tenant's connection-scoped state. adapter is nil until
// MountAPI has run for that tenant — see Manager's doc comment.
type resource struct {
	client  *refdataclient.Client
	adapter *browserrpc.Adapter
}

// Manager implements domain.VehicleTypeValidator directly (BR-TP14) — it's
// the natural owner of "which tenant's connection to use," so there's no
// separate wrapper type.
var _ domain.VehicleTypeValidator = (*Manager)(nil)

// Manager wraps shared/natstenants.Manager with this service's resource
// shape and two-pass adapter wiring.
type Manager struct {
	mgr *natstenants.Manager[*resource]

	mu      sync.RWMutex
	apiDeps *browserrpc.Deps // nil until MountAPI has run
}

func NewManager(natsURL, credsDir string, log *slog.Logger) *Manager {
	m := &Manager{}
	m.mgr = natstenants.NewManager(natsURL, credsDir, "trading-partner-service", log,
		func(_ context.Context, nc *nats.Conn, tenant string) (*resource, error) {
			res := &resource{client: refdataclient.New(nc)}
			m.mu.RLock()
			deps := m.apiDeps
			m.mu.RUnlock()
			// Only if MountAPI has already run — a tenant connecting before
			// that carries no api.* adapter until MountAPI's Update pass
			// backfills it below.
			if deps != nil {
				scoped := *deps
				scoped.Tenant = tenant
				adapter, err := browserrpc.New(nc, scoped)
				if err != nil {
					return nil, err
				}
				res.adapter = adapter
			}
			return res, nil
		},
		func(_ string, res *resource) error {
			if res.adapter != nil {
				return res.adapter.Stop()
			}
			return nil
		},
	)
	return m
}

// EnsureAll connects to every tenant currently discoverable in credsDir —
// see shared/natstenants.Manager.EnsureAll.
func (m *Manager) EnsureAll(ctx context.Context) error {
	return m.mgr.EnsureAll(ctx)
}

// ErrTenantNotConnected is returned when Exists is called for a tenant this
// Manager has no live connection for (an unknown or not-yet-discovered
// .creds file).
var ErrTenantNotConnected = fmt.Errorf("tenant is not connected")

// Exists implements domain.VehicleTypeValidator (BR-TP14) by resolving
// tenant's own connection and delegating to its refdataclient.Client.
func (m *Manager) Exists(ctx context.Context, tenant, contextKey, code string) (bool, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return false, ErrTenantNotConnected
	}
	return res.client.Exists(ctx, contextKey, code)
}

// MountAPI registers the api.* adapter on every currently-connected tenant,
// and arms every tenant that connects afterward to get one too (the
// provision closure above reads apiDeps). Called once, after
// composition.Startup has built the command handlers deps carries.
// deps.Tenant is ignored — it's filled in per connection.
func (m *Manager) MountAPI(deps browserrpc.Deps) error {
	m.mu.Lock()
	m.apiDeps = &deps
	m.mu.Unlock()

	var firstErr error
	m.mgr.Range(func(tenant string, _ *nats.Conn, res *resource) {
		if res.adapter != nil {
			return
		}
		err := m.mgr.Update(tenant, func(nc *nats.Conn, cur *resource) (*resource, error) {
			if cur.adapter != nil {
				return cur, nil
			}
			scoped := deps
			scoped.Tenant = tenant
			adapter, err := browserrpc.New(nc, scoped)
			if err != nil {
				return cur, err
			}
			cur.adapter = adapter
			return cur, nil
		})
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("mount api adapter for tenant %q: %w", tenant, err)
		}
	})
	return firstErr
}

// Close closes every tenant's connection — called on process shutdown.
func (m *Manager) Close() {
	m.mgr.Close()
}
