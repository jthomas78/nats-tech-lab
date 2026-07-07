// Package rest exposes the dictionary module over HTTP.
//
// Routes:
//
//	POST   /api/entries                                     create entry (fires event)
//	PUT    /api/entries/{context}/{entityType}/{id}         update entry (fires event)
//	GET    /api/shape-a/entries/{context}                   list Shape A read model
//	GET    /api/shape-a/entries/{context}/{entityType}/{id} read from KV read model
//	GET    /api/shape-b/entries/{context}                   list canonical Postgres projection
//	GET    /api/shape-b/entries/{context}/{entityType}/{id} read via KV cache → Postgres
//	DELETE /api/shape-b/cache/{context}/{entityType}/{id}   evict cache key (demo the miss path)
//	GET    /api/watch/{context}                             SSE stream of KV changes, both shapes
//	GET    /api/jetstream/watch                             SSE stream of raw JetStream messages (live, DeliverNew)
//	GET    /api/jetstream/stream                            SSE stream of all JetStream messages (replay + live, DeliverAll)
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
	commands *commands.Handler
	shapeA   *queries.ShapeA
	shapeB   *queries.ShapeB
	kvA      *kvstore.Store
	kvB      *kvstore.Store
	js       jetstream.JetStream
	log      *slog.Logger
}

func NewHandlers(cmd *commands.Handler, shapeA *queries.ShapeA, shapeB *queries.ShapeB, kvA, kvB *kvstore.Store, js jetstream.JetStream, log *slog.Logger) *Handlers {
	return &Handlers{commands: cmd, shapeA: shapeA, shapeB: shapeB, kvA: kvA, kvB: kvB, js: js, log: log}
}

func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/entries", h.createEntry)
	mux.HandleFunc("PUT /api/entries/{context}/{entityType}/{id}", h.updateEntry)
	mux.HandleFunc("GET /api/shape-a/entries/{context}", h.listShapeA)
	mux.HandleFunc("GET /api/shape-a/entries/{context}/{entityType}/{id}", h.getShapeA)
	mux.HandleFunc("GET /api/shape-b/entries/{context}", h.listShapeB)
	mux.HandleFunc("GET /api/shape-b/entries/{context}/{entityType}/{id}", h.getShapeB)
	mux.HandleFunc("DELETE /api/shape-b/cache/{context}/{entityType}/{id}", h.evictShapeBCache)
	mux.HandleFunc("GET /api/watch/{context}", h.watch)
	mux.HandleFunc("GET /api/jetstream/watch", h.watchJetStream)
	mux.HandleFunc("GET /api/jetstream/stream", h.replayJetStream)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func (h *Handlers) createEntry(w http.ResponseWriter, r *http.Request) {
	var in commands.EntryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	entry, err := h.commands.CreateEntry(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 202: the command is accepted, projections update asynchronously.
	writeJSON(w, http.StatusAccepted, map[string]any{"entry": entry})
}

func (h *Handlers) updateEntry(w http.ResponseWriter, r *http.Request) {
	var in commands.EntryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Context = r.PathValue("context")
	in.EntityType = r.PathValue("entityType")
	in.ID = r.PathValue("id")
	entry, err := h.commands.UpdateEntry(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"entry": entry})
}

func (h *Handlers) getShapeA(w http.ResponseWriter, r *http.Request) {
	entry, revision, err := h.shapeA.GetEntry(r.Context(), r.PathValue("context"), r.PathValue("entityType"), r.PathValue("id"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": entry, "revision": revision, "source": "kv"})
}

func (h *Handlers) listShapeA(w http.ResponseWriter, r *http.Request) {
	entries, err := h.shapeA.ListEntries(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *Handlers) getShapeB(w http.ResponseWriter, r *http.Request) {
	entry, cacheHit, err := h.shapeB.GetEntry(r.Context(), r.PathValue("context"), r.PathValue("entityType"), r.PathValue("id"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	source := "postgres"
	if cacheHit {
		source = "kv-cache"
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": entry, "cacheHit": cacheHit, "source": source})
}

func (h *Handlers) listShapeB(w http.ResponseWriter, r *http.Request) {
	entries, err := h.shapeB.ListEntries(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *Handlers) evictShapeBCache(w http.ResponseWriter, r *http.Request) {
	err := h.shapeB.EvictCacheEntry(r.Context(), r.PathValue("context"), r.PathValue("entityType"), r.PathValue("id"))
	if err != nil {
		h.writeQueryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) writeQueryError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "entry not found")
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
