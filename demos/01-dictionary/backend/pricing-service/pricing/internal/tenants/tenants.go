// Package tenants manages one bare NATS connection per tenant so
// pricing-service's browserrpc.Adapter can answer api.* calls on every
// known tenant's account — mirroring shipping-service's
// dictionary/internal/rest/tenant.go, but lighter: no JetStream, KV, or
// projectors, since pricing data lives in one shared Postgres scoped by
// `context` (business unit), not per-tenant NATS resources. The only thing
// that's genuinely per-tenant here is the NATS connection itself.
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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/natstrace"
)

// nonTenantCredsFiles mirrors shipping-service's own list (rest/tenant.go)
// — these .creds stems in the shared creds directory are never tenants.
// "observability" (observability-service's restricted PLATFORM connection,
// Phase 30c) was missing here — the same gap found live in
// trading-partner-service's identical list: without it, Discover offers
// observability.creds as a switchable tenant, opening a phantom connection
// under a PLATFORM-account user that was never granted tenant-shaped
// permissions.
var nonTenantCredsFiles = map[string]bool{"platform": true, "shipping-admin": true, "sys": true, "observability": true}

// Credentials is one discovered tenant's creds file path.
type Credentials struct {
	CredsPath string
}

// Discover scans credsDir for *.creds files and returns the known-tenant
// map. Re-scanned on every call rather than cached — same rationale as
// shipping-service's discoverTenants: seeing a just-minted or
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
// every tenant's Adapter, since pricing data isn't NATS-account-scoped
// (see browserrpc's package doc comment); only deps.Tenant is overwritten
// per connection.
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
// immediately, not just whichever tenant happens to be referenced first.
// Failures are logged and skipped per-tenant rather than aborting Startup:
// one tenant's bad creds file shouldn't prevent every other tenant, or the
// service itself, from coming up.
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
// moment accounts-service mints it (mirrors shipping-service's
// EnsureTenantByName, BR-030) — a no-op, not an error, if tenant isn't yet
// visible in credsDir (the creds file write happens before the notify
// publish, so this shouldn't race it in practice, but staying defensive
// means a stray/duplicate delivery can't fail loudly).
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
// file accounts-service has already deleted — mirroring shipping-service's
// TeardownTenantByName (BR-031). A no-op if tenant was never provisioned or
// has already been torn down.
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
// first sight. Idempotent: a tenant already present is returned as-is —
// no reconnect, no re-registration.
func (m *Manager) ensure(ctx context.Context, tenant, credsPath string) (*resources, error) {
	m.mu.RLock()
	res, ok := m.resources[tenant]
	m.mu.RUnlock()
	if ok {
		return res, nil
	}

	nc, err := nats.Connect(m.natsURL, nats.Name("pricing-service"), nats.UserCredentials(credsPath))
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

// subscribeLifecycle mirrors shipping-service's subscribeTenantLifecycle:
// each tenant connection observes accounts-service's imported PLATFORM
// lifecycle events directly, rather than pricing-service holding a second,
// separate PLATFORM-only creds file just for this — keeping the
// cross-account path entirely in accounts-service's declared imports.
func (m *Manager) subscribeLifecycle(ctx context.Context, nc *nats.Conn) error {
	ctx = context.WithoutCancel(ctx)
	tracer := natstrace.New(nc)
	type lifecycleEvent struct {
		Name string `json:"name"`
	}
	// spanAction reads the trailing token off a notify.accounts.account.*
	// subject — "created" below is reused verbatim as the "reactivated"
	// handler too (same idempotent Ensure operation), so the span's action
	// label can't be a literal hardcoded per closure.
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
