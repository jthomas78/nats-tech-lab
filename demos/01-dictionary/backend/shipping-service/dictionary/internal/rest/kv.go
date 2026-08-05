package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// opString renders a jetstream.KeyValueOp for the wire (kvChange.Op /
// the old watchEvent.Op) — PUT/DEL/PURGE, matching NATS KV's own three
// operation kinds.
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

// kvChange is one KV entry as returned by kvBucketEntriesOnce's bootstrap
// snapshot (Phase 23) — live changes after this snapshot arrive over
// notify.{context}.kv.{bucket}.{key}.changed instead, which carries just the
// raw value (see internal/kvstore.Store.EnableNotify), not this envelope.
type kvChange struct {
	Key      string          `json:"key"`
	Op       string          `json:"op"` // PUT, DEL, PURGE
	Revision uint64          `json:"revision"`
	Created  time.Time       `json:"created,omitempty"`
	Value    json.RawMessage `json:"value,omitempty" swaggertype:"object"`
}

// kvBucketEntriesOnce godoc
//
// @Summary      One-shot KV bucket entries (Phase 23)
// @Description  Returns every current entry in one KV bucket as a single JSON array, snapshotted at request time — the bootstrap half of watchKVBucket's old SSE stream (Live=false entries only). Live changes after this snapshot arrive via notify.{context}.kv.{bucket}.{key}.changed (internal/kvstore.Store.EnableNotify) instead of holding a connection open.
// @Tags         kv
// @Produce      json
// @Param        bucket  path  string  true  "KV bucket name (e.g. dict-a, dict-b, container, meta)"
// @Success      200  {array}   kvChange
// @Failure      400  {object}  errorResponse  "Unknown bucket"
// @Failure      500  {object}  errorResponse
// @Router       /api/kv/buckets/{bucket}/entries [get]
func (h *Handlers) kvBucketEntriesOnce(w http.ResponseWriter, r *http.Request) {
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

	entries := []kvChange{}
	for entry := range watcher.Updates() {
		if entry == nil {
			break // WatchAll's INIT_DONE marker — snapshot complete
		}
		change := kvChange{
			Key:      entry.Key(),
			Op:       opString(entry.Operation()),
			Revision: entry.Revision(),
			Created:  entry.Created(),
		}
		if entry.Operation() == jetstream.KeyValuePut {
			change.Value = entry.Value()
		}
		entries = append(entries, change)
	}

	writeJSON(w, http.StatusOK, entries)
}

