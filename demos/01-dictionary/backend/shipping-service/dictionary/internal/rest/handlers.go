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
//	GET    /api/shape-c/fleet                         reconstruct fleet + containers from JetStream replay
//	GET    /api/watch/{context}                       SSE stream of ship KV changes, both shapes
//	GET    /api/watch-terminal/{context}              SSE stream of container + meta KV changes
//	GET    /api/kv/buckets                             every KV bucket registered on the NATS server (+ status)
//	GET    /api/kv/buckets/{bucket}/watch              SSE: bucket contents snapshot then live changes
//	GET    /api/jetstream/streams                      names of every stream registered on the NATS server
//	GET    /api/jetstream/watch                       SSE stream of live JetStream messages (DeliverNew)
//	GET    /api/jetstream/stream                      SSE stream of all JetStream messages (DeliverAll)
//	GET    /api/rpc-watch                              SSE stream of obs.rpc.* dual-transport RPC traffic (Phase 12.10)
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/refdataconsumer"
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

type refdataDemoResponse struct {
	Code        string `json:"code"`
	Status      string `json:"status"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"` // "kv-cache" | "api-refetch"
}

type refdataItemsResponse struct {
	Items []refdataDemoResponse `json:"items"`
}

// Deps bundles everything the HTTP layer needs; keeps NewHandlers readable as
// the module grows.
type Deps struct {
	Ships      *commands.ShipHandler
	Containers *commands.ContainerHandler
	Ports      *commands.PortHandler
	ShapeB     *queries.ShapeB
	ShapeC     *queries.ShapeC
	Terminal   *queries.Terminal
	Meta       *queries.Meta
	KVA        *kvstore.Store            // Shape A ship read model
	KVB        *kvstore.Store            // Shape B ship cache
	KVCont     *kvstore.Store            // container projection
	KVMeta     *kvstore.Store            // meta.* lookup sets
	Refdata    *refdataconsumer.Consumer // rpc.*-only cross-service consumer (BR-D08) — no KV dep
	JS         jetstream.JetStream
	NC         *nats.Conn // raw core-NATS conn (Phase 12.10 — obs.rpc.* SSE bridge)
	Log        *slog.Logger
}

type Handlers struct {
	deps Deps
}

func NewHandlers(deps Deps) *Handlers {
	return &Handlers{deps: deps}
}

func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ships/arrive", h.arrivePort)
	mux.HandleFunc("POST /api/ships/depart", h.departPort)
	mux.HandleFunc("POST /api/ships/register", h.registerShip)
	mux.HandleFunc("POST /api/ships/correct-id", h.correctShipID)
	mux.HandleFunc("POST /api/containers/register", h.registerContainer)
	mux.HandleFunc("POST /api/containers/load", h.loadContainer)
	mux.HandleFunc("POST /api/containers/unload", h.unloadContainer)
	mux.HandleFunc("GET /api/containers/{context}", h.listContainers)
	mux.HandleFunc("GET /api/terminal/{context}/{port}", h.terminalByPort)
	mux.HandleFunc("GET /api/manifest/{context}/{shipID}", h.manifestByShip)
	mux.HandleFunc("GET /api/ports/{context}", h.listPorts)
	mux.HandleFunc("POST /api/ports", h.registerPort)
	mux.HandleFunc("GET /api/admin/ports/{context}", h.adminPortsTable)
	mux.HandleFunc("GET /api/meta/{context}/known-containers", h.knownContainers)
	mux.HandleFunc("GET /api/shape-b/ships/{context}/{shipID}", h.getShipShapeB)
	mux.HandleFunc("DELETE /api/shape-b/cache/{context}/{shipID}", h.evictShipCache)
	mux.HandleFunc("GET /api/shape-c/fleet", h.getFleet)
	mux.HandleFunc("GET /api/watch/{context}", h.watch)
	mux.HandleFunc("GET /api/watch-terminal/{context}", h.watchTerminal)
	mux.HandleFunc("GET /api/kv/buckets", h.listKVBuckets)
	mux.HandleFunc("GET /api/kv/buckets/{bucket}/watch", h.watchKVBucket)
	mux.HandleFunc("GET /api/jetstream/streams", h.listStreams)
	mux.HandleFunc("GET /api/jetstream/watch", h.watchJetStream)
	mux.HandleFunc("GET /api/jetstream/stream", h.replayJetStream)
	mux.HandleFunc("GET /api/refdata-demo/{context}/{type}/{code}", h.getRefdataDemo)
	mux.HandleFunc("GET /api/refdata/types/{type}", h.listRefdataType)
	mux.HandleFunc("GET /api/refdata/locales", h.listRefdataLocales)
	mux.HandleFunc("GET /api/refdata-watch", h.watchRefdata)
	mux.HandleFunc("GET /api/rpc-watch", h.watchRPCObs)
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
	state, err := h.deps.Ships.ArrivePort(r.Context(), in)
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
	state, err := h.deps.Ships.DepartPort(r.Context(), in)
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
	state, err := h.deps.Ships.RegisterShip(r.Context(), in)
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
	state, err := h.deps.Ships.CorrectShipID(r.Context(), in)
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
	h.containerCommand(w, r, h.deps.Containers.RegisterContainer)
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
	h.containerCommand(w, r, h.deps.Containers.LoadContainer)
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
	h.containerCommand(w, r, h.deps.Containers.UnloadContainer)
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
// @Param        context  path      string  true  "Fleet context (e.g. global)"
// @Success      200      {object}  containersResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/containers/{context} [get]
func (h *Handlers) listContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := h.deps.Terminal.List(r.Context(), r.PathValue("context"))
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
	containers, err := h.deps.Terminal.ListByPort(r.Context(), r.PathValue("context"), r.PathValue("port"))
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
	containers, err := h.deps.Terminal.ListByShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
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
	ports, err := h.deps.Ports.List(r.Context(), r.PathValue("context"))
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
	if err := h.deps.Ports.Register(r.Context(), in.Context, in.Name); err != nil {
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
	rows, err := h.deps.Ports.ListRecords(r.Context(), r.PathValue("context"))
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
	ids, err := h.deps.Meta.KnownContainers(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": ids})
}

// ─── Shape B / Shape C ────────────────────────────────────────────────────────

// getShipShapeB godoc
//
// @Summary      Get ship (Shape B — KV cache → Postgres)
// @Description  Returns current ship state from the Shape B read model: checks KV cache first, falls through to Postgres on a miss and backfills the cache.
// @Tags         shape-b
// @Produce      json
// @Param        context  path      string  true  "Fleet context (e.g. global, atlantic-fleet)"
// @Param        shipID   path      string  true  "Ship identifier (e.g. orient-express)"
// @Success      200      {object}  shipBResponse
// @Failure      404      {object}  errorResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/shape-b/ships/{context}/{shipID} [get]
func (h *Handlers) getShipShapeB(w http.ResponseWriter, r *http.Request) {
	state, cacheHit, err := h.deps.ShapeB.GetShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
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

// getRefdataDemo godoc
//
// @Summary      Get a reference-data item (Phase 11.3 cross-service consumer demo)
// @Description  Demonstrates the Q5 versioned-read protocol from the consuming side: reads the refdata-service's KV cache directly, falling through to its REST API (which also backfills the cache) on a miss or a stale entry.
// @Tags         refdata-demo
// @Produce      json
// @Param        context  path      string  true  "tenant/region context (e.g. emea-acme)"
// @Param        type     path      string  true  "dictionary type key (e.g. hazard-class)"
// @Param        code     path      string  true  "item code"
// @Success      200      {object}  refdataDemoResponse
// @Failure      404      {object}  errorResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata-demo/{context}/{type}/{code} [get]
func (h *Handlers) getRefdataDemo(w http.ResponseWriter, r *http.Request) {
	if h.deps.Refdata == nil {
		writeError(w, http.StatusInternalServerError, "refdata consumer not configured")
		return
	}
	result, err := h.deps.Refdata.Lookup(r.Context(), r.PathValue("context"), r.PathValue("type"), r.PathValue("code"), r.URL.Query().Get("locale"))
	if err != nil {
		writeRefdataError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refdataDemoResponse{Code: result.Code, Status: result.Status, Label: result.Label, Description: result.Description, Source: result.Source})
}

// writeRefdataError maps a refdataconsumer error to an HTTP response. Phase
// 12.11 (BR-D28) made rpc.* the consumer's only transport — no REST fallback
// inside refdataconsumer — so a sustained rpc.* outage on a cache miss now
// surfaces as ErrRPCUnavailable rather than always eventually succeeding via
// REST. That's a "the dependency is down, try again" condition, distinct
// from a genuine internal fault, so it maps to 503, not the generic 500.
func writeRefdataError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, refdataconsumer.ErrNotFound):
		writeError(w, http.StatusNotFound, "item not found")
	case errors.Is(err, refdataconsumer.ErrRPCUnavailable):
		writeError(w, http.StatusServiceUnavailable, "reference data temporarily unavailable")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// refdataContext is the fixed tenant/region context the shipping backend
// reads reference data under — it matches refdata-service's seed DefaultContext
// and is deliberately independent of the fleet context selector (global,
// atlantic-fleet, …), which scopes ship/container data, not reference data.
const refdataContext = "emea-acme"

// listRefdataType godoc
//
// @Summary      Resolve all items of a reference-data type (Phase 11.6)
// @Description  Returns every item of a dictionary type under the fixed refdata context, each with its label resolved for the requested locale (BR-D03 fallback). Read KV-first via the Q5 protocol; per-item source is "kv-cache" or "api-refetch".
// @Tags         refdata
// @Produce      json
// @Param        type    path   string  true   "dictionary type key (e.g. ship-status)"
// @Param        locale  query  string  false  "locale to resolve labels in (e.g. en, es)"
// @Success      200     {object}  refdataItemsResponse
// @Failure      500     {object}  errorResponse
// @Router       /api/refdata/types/{type} [get]
func (h *Handlers) listRefdataType(w http.ResponseWriter, r *http.Request) {
	if h.deps.Refdata == nil {
		writeError(w, http.StatusInternalServerError, "refdata consumer not configured")
		return
	}
	results, err := h.deps.Refdata.ResolveType(r.Context(), refdataContext, r.PathValue("type"), r.URL.Query().Get("locale"))
	if err != nil {
		writeRefdataError(w, err)
		return
	}
	items := make([]refdataDemoResponse, 0, len(results))
	for _, res := range results {
		items = append(items, refdataDemoResponse{Code: res.Code, Status: res.Status, Label: res.Label, Description: res.Description, Source: res.Source})
	}
	writeJSON(w, http.StatusOK, refdataItemsResponse{Items: items})
}

// listRefdataLocales godoc
//
// @Summary      List reference-data locales (Phase 11.6)
// @Description  Returns the locales registered for the fixed refdata context, for the frontend locale switcher.
// @Tags         refdata
// @Produce      json
// @Success      200  {object}  metaValuesResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/refdata/locales [get]
func (h *Handlers) listRefdataLocales(w http.ResponseWriter, r *http.Request) {
	if h.deps.Refdata == nil {
		writeError(w, http.StatusInternalServerError, "refdata consumer not configured")
		return
	}
	result, err := h.deps.Refdata.Locales(r.Context(), refdataContext)
	if err != nil {
		writeRefdataError(w, err)
		return
	}
	// defaultLocale travels with the list — BR-D32: the frontend switcher has
	// to show the default first and mark it, which it can't do without knowing
	// which one is default.
	writeJSON(w, http.StatusOK, map[string]any{
		"locales":       result.Locales,
		"defaultLocale": result.DefaultLocale,
	})
}

// evictShipCache godoc
//
// @Summary      Evict Shape B cache entry
// @Description  Deletes the KV cache entry for a ship in the Shape B bucket. The next read will miss and fall through to Postgres, demonstrating the cache-miss path.
// @Tags         shape-b
// @Param        context  path  string  true  "Fleet context"
// @Param        shipID   path  string  true  "Ship identifier"
// @Success      204  "Cache entry evicted"
// @Failure      404  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/shape-b/cache/{context}/{shipID} [delete]
func (h *Handlers) evictShipCache(w http.ResponseWriter, r *http.Request) {
	err := h.deps.ShapeB.EvictCacheShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getFleet godoc
//
// @Summary      Reconstruct fleet (Shape C — pure event sourcing)
// @Description  Replays the full JetStream event log from seq=1 and reconstructs current state: ship.* events fold into ShipAggregates, container.* events into ContainerAggregates, and each ship's manifest is the onShipID join. No KV or Postgres involved.
// @Tags         shape-c
// @Produce      json
// @Success      200  {object}  queries.FleetReconstruction
// @Failure      500  {object}  errorResponse
// @Router       /api/shape-c/fleet [get]
func (h *Handlers) getFleet(w http.ResponseWriter, r *http.Request) {
	result, err := h.deps.ShapeC.ReconstructFleet(r.Context())
	if err != nil {
		h.deps.Log.Error("reconstruct fleet", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ─── JetStream introspection ──────────────────────────────────────────────────

// listStreams godoc
//
// @Summary      List registered streams
// @Description  Names of every event stream registered on the NATS server (not just SHIPPING) — lets the frontend's stream picker reflect what's actually provisioned. KV_* streams are NATS' internal storage for KV buckets, not event streams a client watches for messages, so they're excluded.
// @Tags         streams
// @Produce      json
// @Success      200  {object}  metaValuesResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/jetstream/streams [get]
func (h *Handlers) listStreams(w http.ResponseWriter, r *http.Request) {
	lister := h.deps.JS.StreamNames(r.Context())
	names := []string{}
	for name := range lister.Name() {
		if strings.HasPrefix(name, "KV_") {
			continue
		}
		names = append(names, name)
	}
	if err := lister.Err(); err != nil {
		h.deps.Log.Error("list streams", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": names})
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
	h.deps.Log.Error("query failed", "err", err)
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
