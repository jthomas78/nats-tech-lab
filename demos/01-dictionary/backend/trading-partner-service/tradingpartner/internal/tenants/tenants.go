// Package tenants manages one NATS connection per tenant, discovered from
// NATS_CREDS_DIR, so BR-TP14's fleet-asset validation can reach
// refdata-service over the requesting tenant's own account import. Mirrors
// pricing-service's internal/tenants, including its browserrpc.Adapter per
// connection — that adapter is currently a micro-service *registration* only
// (see internal/browserrpc's package doc): it makes this service discoverable
// on $SRV, while REST remains the live inbound transport until the api.*
// endpoints land.
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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/natstrace"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/refdataclient"
)

// Manager implements domain.VehicleTypeValidator directly (BR-TP14) — it's
// the natural owner of "which tenant's connection to use," so there's no
// separate wrapper type.
var _ domain.VehicleTypeValidator = (*Manager)(nil)

// nonTenantCredsFiles mirrors shipping-service's/pricing-service's own list
// — these .creds stems in the shared creds directory are never tenants.
// "observability" (observability-service's restricted PLATFORM connection,
// Phase 30c) was missing here: Discover treated it as a switchable tenant,
// so this service opened a phantom "tenant" connection using
// observability.creds (a PLATFORM-account user with a narrow, non-tenant
// permission set) and then tried to run full tenant machinery over it — the
// notify.accounts.account.* subscription and the browserrpc adapter's
// api.*.trading-partner.* registration — both denied with a Subscription
// Violation, since the observability user was never meant to carry tenant
// grants. Same bug, same fix shipping-service's rest/tenant.go already
// applied (Phase 30h); this file was never updated to match.
var nonTenantCredsFiles = map[string]bool{"platform": true, "shipping-admin": true, "sys": true, "observability": true}

// Credentials is one discovered tenant's creds file path.
type Credentials struct {
	CredsPath string
}

// Discover scans credsDir for *.creds files and returns the known-tenant
// map. Re-scanned on every call, not cached — same rationale as
// shipping-service/pricing-service: seeing a just-minted or just-suspended
// tenant immediately matters more than avoiding a few stat calls.
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
	client  *refdataclient.Client
	adapter *browserrpc.Adapter
}

// Manager holds one persistent NATS connection + refdataclient.Client +
// browserrpc.Adapter per known tenant.
//
// The adapter is mounted in a second pass (MountAPI), not at connect time,
// because trading-partner-service has a dependency cycle the other services
// don't: composition.Startup needs this Manager to satisfy BR-TP14's
// VehicleTypeValidator, while the adapter needs the command handlers Startup
// builds. Connections come up first (so refdata validation works), then
// MountAPI backfills adapters onto them.
type Manager struct {
	natsURL  string
	credsDir string
	log      *slog.Logger

	mu        sync.RWMutex
	resources map[string]*resources
	// apiDeps is nil until MountAPI is called; while nil, tenant connections
	// carry no api.* adapter and this service answers only over REST.
	apiDeps *browserrpc.Deps
}

func NewManager(natsURL, credsDir string, log *slog.Logger) *Manager {
	return &Manager{natsURL: natsURL, credsDir: credsDir, log: log, resources: make(map[string]*resources)}
}

// EnsureAll connects to every tenant currently discoverable in credsDir that
// doesn't already have a connection — called once at Startup. Failures are
// logged and skipped per-tenant, not fatal to Startup.
func (m *Manager) EnsureAll(ctx context.Context) error {
	known, err := Discover(m.credsDir)
	if err != nil {
		return err
	}
	for tenant, creds := range known {
		if _, err := m.ensure(ctx, tenant, creds.CredsPath); err != nil && m.log != nil {
			m.log.Error("ensure tenant connection at startup", "tenant", tenant, "err", err)
		}
	}
	return nil
}

// EnsureByName reactively connects a single tenant the moment
// accounts-service mints it (mirrors shipping-service's/pricing-service's
// EnsureTenantByName, BR-030) — a no-op if tenant isn't yet visible in
// credsDir.
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
// registration first, so the tenant stops answering $SRV discovery rather than
// lingering as a phantom row in the Services panel (mirrors BR-031). A no-op
// if tenant was never connected or already torn down.
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
	// adapter is nil for a tenant that connected before MountAPI ran.
	if res.adapter != nil {
		if err := res.adapter.Stop(); err != nil && m.log != nil {
			m.log.Error("stop browserrpc adapter on teardown", "tenant", tenant, "err", err)
		}
	}
	res.nc.Close()
	return nil
}

// MountAPI registers the api.* adapter on every currently-connected tenant and
// on every tenant connected afterwards. Called once, after
// composition.Startup has built the command handlers deps carries. deps.Tenant
// is ignored — it's filled in per connection.
func (m *Manager) MountAPI(deps browserrpc.Deps) error {
	m.mu.Lock()
	m.apiDeps = &deps
	pending := make(map[string]*resources, len(m.resources))
	for tenant, res := range m.resources {
		if res.adapter == nil {
			pending[tenant] = res
		}
	}
	m.mu.Unlock()

	for tenant, res := range pending {
		adapter, err := m.newAdapter(tenant, res.nc)
		if err != nil {
			return fmt.Errorf("mount api adapter for tenant %q: %w", tenant, err)
		}
		m.mu.Lock()
		// Re-check under the lock: the tenant may have been torn down (or already
		// adapted by a concurrent ensure) while we were registering.
		if cur, ok := m.resources[tenant]; ok && cur == res && cur.adapter == nil {
			cur.adapter = adapter
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()
		_ = adapter.Stop()
	}
	return nil
}

// newAdapter registers an api.* adapter on nc for tenant, or returns (nil, nil)
// if MountAPI hasn't run yet.
func (m *Manager) newAdapter(tenant string, nc *nats.Conn) (*browserrpc.Adapter, error) {
	m.mu.RLock()
	deps := m.apiDeps
	m.mu.RUnlock()
	if deps == nil {
		return nil, nil
	}
	scoped := *deps
	scoped.Tenant = tenant
	scoped.Log = m.log
	return browserrpc.New(nc, scoped)
}

// Client returns tenant's refdataclient.Client, if a connection exists for
// it.
func (m *Manager) Client(tenant string) (*refdataclient.Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res, ok := m.resources[tenant]
	if !ok {
		return nil, false
	}
	return res.client, true
}

// ErrTenantNotConnected is returned when Exists is called for a tenant this
// Manager has no live connection for (an unknown or not-yet-discovered
// .creds file).
var ErrTenantNotConnected = fmt.Errorf("tenant is not connected")

// Exists implements domain.VehicleTypeValidator (BR-TP14) by resolving
// tenant's own connection and delegating to its refdataclient.Client.
func (m *Manager) Exists(ctx context.Context, tenant, contextKey, code string) (bool, error) {
	client, ok := m.Client(tenant)
	if !ok {
		return false, ErrTenantNotConnected
	}
	return client.Exists(ctx, contextKey, code)
}

func (m *Manager) ensure(ctx context.Context, tenant, credsPath string) (*resources, error) {
	m.mu.RLock()
	res, ok := m.resources[tenant]
	m.mu.RUnlock()
	if ok {
		return res, nil
	}

	nc, err := nats.Connect(m.natsURL, nats.Name("trading-partner-service"), nats.UserCredentials(credsPath))
	if err != nil {
		return nil, fmt.Errorf("connect as tenant %q: %w", tenant, err)
	}

	if err := m.subscribeLifecycle(ctx, nc); err != nil {
		nc.Close()
		return nil, fmt.Errorf("subscribe tenant lifecycle for tenant %q: %w", tenant, err)
	}

	// Only if MountAPI has run — see Manager's doc comment on the two-pass wiring.
	adapter, err := m.newAdapter(tenant, nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("register browserrpc adapter for tenant %q: %w", tenant, err)
	}

	candidate := &resources{nc: nc, client: refdataclient.New(nc), adapter: adapter}

	m.mu.Lock()
	if existing, ok := m.resources[tenant]; ok {
		m.mu.Unlock()
		// Lost the race — drop this duplicate registration before closing the
		// connection, so the winner's $SRV identity is the only one left.
		if adapter != nil {
			_ = adapter.Stop()
		}
		nc.Close()
		return existing, nil
	}
	m.resources[tenant] = candidate
	m.mu.Unlock()

	return candidate, nil
}

// subscribeLifecycle mirrors shipping-service's/pricing-service's own
// subscribeTenantLifecycle: each tenant connection observes
// accounts-service's imported PLATFORM lifecycle events directly.
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
				m.log.Error("ensure tenant connection on provisioning event", "tenant", evt.Name, "err", err)
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
				m.log.Error("tear down tenant connection on suspension event", "tenant", evt.Name, "err", err)
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

// Close closes every tenant's connection — called on process shutdown.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for tenant, res := range m.resources {
		if res.adapter != nil {
			if err := res.adapter.Stop(); err != nil && m.log != nil {
				m.log.Error("stop browserrpc adapter on shutdown", "tenant", tenant, "err", err)
			}
		}
		res.nc.Close()
	}
	m.resources = make(map[string]*resources)
}
