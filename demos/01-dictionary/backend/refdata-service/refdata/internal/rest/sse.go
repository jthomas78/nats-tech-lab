package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// watchEvent is one SSE payload: a KV change in the refdata-{context} cache
// bucket — an item entry PUT/DEL, or a {type}._meta version bump.
type watchEvent struct {
	Key      string          `json:"key"`
	Op       string          `json:"op"` // PUT, DEL, PURGE
	Revision uint64          `json:"revision"`
	Value    json.RawMessage `json:"value,omitempty"`
}

func opString(op jetstream.KeyValueOp) string {
	switch op {
	case jetstream.KeyValueDelete:
		return "DEL"
	case jetstream.KeyValuePurge:
		return "PURGE"
	default:
		return "PUT"
	}
}

// @Summary      Cache KV watch stream (SSE)
// @Description  Server-Sent Events stream of NATS KV changes in the refdata-{context} cache bucket — item entries and {type}._meta version bumps. Replays current state first, then live updates. Live invalidation feed for a frontend cache-status widget.
// @Tags         streams
// @Produce      text/event-stream
// @Param        context  path      string  true  "tenant/region context"
// @Success      200      {string}  string  "SSE stream — data: {watchEvent JSON}"
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata-watch/{context} [get]
func (h *Handlers) watch(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "streaming unsupported"})
		return
	}
	if h.deps.KV == nil {
		h.writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "cache not available"})
		return
	}

	kvContext := r.PathValue("context")
	ctx := r.Context()

	watcher, err := h.deps.KV.Watch(ctx, kvContext)
	if err != nil {
		if h.deps.Log != nil {
			h.deps.Log.Error("kv watch", "context", kvContext, "err", err)
		}
		h.writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal error"})
		return
	}
	defer func() { _ = watcher.Stop() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	send := func(entry jetstream.KeyValueEntry) {
		if entry == nil {
			return // end-of-replay marker
		}
		event := watchEvent{Key: entry.Key(), Op: opString(entry.Operation()), Revision: entry.Revision()}
		if entry.Operation() == jetstream.KeyValuePut {
			event.Value = entry.Value()
		}
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				return
			}
			send(entry)
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		}
	}
}
