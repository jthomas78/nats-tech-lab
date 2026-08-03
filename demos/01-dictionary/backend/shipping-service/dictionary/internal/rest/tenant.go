package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// nonTenantCredsFiles are the .creds stems (checked case-insensitively —
// see below) in the shared creds directory that are never switchable
// tenants — PLATFORM is the permanent connection (monolith.Monolith.NC/JS),
// SYS is accounts-service's own credential (Phase 14b), neither is a
// ship/container tenant account.
var nonTenantCredsFiles = map[string]bool{"platform": true, "sys": true}

// discoverTenants scans credsDir (the shared volume accounts-service also
// writes into, Phase 14b) for *.creds files and returns the known-tenant map
// SwitchTenant and getTenant used to get from a static, hardcoded map
// (Phase 13b/14a) — this is the only way a tenant minted by accounts-service
// after this process started becomes visible without a shipping-service
// restart. Re-scanned on every call rather than cached: the directory is
// small, and correctness (seeing a just-minted or just-suspended tenant
// immediately) matters more than avoiding a handful of stat calls.
func discoverTenants(credsDir string) (map[string]TenantCredentials, error) {
	entries, err := os.ReadDir(credsDir)
	if err != nil {
		return nil, fmt.Errorf("scan creds dir %q: %w", credsDir, err)
	}
	out := make(map[string]TenantCredentials)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".creds") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".creds")
		// Checked case-insensitively — defense in depth against a
		// differently-cased "Default"/"SYS"/etc. creds file ever reaching
		// this directory (accounts-service's own reservedAccountNames check,
		// accounts/handler.go, is meant to prevent that account from being
		// mintable in the first place; this is the fallback if it isn't).
		if nonTenantCredsFiles[strings.ToLower(name)] {
			continue
		}
		out[name] = TenantCredentials{CredsPath: filepath.Join(credsDir, e.Name())}
	}
	return out, nil
}

// tenantResources bundles everything scoped to ONE tenant NATS account: its
// own connection, JetStream context, the four KV stores, the ship/container
// command handlers, the query handlers, the four durable projector
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
// first time an operator switches to it), and then kept alive permanently —
// see ensureTenantResources. SwitchTenant no longer stops or rebuilds
// anything; it only changes which tenant's *already-running* bundle REST/
// SSE's Deps fields point at.
type tenantResources struct {
	nc                             *nats.Conn
	js                             jetstream.JetStream
	kvA, kvB, kvContainers, kvMeta *kvstore.Store
	ships                          *commands.ShipHandler
	containers                     *commands.ContainerHandler
	shapeB                         *queries.ShapeB
	shapeC                         *queries.ShapeC
	terminal                       *queries.Terminal
	meta                           *queries.Meta
	shapeA                         *queries.ShapeA
	projectors                     []jetstream.ConsumeContext
	rpcAdapter                     *browserrpc.Adapter
}

// SwitchTenant points REST/SSE's Deps fields at tenant's persistent resource
// bundle, creating it first via ensureTenantResources if this is the first
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
	known, err := discoverTenants(prev.CredsDir)
	if err != nil {
		return err
	}
	creds, ok := known[tenant]
	if !ok {
		return fmt.Errorf("unknown tenant %q", tenant)
	}
	if tenant == prev.Tenant && prev.TenantNC != nil && !prev.TenantNC.IsClosed() {
		return nil // already active; not an error, just a no-op
	}

	res, err := h.ensureTenantResources(ctx, tenant, creds.CredsPath)
	if err != nil {
		return err
	}

	// Re-read: ensureTenantResources may have just added tenant to the
	// shared TenantResources map via its own SetDeps — start from that
	// latest snapshot rather than the possibly-stale prev.
	next := h.deps()
	next.Ships = res.ships
	next.Containers = res.containers
	next.ShapeB = res.shapeB
	next.ShapeC = res.shapeC
	next.Terminal = res.terminal
	next.Meta = res.meta
	next.KVA, next.KVB, next.KVCont, next.KVMeta = res.kvA, res.kvB, res.kvContainers, res.kvMeta
	next.JS = res.js
	next.Tenant = tenant
	next.TenantNC = res.nc

	h.SetDeps(next)
	return nil
}

// ensureTenantResources returns tenant's persistent resource bundle,
// creating it on first sight. Idempotent: a tenant already present in
// TenantResources is returned as-is — no reconnect, no re-registration, and
// critically no Stop() of anything, since other tenants' browsers may be
// actively relying on that bundle's api.* adapter and projectors right now.
func (h *Handlers) ensureTenantResources(ctx context.Context, tenant, credsPath string) (*tenantResources, error) {
	prev := h.deps()
	if res, ok := prev.TenantResources[tenant]; ok {
		return res, nil
	}

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

	nc, err := nats.Connect(prev.NatsURL, nats.Name("shipping-service"), nats.UserCredentials(credsPath))
	if err != nil {
		return nil, fmt.Errorf("connect as tenant %q: %w", tenant, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream context for tenant %q: %w", tenant, err)
	}
	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		nc.Close()
		return nil, fmt.Errorf("create stream for tenant %q: %w", tenant, err)
	}

	kvA := kvstore.New(js, domain.ShapeABucketPrefix)
	kvB := kvstore.New(js, domain.ShapeBBucketPrefix)
	kvContainers := kvstore.New(js, domain.ContainerBucketPrefix)
	kvMeta := kvstore.New(js, domain.MetaBucketPrefix)

	projectors, err := registerProjectors(ctx, js, kvA, kvB, kvContainers, kvMeta, nc, prev.ShipRepo, prev.ContainerRepo, prev.Log)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("register projectors for tenant %q: %w", tenant, err)
	}

	pub := jstream.NewPublisher(js)
	ships := commands.NewShipHandler(pub, js, prev.PortRepo)
	containers := commands.NewContainerHandler(pub, js, prev.PortRepo)
	terminal := queries.NewTerminal(kvContainers)
	meta := queries.NewMeta(kvMeta)
	shapeA := queries.NewShapeA(kvA)

	rpcAdapter, err := browserrpc.New(nc, browserrpc.Deps{
		Ships:      ships,
		Containers: containers,
		Ports:      prev.Ports, // static: Postgres-backed, not account-scoped — shared across every tenant's adapter
		Terminal:   terminal,
		Meta:       meta,
		ShapeA:     shapeA,
		Log:        prev.Log,
		Tenant:     tenant,
	})
	if err != nil {
		stopAll(projectors)
		nc.Close()
		return nil, fmt.Errorf("register rpc adapter for tenant %q: %w", tenant, err)
	}

	res := &tenantResources{
		nc:           nc,
		js:           js,
		kvA:          kvA,
		kvB:          kvB,
		kvContainers: kvContainers,
		kvMeta:       kvMeta,
		ships:        ships,
		containers:   containers,
		shapeB:       queries.NewShapeB(kvB, prev.ShipRepo),
		shapeC:       queries.NewShapeC(js),
		terminal:     terminal,
		meta:         meta,
		shapeA:       shapeA,
		projectors:   projectors,
		rpcAdapter:   rpcAdapter,
	}

	// Copy-on-write into the shared map, re-reading h.deps() rather than
	// reusing prev — this call may race a sibling ensureTenantResources call
	// for a DIFFERENT tenant (e.g. EnsureAllTenants looping over several at
	// once); starting from the latest snapshot avoids one call's map update
	// clobbering the other's. This is a lab/POC-scale race window (two
	// tenants first-seen in the same instant), not a hot path.
	latest := h.deps()
	newMap := make(map[string]*tenantResources, len(latest.TenantResources)+1)
	for k, v := range latest.TenantResources {
		newMap[k] = v
	}
	newMap[tenant] = res
	latest.TenantResources = newMap
	h.SetDeps(latest)

	return res, nil
}

// EnsureAllTenants creates persistent resources (see ensureTenantResources)
// for every tenant currently discoverable in CredsDir that doesn't already
// have them — called once at Startup so every tenant present at boot gets
// working rpc.*/notify.* support immediately, not just the one REST starts
// out active on. A tenant minted later by accounts-service is instead
// picked up the first time any SwitchTenant call names it (an operator
// switching REST to it, e.g.) — there is no background poll for newly
// minted tenants nobody has referenced yet.
//
// Failures are logged and skipped per-tenant rather than aborting Startup:
// one tenant's bad creds file (or a NATS hiccup while dialing it)
// shouldn't prevent every other tenant, or the service itself, from coming
// up.
func (h *Handlers) EnsureAllTenants(ctx context.Context) error {
	deps := h.deps()
	known, err := discoverTenants(deps.CredsDir)
	if err != nil {
		return err
	}
	for tenant, creds := range known {
		if _, err := h.ensureTenantResources(ctx, tenant, creds.CredsPath); err != nil {
			h.deps().Log.Error("ensure tenant resources at startup", "tenant", tenant, "err", err)
		}
	}
	return nil
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
//
// A no-op, not an error, if tenant isn't yet visible in CredsDir: the creds
// file write (accounts-service's handler.go createAccount) happens before
// the notify publish, so this shouldn't race it in practice, but staying
// defensive means a stray/duplicate delivery can't fail loudly — the next
// delivery, or an operator's own SwitchTenant, remains a fallback.
func (h *Handlers) EnsureTenantByName(ctx context.Context, tenant string) error {
	deps := h.deps()
	known, err := discoverTenants(deps.CredsDir)
	if err != nil {
		return err
	}
	creds, ok := known[tenant]
	if !ok {
		return nil
	}
	_, err = h.ensureTenantResources(ctx, tenant, creds.CredsPath)
	return err
}

// TeardownTenantByName is the mirror of EnsureTenantByName (BR-031;
// accounts-service's Handlers.publishAccountSuspended, BR-AC09, is the
// producer) — reacts to a tenant being suspended by stopping that tenant's
// persistent resource bundle instead of leaving it running (or, worse,
// leaving nats.go's default reconnect logic retrying forever against a
// .creds file suspendAccount has already deleted; see
// ARCHITECTURE-ACCOUNTS.md § 2t-a for the runtime behavior this closes).
//
// Explicitly closing res.nc is what actually stops the reconnect loop —
// NATS force-evicts the connection the instant the account is revoked at
// the resolver (also documented in § 2t-a), but an evicted connection still
// retries on its own by default; only an explicit Close() from this side
// disables that. Stopping the projectors and the browserrpc adapter first
// is not strictly required for that (closing nc alone would stop them
// eventually via read errors), but avoids each one logging a burst of
// errors against a connection this function already knows is being torn
// down deliberately.
//
// A no-op, not an error, if tenant was never provisioned (nothing in
// TenantResources) or has already been torn down — mirrors
// EnsureTenantByName's own idempotency, and for the same reason: a stray or
// duplicate notify.accounts.account.suspended delivery must not fail loudly.
//
// Deliberately does not touch deps.Tenant/TenantNC even if tenant happens to
// be REST's currently-active tenant — SwitchTenant already does not stop or
// rebuild anything (see tenantResources's doc comment), and there is no
// precedent here for this function auto-switching REST away from a tenant
// an operator explicitly selected. A REST/SSE request against a suspended
// active tenant will simply start failing against the closed connection,
// which is the correct outcome for a tenant that no longer exists.
func (h *Handlers) TeardownTenantByName(_ context.Context, tenant string) error {
	deps := h.deps()
	res, ok := deps.TenantResources[tenant]
	if !ok {
		return nil
	}

	stopAll(res.projectors)
	if err := res.rpcAdapter.Stop(); err != nil {
		deps.Log.Error("stop rpc adapter during tenant teardown", "tenant", tenant, "err", err)
	}
	res.nc.Close()

	// Copy-on-write into the shared map, re-reading h.deps() rather than
	// reusing deps — mirrors ensureTenantResources's own race-avoidance
	// comment (a sibling Ensure/Teardown call for a DIFFERENT tenant may be
	// running concurrently).
	latest := h.deps()
	newMap := make(map[string]*tenantResources, len(latest.TenantResources))
	for k, v := range latest.TenantResources {
		if k != tenant {
			newMap[k] = v
		}
	}
	latest.TenantResources = newMap
	h.SetDeps(latest)

	return nil
}

// registerProjectors starts the four projector durables against js and
// returns their ConsumeContexts. Unlike before Phase 15, the caller never
// stops these — see tenantResources's doc comment.
//
// nc is this tenant's own connection, threaded through to RegisterShapeA/
// RegisterContainers/RegisterMeta (Phase 15b) so their notify.* publishes
// land on the SAME account a browser connected to this tenant is listening
// on — never a shared/PLATFORM connection, which would publish into the
// wrong (or an inaccessible) account entirely.
func registerProjectors(
	ctx context.Context,
	js jetstream.JetStream,
	kvA, kvB, kvContainers, kvMeta *kvstore.Store,
	nc *nats.Conn,
	shipRepo domain.ShipRepository,
	containerRepo domain.ContainerRepository,
	log *slog.Logger,
) ([]jetstream.ConsumeContext, error) {
	var out []jetstream.ConsumeContext

	ccA, err := eventhandler.RegisterShapeA(ctx, js, kvA, nc, log)
	if err != nil {
		return nil, err
	}
	out = append(out, ccA)

	ccB, err := eventhandler.RegisterShapeB(ctx, js, kvB, shipRepo, log)
	if err != nil {
		stopAll(out)
		return nil, err
	}
	out = append(out, ccB)

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
