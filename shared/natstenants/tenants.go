// Package natstenants is the shared per-tenant NATS connection manager
// extracted in Phase 35 from four near-identical per-service copies —
// pricing-service, trading-partner-service, refdata-service, and
// shipping-service's rest/tenant.go (see BR-D40 and
// ARCHITECTURE-ACCOUNTS.md). All four discover tenants the same way (scan
// NATS_CREDS_DIR for *.creds files, excluding a fixed set of non-tenant
// stems), connect the same way (one persistent NATS connection per tenant,
// nats.Name(serviceName) set per CLAUDE.md's connection-naming rule), and
// subscribe to accounts-service's imported PLATFORM lifecycle events the
// same way (notify.accounts.account.{created,suspended,reactivated}) — that
// shared machinery lives here.
//
// What genuinely differs per service is what "the resource for one tenant"
// means: a bare browserrpc.Adapter (pricing, refdata), an
// Adapter+refdataclient.Client pair built in two passes because of a
// dependency-cycle constraint (trading-partner-service, BR-TP14), or a
// JetStream stream + three KV buckets + projectors + Adapter (shipping-
// service). Manager is generic over that resource type (R) via
// provision/deprovision callbacks, so this package never imports any
// service's browserrpc or domain types — the direction of dependency is
// service -> shared/natstenants, never the reverse (mirrors
// shared/browserrpc's own rule).
//
// shipping-service's REST SwitchTenant/getTenant "active tenant" concept is
// NOT part of this package and stays in shipping-service — Manager has no
// notion of a single "current" tenant, only a keyed store every caller
// addresses by name. At Phase 35's extraction, shipping-service was kept off
// Manager entirely: its bundle was assumed too entangled with that active-
// tenant switch to separate, and it already carried an equivalent per-tenant
// map of its own (Deps.TenantResources) that Manager would only duplicate.
// A later architecture review re-examined that call: Manager places no
// interface constraint on R, so the richer JetStream/KV/projector/Adapter
// bundle fits it directly, and the "active tenant" pointer turned out to be
// a thin, cleanly separable layer on top of a keyed store — exactly what
// Manager already provides — rather than something entangled with it. That
// review's conclusion stands: shipping-service now uses Manager[R] the same
// as the other three services (Deps.TenantResources is gone); this
// package's shape didn't need to change to accommodate it.
package natstenants

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

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// NonTenantCredsFiles are the .creds stems (checked case-insensitively) in
// the shared creds directory that are never switchable tenants: "platform"
// and "shipping-admin" are permanent PLATFORM credentials, "sys" is
// accounts-service's own credential, and "observability" is
// observability-service's restricted PLATFORM connection (Phase 30c). BR-D40:
// this list's own incompleteness — missing "observability" — was the actual
// bug this extraction closes; three services had independently fixed it and
// a fourth (refdata-service) never had the gap at all, only for a fifth
// copy of the same list to inevitably need the same fix again.
var NonTenantCredsFiles = map[string]bool{"platform": true, "shipping-admin": true, "sys": true, "observability": true}

// Credentials is one discovered tenant's creds file path.
type Credentials struct {
	CredsPath string
}

// Discover scans credsDir for *.creds files and returns the known-tenant
// map, excluding NonTenantCredsFiles. Re-scanned on every call rather than
// cached — seeing a just-minted or just-suspended tenant immediately
// matters more than avoiding a few stat calls, and the directory is small.
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
		if NonTenantCredsFiles[strings.ToLower(name)] {
			continue
		}
		out[name] = Credentials{CredsPath: filepath.Join(credsDir, e.Name())}
	}
	return out, nil
}

// LifecycleHandlers are the two idempotent operations SubscribeLifecycle
// wires to accounts-service's imported PLATFORM lifecycle events — Ensure
// for created/reactivated (the same handler serves both: reactivating is
// just Ensure again), Teardown for suspended.
type LifecycleHandlers struct {
	Ensure   func(ctx context.Context, tenant string) error
	Teardown func(ctx context.Context, tenant string) error
}

// SubscribeLifecycle subscribes nc to accounts-service's imported PLATFORM
// lifecycle events (notify.accounts.account.{created,suspended,reactivated})
// and dispatches to h, tracing each delivery via shared/natstrace exactly
// the way every other rpc.*/api.* handler does (BR-036). Each tenant
// connection observes these events directly rather than the service holding
// a second, separate PLATFORM-only creds file just for this — keeping the
// cross-account path entirely in accounts-service's declared imports.
func SubscribeLifecycle(ctx context.Context, nc *nats.Conn, log *slog.Logger, h LifecycleHandlers) error {
	ctx = context.WithoutCancel(ctx)
	tracer := natstrace.New(nc)
	type lifecycleEvent struct {
		Name string `json:"name"`
	}
	// spanAction reads the trailing token off a notify.accounts.account.*
	// subject — "created" is reused verbatim as the "reactivated" handler
	// too (same idempotent Ensure operation), so the span's action label
	// can't be a literal hardcoded per closure.
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
			if log != nil {
				log.Error("decode notify.accounts.account.created", "err", err)
			}
			sp.Fail(err, msg.Data, nil)
			return
		}
		if err := h.Ensure(spanCtx, evt.Name); err != nil {
			if log != nil {
				log.Error("ensure tenant resources on provisioning event", "tenant", evt.Name, "err", err)
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
			if log != nil {
				log.Error("decode notify.accounts.account.suspended", "err", err)
			}
			sp.Fail(err, msg.Data, nil)
			return
		}
		if err := h.Teardown(spanCtx, evt.Name); err != nil {
			if log != nil {
				log.Error("tear down tenant resources on suspension event", "tenant", evt.Name, "err", err)
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

// entry bundles one tenant's connection with its service-defined resource.
type entry[R any] struct {
	nc  *nats.Conn
	res R
}

// Manager holds one persistent NATS connection + a service-defined resource
// bundle (R) per known tenant. Provision builds R once a tenant's connection
// and lifecycle subscription are up; Deprovision tears R down before the
// connection is closed. Both run under Manager's own lock discipline, so
// they must not call back into Manager (EnsureByName, TeardownByName,
// Update, etc.) — use the returned error instead, or defer follow-up work
// outside the callback.
type Manager[R any] struct {
	natsURL     string
	credsDir    string
	serviceName string
	log         *slog.Logger

	provision   func(ctx context.Context, nc *nats.Conn, tenant string) (R, error)
	deprovision func(tenant string, res R) error

	mu        sync.RWMutex
	resources map[string]*entry[R]
}

// NewManager builds a Manager that connects to natsURL as tenant users
// discovered under credsDir, naming every connection serviceName (per
// CLAUDE.md's nats.Name(...) rule). provision builds a tenant's resource
// bundle once its connection is up and its lifecycle subscription is
// active; deprovision tears it down. Neither may call back into the
// returned Manager.
func NewManager[R any](
	natsURL, credsDir, serviceName string,
	log *slog.Logger,
	provision func(ctx context.Context, nc *nats.Conn, tenant string) (R, error),
	deprovision func(tenant string, res R) error,
) *Manager[R] {
	return &Manager[R]{
		natsURL:     natsURL,
		credsDir:    credsDir,
		serviceName: serviceName,
		log:         log,
		provision:   provision,
		deprovision: deprovision,
		resources:   make(map[string]*entry[R]),
	}
}

// EnsureAll provisions every tenant currently discoverable in credsDir that
// doesn't already have a connection — called once at Startup so every
// tenant present at boot gets working support immediately, not just
// whichever tenant happens to be referenced first. Failures are logged and
// skipped per-tenant rather than aborting Startup: one tenant's bad creds
// file shouldn't prevent every other tenant, or the service itself, from
// coming up.
func (m *Manager[R]) EnsureAll(ctx context.Context) error {
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

// EnsureByName reactively provisions a single tenant the moment
// accounts-service mints it (BR-030) — a no-op, not an error, if tenant
// isn't yet visible in credsDir (the creds file write happens before the
// notify publish, so this shouldn't race it in practice, but staying
// defensive means a stray/duplicate delivery can't fail loudly).
func (m *Manager[R]) EnsureByName(ctx context.Context, tenant string) error {
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

// TeardownByName closes tenant's connection after deprovisioning its
// resource — disabling nats.go's default reconnect loop against a .creds
// file accounts-service has already deleted (BR-031). A no-op if tenant was
// never provisioned or has already been torn down.
func (m *Manager[R]) TeardownByName(_ context.Context, tenant string) error {
	m.mu.Lock()
	e, ok := m.resources[tenant]
	if ok {
		delete(m.resources, tenant)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if m.deprovision != nil {
		if err := m.deprovision(tenant, e.res); err != nil && m.log != nil {
			m.log.Error("deprovision tenant resource on teardown", "tenant", tenant, "err", err)
		}
	}
	e.nc.Close()
	return nil
}

// ensure returns tenant's persistent connection+resource, creating it on
// first sight. Idempotent: a tenant already present is returned as-is — no
// reconnect, no re-provisioning.
func (m *Manager[R]) ensure(ctx context.Context, tenant, credsPath string) (*entry[R], error) {
	m.mu.RLock()
	e, ok := m.resources[tenant]
	m.mu.RUnlock()
	if ok {
		return e, nil
	}

	nc, err := nats.Connect(m.natsURL, nats.Name(m.serviceName), nats.UserCredentials(credsPath))
	if err != nil {
		return nil, fmt.Errorf("connect as tenant %q: %w", tenant, err)
	}

	if err := SubscribeLifecycle(ctx, nc, m.log, LifecycleHandlers{Ensure: m.EnsureByName, Teardown: m.TeardownByName}); err != nil {
		nc.Close()
		return nil, fmt.Errorf("subscribe tenant lifecycle for tenant %q: %w", tenant, err)
	}

	res, err := m.provision(ctx, nc, tenant)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("provision resource for tenant %q: %w", tenant, err)
	}

	candidate := &entry[R]{nc: nc, res: res}

	m.mu.Lock()
	// Re-check under the write lock — a concurrent ensure() for the same
	// tenant (EnsureAll racing a lifecycle event, e.g.) may have won first.
	if existing, ok := m.resources[tenant]; ok {
		m.mu.Unlock()
		if m.deprovision != nil {
			_ = m.deprovision(tenant, res)
		}
		nc.Close()
		return existing, nil
	}
	m.resources[tenant] = candidate
	m.mu.Unlock()

	return candidate, nil
}

// Resource returns tenant's current resource bundle, if a connection exists
// for it.
func (m *Manager[R]) Resource(tenant string) (R, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.resources[tenant]
	if !ok {
		var zero R
		return zero, false
	}
	return e.res, true
}

// Range calls fn once for every currently-connected tenant, from a
// consistent snapshot taken under a read lock — fn itself runs with no lock
// held, so it may safely call back into Manager (Update, TeardownByName,
// etc.).
func (m *Manager[R]) Range(fn func(tenant string, nc *nats.Conn, res R)) {
	m.mu.RLock()
	snapshot := make([]struct {
		tenant string
		nc     *nats.Conn
		res    R
	}, 0, len(m.resources))
	for tenant, e := range m.resources {
		snapshot = append(snapshot, struct {
			tenant string
			nc     *nats.Conn
			res    R
		}{tenant, e.nc, e.res})
	}
	m.mu.RUnlock()
	for _, s := range snapshot {
		fn(s.tenant, s.nc, s.res)
	}
}

// Update replaces tenant's resource bundle with fn's result, for a service
// whose resource shape is completed in a later pass than connect time (e.g.
// trading-partner-service's BR-TP14 two-pass adapter wiring, MountAPI). A
// no-op returning nil if tenant is no longer connected (it may have been
// torn down concurrently) — fn is not called in that case.
func (m *Manager[R]) Update(tenant string, fn func(nc *nats.Conn, cur R) (R, error)) error {
	m.mu.Lock()
	e, ok := m.resources[tenant]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	updated, err := fn(e.nc, e.res)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check: tenant may have been torn down while fn ran.
	if cur, ok := m.resources[tenant]; ok && cur == e {
		cur.res = updated
	}
	return nil
}

// Close deprovisions and closes every tenant's connection — called on
// process shutdown.
func (m *Manager[R]) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for tenant, e := range m.resources {
		if m.deprovision != nil {
			if err := m.deprovision(tenant, e.res); err != nil && m.log != nil {
				m.log.Error("deprovision tenant resource on shutdown", "tenant", tenant, "err", err)
			}
		}
		e.nc.Close()
	}
	m.resources = make(map[string]*entry[R])
}
