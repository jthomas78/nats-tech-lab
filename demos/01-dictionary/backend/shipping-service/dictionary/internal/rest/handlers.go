// Package rest exposes the shipping module over HTTP.
//
// Routes:
//
//	POST   /api/ships/arrive                          arrive at port (fires ship.arrived event; mints a surrogate id implicitly on first arrival)
//	POST   /api/ships/depart                          depart from port (fires ship.departed event)
//	POST   /api/ships/register                        mint a ship's surrogate id explicitly (BR-021, optional — see arrive)
//	POST   /api/ships/correct-id                      rename a ship's shipID, preserving its surrogate id (BR-022)
//	POST   /api/containers/register                   register container in origin terminal (container.registered)
//	POST   /api/containers/load                       load container onto docked ship (container.loaded)
//	POST   /api/containers/unload                     unload container at destination (container.unloaded)
//	GET    /api/containers/{context}                  all containers in the context
//	GET    /api/terminal/{context}/{port}             containers in the terminal yard at a port
//	GET    /api/manifest/{context}/{shipID}           containers on a ship (the manifest join)
//	GET    /api/ports/{context}                       every port registered for the fleet context
//	POST   /api/ports                                 register a new port (context, name)
//	GET    /api/admin/ports/{context}                 raw ports table rows (name + createdAt) — admin Postgres Tables panel
//	GET    /api/meta/{context}/known-containers       every container ID ever registered
//	GET    /api/shape-b/ships/{context}/{shipID}      read ship via KV cache → Postgres
//	DELETE /api/shape-b/cache/{context}/{shipID}      evict cache key (demo the miss path)
//	GET    /api/tenant                                 active tenant + switchable tenant list (Phase 13b)
//	POST   /api/tenant/switch                          reconnect under a different tenant's NATS account (Phase 13b)
//
// Phase 30h moved the cross-account NATS/JetStream diagnostic routes
// (/api/kv/buckets*, /api/jetstream/streams, /api/jetstream/replay,
// /api/nats/connections, /api/nats/services, /api/nats/account-activity,
// /api/nats/log) to observability-service — none of it was shipping domain
// logic, and this service only ever held it because it was the one service
// with cross-account NATS reach at the time (Main-POC-Plan.md Phase 30).
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
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Swagger response envelope types — used only for OpenAPI schema generation.

type shipResponse struct {
	Ship domain.ShipState `json:"ship"`
}

type shipBResponse struct {
	Ship     domain.ShipState `json:"ship"`
	CacheHit bool             `json:"cacheHit"`
	Source   string           `json:"source"` // "kv-cache" or "postgres"
}

type containerResponse struct {
	Container domain.ContainerState `json:"container"`
}

type containersResponse struct {
	Containers []domain.ContainerState `json:"containers"`
}

type metaValuesResponse struct {
	Values []string `json:"values"`
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
	handle("POST /api/ships/arrive", h.arrivePort)
	handle("POST /api/ships/depart", h.departPort)
	handle("POST /api/ships/register", h.registerShip)
	handle("POST /api/ships/correct-id", h.correctShipID)
	handle("POST /api/containers/register", h.registerContainer)
	handle("POST /api/containers/load", h.loadContainer)
	handle("POST /api/containers/unload", h.unloadContainer)
	handle("GET /api/containers/{context}", h.listContainers)
	handle("GET /api/terminal/{context}/{port}", h.terminalByPort)
	handle("GET /api/manifest/{context}/{shipID}", h.manifestByShip)
	handle("GET /api/ports/{context}", h.listPorts)
	handle("POST /api/ports", h.registerPort)
	handle("GET /api/admin/ports/{context}", h.adminPortsTable)
	handle("GET /api/meta/{context}/known-containers", h.knownContainers)
	handle("GET /api/shape-b/ships/{context}/{shipID}", h.getShipShapeB)
	handle("DELETE /api/shape-b/cache/{context}/{shipID}", h.evictShipCache)
	handle("GET /api/tenant", h.getTenant)
	handle("POST /api/tenant/switch", h.switchTenant)
	// Phase 32 removed this service's five refdata relay routes
	// (/api/refdata-demo, /api/refdata/types, /api/refdata/locales,
	// /api/refdata/contexts, and the /api/refdata-watch SSE stream).
	// shipping-service was acting as an API conduit for another service's
	// data; refdata-service now serves browsers directly over its own
	// api.*.refdata.* subjects (BR-D41) with change notification over
	// notify.* (BR-D42).
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))
}

// ─── Ship commands ────────────────────────────────────────────────────────────

// arrivePort godoc
//
// @Summary      Arrive at port
// @Description  Ship arrives at a port. Validates domain rules (must not already be docked), then publishes a ship.arrived event to JetStream.
// @Tags         ships
// @Accept       json
// @Produce      json
// @Param        body  body      commands.ShipInput  true  "context, shipID, shipName (first arrival only), port"
// @Success      202   {object}  shipResponse
// @Failure      400   {object}  errorResponse
// @Failure      422   {object}  errorResponse  "Domain rule violation"
// @Router       /api/ships/arrive [post]
func (h *Handlers) arrivePort(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.deps().Ships.ArrivePort(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

// departPort godoc
//
// @Summary      Depart from port
// @Description  Ship departs from a port. Validates that the ship is currently docked at the named port, then publishes a ship.departed event.
// @Tags         ships
// @Accept       json
// @Produce      json
// @Param        body  body      commands.ShipInput  true  "context, shipID, port"
// @Success      202   {object}  shipResponse
// @Failure      400   {object}  errorResponse
// @Failure      422   {object}  errorResponse  "Domain rule violation"
// @Router       /api/ships/depart [post]
func (h *Handlers) departPort(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.deps().Ships.DepartPort(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

// registerShip godoc
//
// @Summary      Register ship
// @Description  Mints a ship's surrogate identity explicitly (BR-021: a shipID can only be registered once). Optional — ArrivePort mints one implicitly on a ship's first arrival. Publishes a ship.registered event.
// @Tags         ships
// @Accept       json
// @Produce      json
// @Param        body  body      commands.ShipInput  true  "context, shipID, shipName"
// @Success      202   {object}  shipResponse
// @Failure      400   {object}  errorResponse
// @Failure      422   {object}  errorResponse  "Domain rule violation"
// @Router       /api/ships/register [post]
func (h *Handlers) registerShip(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.deps().Ships.RegisterShip(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

// correctShipID godoc
//
// @Summary      Correct a ship's shipID
// @Description  Renames a registered ship's natural key (BR-022: newShipID must be valid and not currently in use by another ship in the context), preserving its surrogate identity. Publishes a ship.corrected event.
// @Tags         ships
// @Accept       json
// @Produce      json
// @Param        body  body      commands.ShipCorrectionInput  true  "context, shipID, newShipID"
// @Success      202   {object}  shipResponse
// @Failure      400   {object}  errorResponse
// @Failure      422   {object}  errorResponse  "Domain rule violation"
// @Router       /api/ships/correct-id [post]
func (h *Handlers) correctShipID(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipCorrectionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.deps().Ships.CorrectShipID(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

// ─── Container commands ───────────────────────────────────────────────────────

// registerContainer godoc
//
// @Summary      Register container
// @Description  Registers a new container in its origin port's terminal yard (BR-015: a container ID can only be registered once; BR-016: container ID must be TCKU + 7 digits). Publishes a container.registered event.
// @Tags         containers
// @Accept       json
// @Produce      json
// @Param        body  body      commands.ContainerInput  true  "context, containerID (ISO 6346), cargo, originPort, destPort"
// @Success      202   {object}  containerResponse
// @Failure      400   {object}  errorResponse
// @Failure      422   {object}  errorResponse  "Domain rule violation"
// @Router       /api/containers/register [post]
func (h *Handlers) registerContainer(w http.ResponseWriter, r *http.Request) {
	h.containerCommand(w, r, h.deps().Containers.RegisterContainer)
}

// loadContainer godoc
//
// @Summary      Load container onto ship
// @Description  Crane-loads a container onto a docked ship. Enforces BR-008, BR-010, BR-012 and BR-014 from one atomic replay of the SHIPPING stream (both aggregates share it in Phase 8). Publishes a container.loaded event.
// @Tags         containers
// @Accept       json
// @Produce      json
// @Param        body  body      commands.ContainerInput  true  "context, containerID, shipID"
// @Success      202   {object}  containerResponse
// @Failure      400   {object}  errorResponse
// @Failure      404   {object}  errorResponse  "Container not registered"
// @Failure      422   {object}  errorResponse  "Domain rule violation"
// @Router       /api/containers/load [post]
func (h *Handlers) loadContainer(w http.ResponseWriter, r *http.Request) {
	h.containerCommand(w, r, h.deps().Containers.LoadContainer)
}

// unloadContainer godoc
//
// @Summary      Unload container from ship
// @Description  Crane-unloads a container into the terminal at the ship's current port. Enforces BR-009, BR-011, BR-012 and BR-013. Publishes a container.unloaded event.
// @Tags         containers
// @Accept       json
// @Produce      json
// @Param        body  body      commands.ContainerInput  true  "context, containerID, shipID"
// @Success      202   {object}  containerResponse
// @Failure      400   {object}  errorResponse
// @Failure      404   {object}  errorResponse  "Container not registered"
// @Failure      422   {object}  errorResponse  "Domain rule violation"
// @Router       /api/containers/unload [post]
func (h *Handlers) unloadContainer(w http.ResponseWriter, r *http.Request) {
	h.containerCommand(w, r, h.deps().Containers.UnloadContainer)
}

func (h *Handlers) containerCommand(
	w http.ResponseWriter,
	r *http.Request,
	cmd func(context.Context, commands.ContainerInput) (domain.ContainerState, error),
) {
	var in commands.ContainerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := cmd(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"container": state})
}

// ─── Terminal / meta queries ──────────────────────────────────────────────────

// listContainers godoc
//
// @Summary      List all containers
// @Description  Returns every container state in the context's KV projection.
// @Tags         terminal
// @Produce      json
// @Param        context  path      string  true  "Fleet context (e.g. acme)"
// @Success      200      {object}  containersResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/containers/{context} [get]
func (h *Handlers) listContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := h.deps().Terminal.List(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
}

// terminalByPort godoc
//
// @Summary      Containers in a terminal yard
// @Description  Returns the containers currently in-terminal at the given port (terminalPort match — no status branching).
// @Tags         terminal
// @Produce      json
// @Param        context  path      string  true  "Fleet context"
// @Param        port     path      string  true  "Port name (e.g. Hamburg)"
// @Success      200      {object}  containersResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/terminal/{context}/{port} [get]
func (h *Handlers) terminalByPort(w http.ResponseWriter, r *http.Request) {
	containers, err := h.deps().Terminal.ListByPort(r.Context(), r.PathValue("context"), r.PathValue("port"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
}

// manifestByShip godoc
//
// @Summary      Ship manifest
// @Description  Returns the containers currently on the named ship (onShipID join) — the ship aggregate no longer carries a manifest itself.
// @Tags         terminal
// @Produce      json
// @Param        context  path      string  true  "Fleet context"
// @Param        shipID   path      string  true  "Ship identifier"
// @Success      200      {object}  containersResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/manifest/{context}/{shipID} [get]
func (h *Handlers) manifestByShip(w http.ResponseWriter, r *http.Request) {
	containers, err := h.deps().Terminal.ListByShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
}

// listPorts godoc
//
// @Summary      List registered ports
// @Description  Every port registered for the fleet context. Backed by the Postgres ports reference table (BR-017, BR-018) — plain master data, not event-derived.
// @Tags         ports
// @Produce      json
// @Param        context  path      string  true  "Fleet context"
// @Success      200      {object}  metaValuesResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/ports/{context} [get]
func (h *Handlers) listPorts(w http.ResponseWriter, r *http.Request) {
	ports, err := h.deps().Ports.List(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": ports})
}

// registerPortInput is the request body for registerPort.
type registerPortInput struct {
	Context string `json:"context"`
	Name    string `json:"name"`
}

// registerPort godoc
//
// @Summary      Register a port
// @Description  Adds a port to the fleet context's ports registry. Direct Postgres write, not an event — ports are reference data, not an event-sourced aggregate. Idempotent.
// @Tags         ports
// @Accept       json
// @Produce      json
// @Param        body  body      registerPortInput  true  "context, name"
// @Success      201   {object}  metaValuesResponse
// @Failure      400   {object}  errorResponse
// @Router       /api/ports [post]
func (h *Handlers) registerPort(w http.ResponseWriter, r *http.Request) {
	var in registerPortInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.deps().Ports.Register(r.Context(), in.Context, in.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"port": in.Name})
}

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

// knownContainers godoc
//
// @Summary      Known container IDs
// @Description  Every container ID ever registered in the context. Backed by the meta.known-containers KV projection.
// @Tags         meta
// @Produce      json
// @Param        context  path      string  true  "Fleet context"
// @Success      200      {object}  metaValuesResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/meta/{context}/known-containers [get]
func (h *Handlers) knownContainers(w http.ResponseWriter, r *http.Request) {
	ids, err := h.deps().Meta.KnownContainers(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": ids})
}

// ─── Shape B ──────────────────────────────────────────────────────────────────

// getShipShapeB godoc
//
// @Summary      Get ship (Shape B — KV cache → Postgres)
// @Description  Returns current ship state from the Shape B read model: checks KV cache first, falls through to Postgres on a miss and backfills the cache.
// @Tags         shape-b
// @Produce      json
// @Param        context  path      string  true  "Fleet context (e.g. acme, acme-atlantic-fleet)"
// @Param        shipID   path      string  true  "Ship identifier (e.g. orient-express)"
// @Success      200      {object}  shipBResponse
// @Failure      404      {object}  errorResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/shape-b/ships/{context}/{shipID} [get]
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

// @Tags         shape-b
// @Param        context  path  string  true  "Fleet context"
// @Param        shipID   path  string  true  "Ship identifier"
// @Success      204  "Cache entry evicted"
// @Failure      404  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/shape-b/cache/{context}/{shipID} [delete]
func (h *Handlers) evictShipCache(w http.ResponseWriter, r *http.Request) {
	err := h.deps().ShipReads.EvictCacheShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Error mapping ────────────────────────────────────────────────────────────

// writeCommandError maps domain rule violations to 422 so the frontend can
// distinguish them from schema errors (400) and missing entities (404).
func (h *Handlers) writeCommandError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrContainerNotFound), errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyDocked),
		errors.Is(err, domain.ErrMustDepart),
		errors.Is(err, domain.ErrNotDocked),
		errors.Is(err, domain.ErrNotInPort),
		errors.Is(err, domain.ErrContainerAtDestination),
		errors.Is(err, domain.ErrWrongDestination),
		errors.Is(err, domain.ErrContainerNotInTerminal),
		errors.Is(err, domain.ErrContainerNotOnShip),
		errors.Is(err, domain.ErrWrongShip),
		errors.Is(err, domain.ErrContainerNotAtPort),
		errors.Is(err, domain.ErrContainerExists),
		errors.Is(err, domain.ErrInvalidContainerID),
		errors.Is(err, domain.ErrUnknownPort),
		errors.Is(err, domain.ErrShipExists),
		errors.Is(err, domain.ErrShipIDInUse):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

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
