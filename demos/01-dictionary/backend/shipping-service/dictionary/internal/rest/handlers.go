// Package rest exposes shipping-service's infra/admin HTTP surface — never
// business operations. BR-039 (Phase 33): every business operation (ship/
// container/terminal/manifest/ports/meta) is reachable only over
// api.*/rpc.* (see internal/browserrpc), never REST — this package now
// carries only health, admin diagnostics, and tenant-switch.
//
// Routes:
//
//	GET    /api/admin/ports/{context}    raw ports table rows (name + createdAt) — admin Postgres Tables panel
//	GET    /api/tenant                    active tenant + switchable tenant list (Phase 13b)
//	POST   /api/tenant/switch             reconnect under a different tenant's NATS account (Phase 13b)
//
// Phase 30h moved the cross-account NATS/JetStream diagnostic routes
// (/api/kv/buckets*, /api/jetstream/streams, /api/jetstream/replay,
// /api/nats/connections, /api/nats/services, /api/nats/account-activity,
// /api/nats/log) to observability-service — none of it was shipping domain
// logic, and this service only ever held it because it was the one service
// with cross-account NATS reach at the time (Main-POC-Plan.md Phase 30).
//
// Phase 33 deleted this package's business routes (/api/ships/*,
// /api/containers/*, /api/terminal/*, /api/manifest/*, /api/ports/*,
// /api/meta/*) outright — every one of them already had an api.* equivalent
// in internal/browserrpc (the last gap, GET /api/manifest/{context}/{shipID},
// was closed by adding api.*.shipping.container.manifest.v1 in the same
// phase, BR-039). The admin read-path diagnostics route it renamed that same
// phase (/api/shape-b/* to /api/admin/read-path/*) was itself retired along
// with the CQRS Shapes admin panel it existed for — see BUSINESS_RULES-
// SHIPPING.md's BR-038/BR-039 for the KV-cache/Postgres-fallthrough/backfill
// behavior those routes exposed, still covered directly at the query layer.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/shared/natstenants"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Swagger response envelope types — used only for OpenAPI schema generation.

type errorResponse struct {
	Error string `json:"error"`
}

// Deps bundles everything the HTTP layer needs; keeps NewHandlers readable as
// the module grows.
//
// Phase 13b splits this into two lifetimes. Ports, NC,
// ShipRepo, ContainerRepo, PortRepo, NatsURL, CredsDir, and Log are set
// once at Startup and never change. Ships, Containers, ShipReads,
// Terminal, Meta, KVCont, KVMeta, JS, TenantNC, and Tenant mirror
// whichever tenant is currently REST/SSE's active selection — SwitchTenant
// points all of them at that tenant's persistent bundle (held by
// Handlers.mgr, a shared/natstenants.Manager — see tenant.go's
// tenantResources doc comment) and replaces the whole struct atomically via
// Handlers.SetDeps, so no request ever observes a half-swapped mix of old
// and new tenant resources. Unlike before Phase 15, switching away from a
// tenant no longer stops or discards anything — every tenant's own bundle
// in Handlers.mgr keeps running so its api.*/notify.* traffic keeps working
// regardless of which tenant these mirror fields currently point at.
type Deps struct {
	Ships      *commands.ShipHandler
	Containers *commands.ContainerHandler
	Ports      *commands.PortHandler
	ShipReads  *queries.Ships
	Terminal   *queries.Terminal
	Meta       *queries.Meta
	KVCont     *kvstore.Store      // container projection
	KVMeta     *kvstore.Store      // meta.* lookup sets
	JS         jetstream.JetStream // tenant-scoped: SHIPPING stream, ship/container KV
	NC         *nats.Conn          // permanent PLATFORM-account conn (Phase 12.10 — obs.rpc.* SSE bridge)
	Log        *slog.Logger

	// Tenant-switch plumbing (Phase 13b) — shipping-service only; refdata-service
	// stays on PLATFORM (see Main-POC-Plan.md Phase 13b, cost #3).
	Tenant        string                     // currently active tenant account name (which tenant's Manager-held bundle REST/SSE fields below mirror)
	TenantNC      *nats.Conn                 // the active tenant's connection — mirrors that tenant's tenantResources.nc, never drained (Phase 15, see tenant.go's tenantResources doc comment)
	ShipRepo      domain.ShipRepository      // static: Postgres, not account-scoped
	ContainerRepo domain.ContainerRepository // static: Postgres, not account-scoped
	PortRepo      domain.PortRepository      // static: Postgres, not account-scoped
	// NatsURL is a dial target read once, at NewHandlers, to construct
	// Handlers.mgr (shared/natstenants.Manager) — every tenant connection
	// it makes for SwitchTenant/EnsureAllTenants/EnsureTenantByName dials
	// this same URL, never re-read afterward.
	NatsURL string
	// CredsDir replaces Phase 13b's static TenantCreds map (Phase 14b): the
	// shared nats-creds volume accounts-service also writes into. Scanned
	// fresh on every GET /api/tenant and every switch (see tenant.go's
	// discoverTenants) — small directory, and it's the only way a tenant
	// minted by accounts-service after this process started ever becomes
	// visible without a restart.
	CredsDir string
}

type Handlers struct {
	depsPtr atomic.Pointer[Deps]
	// mgr holds every tenant's persistent connection + tenantResources
	// bundle (shared/natstenants.Manager, Phase 35 extraction — see
	// tenant.go's tenantResources doc comment for why shipping-service
	// adopted it). Built once in NewHandlers from Deps' static NatsURL/
	// CredsDir/Log/repositories; never swapped, unlike depsPtr.
	mgr *natstenants.Manager[*tenantResources]
}

func NewHandlers(deps Deps) *Handlers {
	h := &Handlers{}
	h.depsPtr.Store(&deps)
	h.mgr = natstenants.NewManager(deps.NatsURL, deps.CredsDir, "shipping-service", deps.Log,
		func(ctx context.Context, nc *nats.Conn, tenant string) (*tenantResources, error) {
			return buildTenantResources(ctx, nc, tenant, deps)
		},
		func(tenant string, res *tenantResources) error {
			return teardownTenantResources(tenant, res, deps.Log)
		},
	)
	return h
}

// deps returns a snapshot of the current dependency bundle. Called once per
// field access from handler bodies (h.deps().X) — cheap, since it's a
// pointer load plus a struct copy of pointers/interfaces, not a deep clone.
func (h *Handlers) deps() Deps {
	return *h.depsPtr.Load()
}

// SetDeps atomically replaces the entire dependency bundle. Phase 13b's
// SwitchTenant is the only caller — it builds a full new Deps value (tenant
// fields rebuilt, static fields carried over unchanged) and swaps it in with
// one atomic store, so no in-flight request ever sees a mix of old and new
// tenant resources.
func (h *Handlers) SetDeps(deps Deps) {
	h.depsPtr.Store(&deps)
}

// Mount registers this package's routes on mux and returns the exact list of
// patterns registered (BR-040) — a hardcoded allowlist test asserts this
// list ConsistOf its expected contents, so a business route added later to
// this function fails that test rather than only a code review.
func (h *Handlers) Mount(mux *http.ServeMux) []string {
	var routes []string
	handle := func(pattern string, fn http.HandlerFunc) {
		routes = append(routes, pattern)
		mux.HandleFunc(pattern, h.httpTraceMiddleware(fn))
	}
	handle("GET /api/admin/ports/{context}", h.adminPortsTable)
	handle("GET /api/tenant", h.getTenant)
	handle("POST /api/tenant/switch", h.switchTenant)
	// Phase 32 removed this service's five refdata relay routes
	// (/api/refdata-demo, /api/refdata/types, /api/refdata/locales,
	// /api/refdata/contexts, and the /api/refdata-watch SSE stream).
	// shipping-service was acting as an API conduit for another service's
	// data; refdata-service now serves browsers directly over its own
	// api.*.refdata.* subjects (BR-D41) with change notification over
	// notify.* (BR-D42).
	//
	// Phase 33 deleted /api/ships/*, /api/containers/*, /api/terminal/*,
	// /api/manifest/*, /api/ports/* (GET+POST), and /api/meta/* outright —
	// see the package doc comment above (BR-039).
	routes = append(routes, "GET /healthz")
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	routes = append(routes, "/swagger/")
	mux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))
	return routes
}

// ─── Admin — ports table ──────────────────────────────────────────────────────

// portsTableResponse is the Swagger response envelope for adminPortsTable.
type portsTableResponse struct {
	Rows []domain.PortRecord `json:"rows"`
}

// adminPortsTable godoc
//
// @Summary      Raw ports table (admin)
// @Description  Every row of the Postgres ports table for the fleet context — name and registration time. Backs the admin "Postgres Tables" panel; distinct from GET /api/ports/{context}, which returns names only for dropdowns.
// @Tags         admin
// @Produce      json
// @Param        context  path      string  true  "Fleet context"
// @Success      200      {object}  portsTableResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/admin/ports/{context} [get]
func (h *Handlers) adminPortsTable(w http.ResponseWriter, r *http.Request) {
	rows, err := h.deps().Ports.ListRecords(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

// ─── Error mapping ────────────────────────────────────────────────────────────

func (h *Handlers) writeQueryError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrContainerNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.deps().Log.Error("query failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
