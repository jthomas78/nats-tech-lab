// Package rest exposes the shipping module over HTTP.
//
// Routes:
//
//	POST   /api/ships/arrive                          arrive at port (fires ship.arrived event)
//	POST   /api/ships/depart                          depart from port (fires ship.departed event)
//	POST   /api/ships/cargo/load                      load cargo (fires cargo.loaded event)
//	POST   /api/ships/cargo/unload                    unload cargo (fires cargo.unloaded event)
//	GET    /api/shape-b/ships/{context}/{shipID}      read ship via KV cache → Postgres
//	DELETE /api/shape-b/cache/{context}/{shipID}      evict cache key (demo the miss path)
//	GET    /api/shape-c/fleet                         reconstruct fleet from JetStream replay
//	GET    /api/watch/{context}                       SSE stream of KV changes, both shapes
//	GET    /api/jetstream/watch                       SSE stream of live JetStream messages (DeliverNew)
//	GET    /api/jetstream/stream                      SSE stream of all JetStream messages (DeliverAll)
package rest

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
	"github.com/nats-io/nats.go/jetstream"
)

type Handlers struct {
	ships  *commands.ShipHandler
	shapeB *queries.ShapeB
	shapeC *queries.ShapeC
	kvA    *kvstore.Store
	kvB    *kvstore.Store
	js     jetstream.JetStream
	log    *slog.Logger
}

func NewHandlers(
	ships *commands.ShipHandler,
	shapeB *queries.ShapeB,
	shapeC *queries.ShapeC,
	kvA, kvB *kvstore.Store,
	js jetstream.JetStream,
	log *slog.Logger,
) *Handlers {
	return &Handlers{ships: ships, shapeB: shapeB, shapeC: shapeC, kvA: kvA, kvB: kvB, js: js, log: log}
}

func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ships/arrive", h.arrivePort)
	mux.HandleFunc("POST /api/ships/depart", h.departPort)
	mux.HandleFunc("POST /api/ships/cargo/load", h.loadCargo)
	mux.HandleFunc("POST /api/ships/cargo/unload", h.unloadCargo)
	mux.HandleFunc("GET /api/shape-b/ships/{context}/{shipID}", h.getShipShapeB)
	mux.HandleFunc("DELETE /api/shape-b/cache/{context}/{shipID}", h.evictShipCache)
	mux.HandleFunc("GET /api/shape-c/fleet", h.getFleet)
	mux.HandleFunc("GET /api/watch/{context}", h.watch)
	mux.HandleFunc("GET /api/jetstream/watch", h.watchJetStream)
	mux.HandleFunc("GET /api/jetstream/stream", h.replayJetStream)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func (h *Handlers) arrivePort(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.ships.ArrivePort(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

func (h *Handlers) departPort(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.ships.DepartPort(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

func (h *Handlers) loadCargo(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.ships.LoadCargo(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

func (h *Handlers) unloadCargo(w http.ResponseWriter, r *http.Request) {
	var in commands.ShipInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state, err := h.ships.UnloadCargo(r.Context(), in)
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ship": state})
}

func (h *Handlers) getShipShapeB(w http.ResponseWriter, r *http.Request) {
	state, cacheHit, err := h.shapeB.GetShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
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

func (h *Handlers) evictShipCache(w http.ResponseWriter, r *http.Request) {
	err := h.shapeB.EvictCacheShip(r.Context(), r.PathValue("context"), r.PathValue("shipID"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) getFleet(w http.ResponseWriter, r *http.Request) {
	fleet, err := h.shapeC.ReconstructFleet(r.Context())
	if err != nil {
		h.log.Error("reconstruct fleet", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fleet": fleet})
}

// writeCommandError maps domain rule violations to 422 so the frontend can
// distinguish them from schema errors (400).
func (h *Handlers) writeCommandError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrAlreadyDocked) ||
		errors.Is(err, domain.ErrMustDepart) ||
		errors.Is(err, domain.ErrNotDocked) ||
		errors.Is(err, domain.ErrNotInPort) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func (h *Handlers) writeQueryError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "ship not found")
		return
	}
	h.log.Error("query failed", "err", err)
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
