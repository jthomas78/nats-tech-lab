// Package rest exposes shipping-service's infra/admin HTTP surface — never
// business operations. BR-039 (Phase 33): every business operation (ship/
// container/terminal/manifest/ports/meta) is reachable only over
// api.*/rpc.* (see internal/browserrpc), never REST — this package now
// carries only health, admin diagnostics, and tenant-switch.
//
// Routes:
//
//	GET    /api/admin/ports/{context}                       raw ports table rows (name + createdAt) — admin Postgres Tables panel
//	GET    /api/admin/read-path/ships/{context}/{shipID}    read ship via KV cache → Postgres (Shape B diagnostics)
//	DELETE /api/admin/read-path/cache/{context}/{shipID}    evict cache key (demo the miss path)
//	GET    /api/tenant                                       active tenant + switchable tenant list (Phase 13b)
//	POST   /api/tenant/switch                                reconnect under a different tenant's NATS account (Phase 13b)
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
// phase, BR-039) — and renamed /api/shape-b/* to /api/admin/read-path/* to
// reflect that it was always an admin diagnostics panel, not a business
// route, just misclassified by name.
package rest

import (
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
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Swagger response envelope types — used only for OpenAPI schema generation.

type shipBResponse struct {
	Ship     domain.ShipState `json:"ship"`
	CacheHit bool             `json:"cacheHit"`
	Source   string           `json:"source"` // "kv-cache" or "postgres"
}

type errorResponse struct {
	Error string `json:"error"`
}

// TenantCredentials is one tenant's NATS account login: a path to a .creds
// file (Phase 14a — operator mode; must match a JWT/user minted into
// nats/creds/ by nats/bootstrap-operator.sh). Replaces Phase 13b's bare
// user/password pair now that nats.conf has no static accounts{} block to
// match against.
type TenantCredentials struct {
	CredsPath string
}

// Deps bundles everything the HTTP layer needs; keeps NewHandlers readable as
// the module grows.
//
// Phase 13b splits this into two lifetimes. Ports, NC,
// ShipRepo, ContainerRepo, PortRepo, NatsURL, CredsDir, and Log are set
// once at Startup and never change. Ships, Containers, ShipReads,
// Terminal, Meta, KVCont, KVMeta, JS, TenantNC, and Tenant mirror
// whichever tenant is currently REST/SSE's active selection — SwitchTenant
// points all of them at that tenant's persistent bundle (TenantResources,
// Phase 15, see tenant.go's tenantResources doc comment) and replaces the
// whole struct atomically via Handlers.SetDeps, so no request ever observes
// a half-swapped mix of old and new tenant resources. Unlike before Phase
// 15, switching away from a tenant no longer stops or discards anything —
// every tenant's own bundle in TenantResources keeps running so its
// api.*/notify.* traffic keeps working regardless of which tenant these
// mirror fields currently point at.
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
	Tenant   string     // currently active tenant account name (which TenantResources entry REST/SSE fields below mirror)
	TenantNC *nats.Conn // the active tenant's connection — mirrors TenantResources[Tenant].nc, never drained (Phase 15, see tenant.go's tenantResources doc comment)
	// TenantResources holds every tenant's persistent connection, JetStream
	// context, KV stores, command/query handlers, durable projectors, and
	// browserrpc.Adapter (Phase 15a, renamed from natsrpc in Phase 16b) —
	// keyed by tenant name, created once via tenant.go's
	// ensureTenantResources and never torn down. SwitchTenant only changes
	// which entry the Ships/Containers/ShipReads/.../JS/TenantNC fields above
	// mirror; every other tenant's bundle keeps running so its
	// browser-facing api.*/notify.* traffic keeps working regardless of
	// which single tenant REST/SSE currently has active.
	TenantResources map[string]*tenantResources
	ShipRepo        domain.ShipRepository      // static: Postgres, not account-scoped
	ContainerRepo   domain.ContainerRepository // static: Postgres, not account-scoped
	PortRepo        domain.PortRepository      // static: Postgres, not account-scoped
	NatsURL         string                     // static: dial target for SwitchTenant's reconnect
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
}

func NewHandlers(deps Deps) *Handlers {
	h := &Handlers{}
	h.depsPtr.Store(&deps)
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

func (h *Handlers) Mount(mux *http.ServeMux) {
	handle := func(pattern string, fn http.HandlerFunc) {
		mux.HandleFunc(pattern, h.httpTraceMiddleware(fn))
	}
	handle("GET /api/admin/ports/{context}", h.adminPortsTable)
	handle("GET /api/admin/read-path/ships/{context}/{shipID}", h.getShipShapeB)
	handle("DELETE /api/admin/read-path/cache/{context}/{shipID}", h.evictShipCache)
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
	// see the package doc comment above (BR-039) — and renamed
	// /api/shape-b/* to /api/admin/read-path/* above.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))
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

// ─── Admin — read-path diagnostics (Shape B; renamed from /api/shape-b/*) ─────

// getShipShapeB godoc
//
// @Summary      Get ship (admin read-path diagnostics — KV cache → Postgres)
// @Description  Returns current ship state from the Shape B read model: checks KV cache first, falls through to Postgres on a miss and backfills the cache. Admin diagnostics only, not a business route.
// @Tags         admin
// @Produce      json
// @Param        context  path      string  true  "Fleet context (e.g. acme, acme-atlantic-fleet)"
// @Param        shipID   path      string  true  "Ship identifier (e.g. orient-express)"
// @Success      200      {object}  shipBResponse
// @Failure      404      {object}  errorResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/admin/read-path/ships/{context}/{shipID} [get]
func (h *Handlers) getShipShapeB(w http.ResponseWriter, r *http.Request) {
	state, cacheHit, err := h.deps().ShipReads.GetShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	source := "postgres"
	if cacheHit {
		source = "kv-cache"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ship": state, "cacheHit": cacheHit, "source": source})
}

// evictShipCache godoc
//
// @Summary      Evict ship cache entry (admin read-path diagnostics)
// @Description  Evicts the ship's KV cache entry to demonstrate the cache-miss → Postgres fallthrough → backfill path. Admin diagnostics only, not a business route.
// @Tags         admin
// @Param        context  path  string  true  "Fleet context"
// @Param        shipID   path  string  true  "Ship identifier"
// @Success      204  "Cache entry evicted"
// @Failure      404  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/admin/read-path/cache/{context}/{shipID} [delete]
func (h *Handlers) evictShipCache(w http.ResponseWriter, r *http.Request) {
	err := h.deps().ShipReads.EvictCacheShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
