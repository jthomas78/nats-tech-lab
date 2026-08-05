package rest

// Test for Phase 23's one-shot KV bootstrap endpoint, replacing the snapshot
// half of watchKVBucket's SSE stream.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

func TestKVBucketEntriesOnceReturnsCurrentEntriesAsOneJSONArray(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	ctx := context.Background()
	h := NewHandlers(Deps{JS: js, Log: slog.New(slog.DiscardHandler)})

	// Seed the bucket via the same HTTP surface a real client would use:
	// PUT through the KV wrapper isn't exposed directly, so create the
	// bucket and write to it through the raw jetstream.KeyValue API, mirroring
	// how kvBucketEntriesOnce itself reads it.
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "dict-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Put(ctx, "acme.ship.orient-express", []byte(`{"shipID":"orient-express"}`)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/dict-a/entries", nil)
	req.SetPathValue("bucket", "dict-a")
	rec := httptest.NewRecorder()
	h.kvBucketEntriesOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []kvChange
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %s", len(entries), rec.Body.String())
	}
	if entries[0].Key != "acme.ship.orient-express" {
		t.Fatalf("expected raw (unstripped) internal key, got %q", entries[0].Key)
	}
}

func TestKVBucketEntriesOnceReturns400ForAnUnknownBucket(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	h := NewHandlers(Deps{JS: js, Log: slog.New(slog.DiscardHandler)})

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/nope/entries", nil)
	req.SetPathValue("bucket", "nope")
	rec := httptest.NewRecorder()
	h.kvBucketEntriesOnce(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
