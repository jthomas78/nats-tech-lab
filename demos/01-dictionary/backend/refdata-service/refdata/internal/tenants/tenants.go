// Package tenants manages one NATS connection per provisioned tenant so
// refdata-service's internal/browserrpc.Adapter can answer api.* calls on
// every known tenant's account (BR-D40) — copied from pricing-service's
// internal/tenants (and trading-partner-service's own copy) rather than
// inventing a new shape. This is knowingly the fourth copy of this
// connection-manager pattern; extraction into a shared natstenants package
// remains blocked on this repo's lack of a go.work across its 7 Go modules
// (see BR-D40's doc comment in BUSINESS_RULES-REFDATA.md).
//
// Unlike those two services, refdata-service also keeps its single,
// permanent PLATFORM connection running unchanged (internal/natsrpc's rpc.*
// adapter) — this package only adds the per-tenant connections needed for
// the new api.* surface; it does not replace or touch the PLATFORM
// connection cmd/main.go already manages.
package tenants

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natstrace"
)

// nonTenantCredsFiles mirrors pricing-service's and trading-partner-service's
// own list — these .creds stems in the shared creds directory are never
// tenants. Included from this package's first commit (BR-D40) rather than
// left to be independently rediscovered a fourth time: its omission is the
// bug that has already cost three separate fixes across the other services
// (see ARCHITECTURE-ACCOUNTS.md § "Three services now open per-tenant
// connections").
var nonTenantCredsFiles = map[string]bool{"platform": true, "shipping-admin": true, "sys": true, "observability": true}

// Credentials is one discovered tenant's creds file path.
type Credentials struct {
	CredsPath string
}

// Discover scans credsDir for *.creds files and returns the known-tenant
// map. Re-scanned on every call rather than cached — seeing a just-minted or
// just-suspended tenant immediately matters more than avoiding a few stat
// calls, and the directory is small.
func Discover(credsDir string) (map[string]Credentials, error) {
	entries, err := os.ReadDir(credsDir)
	if err != nil {
		return nil, fmt.Errorf("scan creds dir %q: %w", credsDir, err)
	}
	out := make(map[string]Credentials)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".creds") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".creds")
		if nonTenantCredsFiles[strings.ToLower(name)] {
			continue
		}
		out[name] = Credentials{CredsPath: filepath.Join(credsDir, e.Name())}
	}
	return out, nil
}

type resources struct {
	nc      *nats.Conn
	adapter *browserrpc.Adapter
}

// Manager holds one persistent NATS connection + browserrpc.Adapter per
// known tenant. deps are the shared command handlers — identical across
// every tenant's Adapter (see browserrpc's package doc comment); only
// deps.Tenant is overwritten per connection.
type Manager struct {
	natsURL  string
	credsDir string
	log      *slog.Logger
	deps     browserrpc.Deps

	mu        sync.RWMutex
	resources map[string]*resources
}

func NewManager(natsURL, credsDir string, log *slog.Logger, deps browserrpc.Deps) *Manager {
	return &Manager{
		natsURL:   natsURL,
		credsDir:  credsDir,
		log:       log,
		deps:      deps,
		resources: make(map[string]*resources),
	}
}

// EnsureAll creates a connection+adapter for every tenant currently
// discoverable in credsDir that doesn't already have one — called once at
// Startup so every tenant present at boot gets working api.* support
// immediately. Failures are logged and skipped per-tenant rather than
// aborting Startup: one tenant's bad creds file shouldn't prevent every
// other tenant, or the service itself, from coming up.
func (m *Manager) EnsureAll(ctx context.Context) error {
	known, err := Discover(m.credsDir)
	if err != nil {
		return err
	}
	for tenant, creds := range known {
		if _, err := m.ensure(ctx, tenant, creds.CredsPath); err != nil && m.log != nil {
			m.log.Error("ensure tenant resources at startup", "tenant", tenant, "err", err)
		}
	}
	return nil
}

// EnsureByName reactively provisions a single tenant's api.* adapter the
// moment accounts-service mints it — a no-op, not an error, if the tenant
// isn't yet visible in credsDir (the creds file write happens before the
// notify publish, so this shouldn't race it in practice, but staying
// defensive means a stray/duplicate delivery can't fail loudly).
func (m *Manager) EnsureByName(ctx context.Context, tenant string) error {
	known, err := Discover(m.credsDir)
	if err != nil {
		return err
	}
	creds, ok := known[tenant]
	if !ok {
		return nil
	}
	_, err = m.ensure(ctx, tenant, creds.CredsPath)
	return err
}

// TeardownByName closes tenant's connection — stopping its browserrpc
// adapter and disabling nats.go's default reconnect loop against a .creds
// file accounts-service has already deleted. A no-op if tenant was never
// provisioned or has already been torn down.
func (m *Manager) TeardownByName(_ context.Context, tenant string) error {
	m.mu.Lock()
	res, ok := m.resources[tenant]
	if ok {
		delete(m.resources, tenant)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if err := res.adapter.Stop(); err != nil && m.log != nil {
		m.log.Error("stop browserrpc adapter on teardown", "tenant", tenant, "err", err)
	}
	res.nc.Close()
	return nil
}

// ensure returns tenant's persistent connection+adapter, creating it on
// first sight. Idempotent: a tenant already present is returned as-is — no
// reconnect, no re-registration.
func (m *Manager) ensure(ctx context.Context, tenant, credsPath string) (*resources, error) {
	m.mu.RLock()
	res, ok := m.resources[tenant]
	m.mu.RUnlock()
	if ok {
		return res, nil
	}

	// Same nats.Name("refdata-service") as the PLATFORM connection
	// (cmd/main.go) — both are the same service; a caller distinguishes them
	// by which account answered, not by name.
	nc, err := nats.Connect(m.natsURL, nats.Name("refdata-service"), nats.UserCredentials(credsPath))
	if err != nil {
		return nil, fmt.Errorf("connect as tenant %q: %w", tenant, err)
	}

	deps := m.deps
	deps.Tenant = tenant
	adapter, err := browserrpc.New(nc, deps)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("register browserrpc adapter for tenant %q: %w", tenant, err)
	}

	if err := m.subscribeLifecycle(ctx, nc); err != nil {
		_ = adapter.Stop()
		nc.Close()
		return nil, fmt.Errorf("subscribe tenant lifecycle for tenant %q: %w", tenant, err)
	}

	candidate := &resources{nc: nc, adapter: adapter}

	m.mu.Lock()
	// Re-check under the write lock — a concurrent ensure() for the same
	// tenant (EnsureAll racing a lifecycle event, e.g.) may have won first.
	if existing, ok := m.resources[tenant]; ok {
		m.mu.Unlock()
		_ = adapter.Stop()
		nc.Close()
		return existing, nil
	}
	m.resources[tenant] = candidate
	m.mu.Unlock()

	return candidate, nil
}

// subscribeLifecycle mirrors pricing-service's own subscribeLifecycle: each
// tenant connection observes accounts-service's imported PLATFORM lifecycle
// events directly, rather than refdata-service holding a second, separate
// PLATFORM-only creds file just for this — keeping the cross-account path
// entirely in accounts-service's declared imports.
func (m *Manager) subscribeLifecycle(ctx context.Context, nc *nats.Conn) error {
	ctx = context.WithoutCancel(ctx)
	tracer := natstrace.New(nc)
	type lifecycleEvent struct {
		Name string `json:"name"`
	}
	spanAction := func(subject string) string {
		if i := strings.LastIndex(subject, "."); i >= 0 {
			return subject[i+1:]
		}
		return subject
	}
	created := func(msg *nats.Msg) {
		sp := tracer.StartFromHeaders(msg.Header, msg.Subject, msg.Data, "_platform", "accounts", "account", spanAction(msg.Subject))
		spanCtx := natstrace.ContextWithSpan(ctx, sp)
		var evt lifecycleEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil || evt.Name == "" {
			if m.log != nil {
				m.log.Error("decode notify.accounts.account.created", "err", err)
			}
			sp.Fail(err, msg.Data, nil)
			return
		}
		if err := m.EnsureByName(spanCtx, evt.Name); err != nil {
			if m.log != nil {
				m.log.Error("ensure tenant resources on provisioning event", "tenant", evt.Name, "err", err)
			}
			sp.Fail(err, msg.Data, nil)
			return
		}
		sp.End(msg.Data, nil)
	}
	suspended := func(msg *nats.Msg) {
		sp := tracer.StartFromHeaders(msg.Header, msg.Subject, msg.Data, "_platform", "accounts", "account", spanAction(msg.Subject))
		spanCtx := natstrace.ContextWithSpan(ctx, sp)
		var evt lifecycleEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil || evt.Name == "" {
			if m.log != nil {
				m.log.Error("decode notify.accounts.account.suspended", "err", err)
			}
			sp.Fail(err, msg.Data, nil)
			return
		}
		if err := m.TeardownByName(spanCtx, evt.Name); err != nil {
			if m.log != nil {
				m.log.Error("tear down tenant resources on suspension event", "tenant", evt.Name, "err", err)
			}
			sp.Fail(err, msg.Data, nil)
			return
		}
		sp.End(msg.Data, nil)
	}
	if _, err := nc.Subscribe("notify.accounts.account.created", created); err != nil {
		return err
	}
	if _, err := nc.Subscribe("notify.accounts.account.suspended", suspended); err != nil {
		return err
	}
	if _, err := nc.Subscribe("notify.accounts.account.reactivated", created); err != nil {
		return err
	}
	return nc.Flush()
}

// PublishToAll publishes data on subject over every currently-connected
// tenant connection (BR-D42's fan-out leg). refdata-service's own
// evt.{context}.refdata.{typeKey}.changed feed lives on the PLATFORM
// account, but a browser subscribes from inside its own tenant account —
// so a notify.* republish has to be made once per tenant connection rather
// than once globally. Best-effort: a failed publish on one tenant is logged
// and skipped, never propagated, since a change notification is a hint to
// refetch, not a delivery guarantee.
func (m *Manager) PublishToAll(subject string, data []byte) {
	m.mu.RLock()
	conns := make(map[string]*nats.Conn, len(m.resources))
	for tenant, res := range m.resources {
		conns[tenant] = res.nc
	}
	m.mu.RUnlock()
	for tenant, nc := range conns {
		if err := nc.Publish(subject, data); err != nil && m.log != nil {
			m.log.Warn("refdata notify publish failed", "tenant", tenant, "subject", subject, "err", err)
		}
	}
}

// Close stops every tenant's adapter and closes its connection — called on
// process shutdown.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for tenant, res := range m.resources {
		if err := res.adapter.Stop(); err != nil && m.log != nil {
			m.log.Error("stop browserrpc adapter on shutdown", "tenant", tenant, "err", err)
		}
		res.nc.Close()
	}
	m.resources = make(map[string]*resources)
}
