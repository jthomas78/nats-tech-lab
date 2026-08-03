package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// kvBucket is one registered KV bucket's run-time status — the bucket rail's
// row shape. Values counts every message including historical revisions;
// ttlSeconds is 0 when keys never expire.
type kvBucket struct {
	Bucket       string `json:"bucket"`
	Values       uint64 `json:"values"`
	History      int64  `json:"history"`
	Bytes        uint64 `json:"bytes"`
	TTLSeconds   int64  `json:"ttlSeconds"`
	BackingStore string `json:"backingStore"`
}

type kvBucketsResponse struct {
	Buckets []kvBucket `json:"buckets"`
}

// listKVBuckets godoc
//
// @Summary      List KV buckets
// @Description  Every Key-Value bucket registered on the NATS server, with run-time status (value count, history depth, size, TTL, backing store). Backs the KV inspector's bucket rail — the raw NATS KV stores this lab provisions (one per role per tenant: dict-a, dict-b, container, meta; plus refdata-service's cache). Each bucket holds keys for all business-unit contexts, prefixed as {context}.{entityType}.{id}.
// @Tags         kv
// @Produce      json
// @Success      200  {object}  kvBucketsResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/kv/buckets [get]
func (h *Handlers) listKVBuckets(w http.ResponseWriter, r *http.Request) {
	lister := h.deps().JS.KeyValueStores(r.Context())
	buckets := []kvBucket{}
	for status := range lister.Status() {
		buckets = append(buckets, kvBucket{
			Bucket:       status.Bucket(),
			Values:       status.Values(),
			History:      status.History(),
			Bytes:        status.Bytes(),
			TTLSeconds:   int64(status.TTL() / time.Second),
			BackingStore: status.BackingStore(),
		})
	}
	if err := lister.Error(); err != nil {
		h.deps().Log.Error("list kv buckets", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Stable alphabetical order so the rail doesn't reshuffle between polls;
	// the frontend groups by prefix family for display.
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Bucket < buckets[j].Bucket })
	writeJSON(w, http.StatusOK, kvBucketsResponse{Buckets: buckets})
}

// kvChange is one SSE payload from a bucket watch. During the initial replay
// Live is false (these events reconstruct the bucket's current contents);
// once the snapshot completes an INIT_DONE control event is sent and every
// subsequent change has Live=true (these are the "how it was updated" feed).
type kvChange struct {
	Key      string          `json:"key"`
	Op       string          `json:"op"` // PUT, DEL, PURGE, INIT_DONE
	Revision uint64          `json:"revision"`
	Created  time.Time       `json:"created,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	Live     bool            `json:"live"`
}

// watchKVBucket godoc
//
// @Summary      Watch a KV bucket (SSE)
// @Description  Server-Sent Events stream for one KV bucket by full name. Replays the bucket's current entries first (Live=false), sends an INIT_DONE control event, then streams live changes (Live=true). One connection drives both the contents snapshot and the live update feed — the same WatchAll semantics the NATS KV watch model is built on.
// @Tags         kv
// @Produce      text/event-stream
// @Param        bucket  path  string  true  "KV bucket name (e.g. dict-a, dict-b, container, meta, refdata-acme)"
// @Success      200  {string}  string  "SSE stream — data: {kvChange JSON}"
// @Failure      400  {object}  errorResponse  "Unknown bucket"
// @Failure      500  {object}  errorResponse
// @Router       /api/kv/buckets/{bucket}/watch [get]
func (h *Handlers) watchKVBucket(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ctx := r.Context()

	kv, err := h.deps().JS.KeyValue(ctx, r.PathValue("bucket"))
	if err != nil {
		if errors.Is(err, jetstream.ErrBucketNotFound) {
			writeError(w, http.StatusBadRequest, "unknown bucket: "+r.PathValue("bucket"))
			return
		}
		h.deps().Log.Error("open kv bucket", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		h.deps().Log.Error("kv watch all", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
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

	send := func(change kvChange) {
		data, err := json.Marshal(change)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}

	// WatchAll replays current entries, then delivers a single nil entry to
	// mark the end of the initial snapshot, then live updates. `live` flips
	// true at that marker so the frontend can split contents from the feed.
	live := false
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				return
			}
			if entry == nil {
				live = true
				send(kvChange{Op: "INIT_DONE", Live: true})
				continue
			}
			change := kvChange{
				Key:      entry.Key(),
				Op:       opString(entry.Operation()),
				Revision: entry.Revision(),
				Created:  entry.Created(),
				Live:     live,
			}
			if entry.Operation() == jetstream.KeyValuePut {
				change.Value = entry.Value()
			}
			send(change)
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}
