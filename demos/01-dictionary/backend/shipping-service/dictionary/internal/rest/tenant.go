package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/shared/natstenants"
)

// discoverTenants scans credsDir (the shared volume accounts-service also
// writes into, Phase 14b) for *.creds files and returns the known-tenant map
// SwitchTenant and getTenant used to get from a static, hardcoded map
// (Phase 13b/14a) — this is the only way a tenant minted by accounts-service
// after this process started becomes visible without a shipping-service
// restart. Delegates to shared/natstenants.Discover (Phase 35) — the
// PLATFORM/shipping-admin/SYS/observability exclusion list this used to
// carry its own copy of lives there now, shared with every other service's
// per-tenant connection manager.
func discoverTenants(credsDir string) (map[string]natstenants.Credentials, error) {
	return natstenants.Discover(credsDir)
}

// tenantResources bundles everything scoped to ONE tenant NATS account: its
// own connection, JetStream context, the three KV stores, the ship/container
// command handlers, the query handlers, the three durable projector
// ConsumeContexts, and its browserrpc.Adapter (Phase 15a, renamed from
// natsrpc in Phase 16b).
//
// Before Phase 15, this bundle (minus the Adapter) was rebuilt from scratch
// on every SwitchTenant call — including switching BACK to a
// previously-active tenant — and the old bundle's projectors were Stop()'d
// on every switch-away. Phase 15's browserrpc adapters need to keep
// answering api.* calls for EVERY known tenant regardless of which single
// tenant REST's SwitchTenant currently has active (a browser connected
// directly to ACME's account must keep working even while the Admin operator has
// GLOBEX active) — and a browser command published into a tenant's own
// SHIPPING stream needs that SAME tenant's projectors running to update its
// KV read model and fire notify.* (Phase 15b), regardless of REST's active
// selection. So every tenant's resources are now created ONCE, the first
// time that tenant is seen (either at Startup, via EnsureAllTenants, or the
// first time an operator switches to it), and then kept alive permanently.
//
// This bundle is R in Handlers.mgr, a shared/natstenants.Manager[R] (an
// architecture review reversed Phase 35's original call to keep
// shipping-service off Manager — see natstenants.go's package doc). Manager
// owns the per-tenant map, the connect/reconnect, and the lifecycle-event
// subscription that used to live here as ensureTenantResources/
// subscribeTenantLifecycle; buildTenantResources/teardownTenantResources
// below are its provision/deprovision callbacks — everything Manager can't
// know about a tenant's resources without shipping-service telling it.
// SwitchTenant still never stops or rebuilds anything; it only changes
// which tenant's *already-running* bundle REST/SSE's Deps fields point at.
type tenantResources struct {
	nc                            *nats.Conn
	js                            jetstream.JetStream
	kvShips, kvContainers, kvMeta *kvstore.Store
	ships                         *commands.ShipHandler
	containers                    *commands.ContainerHandler
	shipReads                     *queries.Ships
	terminal                      *queries.Terminal
	meta                          *queries.Meta
	projectors                    []jetstream.ConsumeContext
	rpcAdapter                    *browserrpc.Adapter
}

// SwitchTenant points REST/SSE's Deps fields at tenant's persistent resource
// bundle, creating it first via h.mgr.EnsureByName if this is the first
// time tenant has ever been seen. See tenantResources's doc comment for why
// switching is no longer destructive — this never stops or rebuilds
// anything that already exists, unlike the pre-Phase-15 version of this
// function.
//
// This is deliberately the *same* code path used for the initial connect at
// Startup and for every later switch (composition.go calls it once with the
// initial tenant before Mount) — there is no separate bootstrap case to keep
// in sync.
func (h *Handlers) SwitchTenant(ctx context.Context, tenant string) error {
	prev := h.deps()
	if tenant == prev.Tenant && prev.TenantNC != nil && !prev.TenantNC.IsClosed() {
		return nil // already active; not an error, just a no-op
	}

	// EnsureByName is a no-op (nil error), not an error, for a tenant name
	// with no creds file — the Resource lookup below is what turns that
	// into "unknown tenant", same observable failure as the pre-Manager
	// version's own discoverTenants+lookup.
	if err := h.mgr.EnsureByName(ctx, tenant); err != nil {
		return err
	}
	res, ok := h.mgr.Resource(tenant)
	if !ok {
		return fmt.Errorf("unknown tenant %q", tenant)
	}

	// Re-read: h.mgr.EnsureByName may have just run concurrently with
	// another switch — start from the latest snapshot rather than the
	// possibly-stale prev.
	next := h.deps()
	next.Ships = res.ships
	next.Containers = res.containers
	next.ShipReads = res.shipReads
	next.Terminal = res.terminal
	next.Meta = res.meta
	next.KVCont, next.KVMeta = res.kvContainers, res.kvMeta
	next.JS = res.js
	next.Tenant = tenant
	next.TenantNC = res.nc

	h.SetDeps(next)
	return nil
}

// buildTenantResources is Handlers.mgr's provision callback: it builds one
// tenant's persistent resource bundle over an already-connected,
// already-lifecycle-subscribed nc. deps supplies the static (never-changing)
// fields captured at NewHandlers — ShipRepo/ContainerRepo/PortRepo/Ports/Log
// — since a provision callback has no access to Handlers.deps() done later.
func buildTenantResources(ctx context.Context, nc *nats.Conn, tenant string, deps Deps) (*tenantResources, error) {
	// eventhandler.register() and browserrpc's micro.AddService close over this
	// ctx (or a derived one) for the *entire remaining lifetime of the
	// process* now — not just until the next switch, since resources here
	// are never torn down. Startup's ctx is already long-lived (canceled
	// only on SIGINT/SIGTERM), but this is also reachable from an HTTP
	// request's r.Context() via switchTenant below, which is canceled the
	// instant that response is sent — stripping cancellation (keeping any
	// values) is required so a REST-triggered first-sight of a tenant
	// doesn't leave its projectors failing every event with "context
	// canceled" and stuck redelivering forever.
	ctx = context.WithoutCancel(ctx)

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream context for tenant %q: %w", tenant, err)
	}
	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		return nil, fmt.Errorf("create stream for tenant %q: %w", tenant, err)
	}

	kvShips := kvstore.New(js, domain.ShipBucketPrefix)
	kvContainers := kvstore.New(js, domain.ContainerBucketPrefix)
	kvMeta := kvstore.New(js, domain.MetaBucketPrefix)
	// Phase 23: the Admin UI's KV inspector watches these over
	// notify.{context}.kv.{bucket}.{key}.changed instead of the SSE
	// watchKVBucket handler it replaces.
	for _, kv := range []*kvstore.Store{kvShips, kvContainers, kvMeta} {
		kv.EnableNotify(nc, deps.Log)
	}

	projectors, err := registerProjectors(ctx, js, kvShips, kvContainers, kvMeta, nc, deps.ShipRepo, deps.ContainerRepo, deps.Log)
	if err != nil {
		return nil, fmt.Errorf("register projectors for tenant %q: %w", tenant, err)
	}

	pub := jstream.NewPublisher(js)
	// Phase 43a (BR-045): every evt.* publish this tenant makes also emits an
	// obs.pubsub.* observation, which PLATFORM imports under
	// monitor.{tenant}.pubsub.> for the Admin UI's Messages panel.
	pub.EnableObservation(nc)
	ships := commands.NewShipHandler(pub, js, deps.PortRepo)
	containers := commands.NewContainerHandler(pub, js, deps.PortRepo)
	terminal := queries.NewTerminal(kvContainers)
	meta := queries.NewMeta(kvMeta)
	shipReads := queries.NewShips(kvShips, deps.ShipRepo)

	rpcAdapter, err := browserrpc.New(nc, browserrpc.Deps{
		Ships:      ships,
		Containers: containers,
		Ports:      deps.Ports, // static: Postgres-backed, not account-scoped — shared across every tenant's adapter
		Terminal:   terminal,
		Meta:       meta,
		ShipReads:  shipReads,
		Log:        deps.Log,
		Tenant:     tenant,
	})
	if err != nil {
		stopAll(projectors)
		return nil, fmt.Errorf("register rpc adapter for tenant %q: %w", tenant, err)
	}

	return &tenantResources{
		nc:           nc,
		js:           js,
		kvShips:      kvShips,
		kvContainers: kvContainers,
		kvMeta:       kvMeta,
		ships:        ships,
		containers:   containers,
		shipReads:    shipReads,
		terminal:     terminal,
		meta:         meta,
		projectors:   projectors,
		rpcAdapter:   rpcAdapter,
	}, nil
}

// teardownTenantResources is Handlers.mgr's deprovision callback: it stops
// res's projectors and rpc adapter. Manager.TeardownByName closes res.nc
// itself immediately after this returns — that's what actually stops
// nats.go's reconnect loop against a .creds file accounts-service has
// already deleted (see ARCHITECTURE-ACCOUNTS.md § 2t-a); stopping the
// projectors and adapter first isn't strictly required for that (closing nc
// alone would stop them eventually via read errors), but avoids each one
// logging a burst of errors against a connection already known to be torn
// down deliberately.
func teardownTenantResources(tenant string, res *tenantResources, log *slog.Logger) error {
	stopAll(res.projectors)
	if err := res.rpcAdapter.Stop(); err != nil {
		log.Error("stop rpc adapter during tenant teardown", "tenant", tenant, "err", err)
	}
	return nil
}

// EnsureAllTenants creates persistent resources for every tenant currently
// discoverable in CredsDir that doesn't already have them — called once at
// Startup so every tenant present at boot gets working rpc.*/notify.*
// support immediately, not just the one REST starts out active on. A tenant
// minted later by accounts-service is instead picked up the first time any
// SwitchTenant call names it (an operator switching REST to it, e.g.) —
// there is no background poll for newly minted tenants nobody has
// referenced yet. Delegates to Handlers.mgr.EnsureAll (shared/natstenants),
// which already logs and skips per-tenant failures rather than aborting.
func (h *Handlers) EnsureAllTenants(ctx context.Context) error {
	return h.mgr.EnsureAll(ctx)
}

// EnsureTenantByName reactively provisions a single tenant's resources the
// moment accounts-service mints it (BR-030: composition.go subscribes to
// accounts-service's notify.accounts.account.created and calls this) —
// closing the gap EnsureAllTenants's doc comment above describes: a browser
// connecting directly to a brand-new tenant's account (Sea Freight Flow,
// Phase 15d — never calls SwitchTenant) previously got no working api.*
// adapter until a human happened to switch the Admin UI to that tenant, or
// the process restarted. Every api.* request in the meantime timed out
// silently (5s, then swallowed by the browser's own catch), which read as
// "this tenant has no ships/ports" rather than "not provisioned yet".
// Delegates to Handlers.mgr.EnsureByName, which is a no-op, not an error,
// if tenant isn't yet visible in CredsDir — a stray/duplicate delivery
// can't fail loudly.
func (h *Handlers) EnsureTenantByName(ctx context.Context, tenant string) error {
	return h.mgr.EnsureByName(ctx, tenant)
}

// TeardownTenantByName is the mirror of EnsureTenantByName (BR-031;
// accounts-service's Handlers.publishAccountSuspended, BR-AC09, is the
// producer) — reacts to a tenant being suspended by stopping that tenant's
// persistent resource bundle instead of leaving it running. Delegates to
// Handlers.mgr.TeardownByName, which deprovisions (see
// teardownTenantResources) and then closes the tenant's connection — a
// no-op, not an error, if tenant was never provisioned or has already been
// torn down.
//
// Deliberately does not touch deps.Tenant/TenantNC even if tenant happens to
// be REST's currently-active tenant — SwitchTenant already does not stop or
// rebuild anything (see tenantResources's doc comment), and there is no
// precedent here for this function auto-switching REST away from a tenant
// an operator explicitly selected. A REST/SSE request against a suspended
// active tenant will simply start failing against the closed connection,
// which is the correct outcome for a tenant that no longer exists.
func (h *Handlers) TeardownTenantByName(ctx context.Context, tenant string) error {
	return h.mgr.TeardownByName(ctx, tenant)
}

// registerProjectors starts the three projector durables against js and
// returns their ConsumeContexts. Unlike before Phase 15, the caller never
// stops these — see tenantResources's doc comment.
//
// nc is this tenant's own connection, threaded through to RegisterShips/
// RegisterContainers/RegisterMeta (Phase 15b) so their notify.* publishes
// land on the SAME account a browser connected to this tenant is listening
// on — never a shared/PLATFORM connection, which would publish into the
// wrong (or an inaccessible) account entirely.
func registerProjectors(
	ctx context.Context,
	js jetstream.JetStream,
	kvShips, kvContainers, kvMeta *kvstore.Store,
	nc *nats.Conn,
	shipRepo domain.ShipRepository,
	containerRepo domain.ContainerRepository,
	log *slog.Logger,
) ([]jetstream.ConsumeContext, error) {
	var out []jetstream.ConsumeContext

	ccShips, err := eventhandler.RegisterShips(ctx, js, kvShips, nc, shipRepo, log)
	if err != nil {
		return nil, err
	}
	out = append(out, ccShips)

	ccCont, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nc, containerRepo, log)
	if err != nil {
		stopAll(out)
		return nil, err
	}
	out = append(out, ccCont)

	ccMeta, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nc, log)
	if err != nil {
		stopAll(out)
		return nil, err
	}
	out = append(out, ccMeta)

	return out, nil
}

func stopAll(ccs []jetstream.ConsumeContext) {
	for _, cc := range ccs {
		cc.Stop()
	}
}

// tenantResponse is what GET /api/tenant returns: the active tenant plus the
// known, switchable tenant list (sorted for a stable dropdown order).
type tenantResponse struct {
	Tenant    string   `json:"tenant"`
	Available []string `json:"available"`
}

// @Summary      Active tenant + switchable tenant list
// @Tags         tenant
// @Produce      json
// @Success      200  {object}  tenantResponse
// @Router       /api/tenant [get]
func (h *Handlers) getTenant(w http.ResponseWriter, r *http.Request) {
	deps := h.deps()
	known, err := discoverTenants(deps.CredsDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	available := make([]string, 0, len(known))
	for t := range known {
		available = append(available, t)
	}
	sort.Strings(available)
	writeJSON(w, http.StatusOK, tenantResponse{Tenant: deps.Tenant, Available: available})
}

type switchTenantRequest struct {
	Tenant string `json:"tenant"`
}

// @Summary      Switch the active tenant (Phase 13b)
// @Description  Reconnects shipping-service's tenant-scoped NATS connection under a different account and rebinds every ship/container resource to it. The previous tenant's data becomes unreachable because the server enforces the account boundary, not because of an application-level filter.
// @Tags         tenant
// @Accept       json
// @Produce      json
// @Param        request  body      switchTenantRequest  true  "target tenant"
// @Success      200      {object}  tenantResponse
// @Failure      400      {object}  errorResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/tenant/switch [post]
func (h *Handlers) switchTenant(w http.ResponseWriter, r *http.Request) {
	var in switchTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant is required")
		return
	}
	if err := h.SwitchTenant(r.Context(), in.Tenant); err != nil {
		h.deps().Log.Error("switch tenant", "tenant", in.Tenant, "err", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.getTenant(w, r)
}
