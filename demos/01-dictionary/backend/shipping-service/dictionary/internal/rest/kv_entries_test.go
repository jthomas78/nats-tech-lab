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
	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

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

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/acme/dict-a/entries", nil)
	req.SetPathValue("account", "acme")
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

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/acme/nope/entries", nil)
	req.SetPathValue("account", "acme")
	req.SetPathValue("bucket", "nope")
	rec := httptest.NewRecorder()
	h.kvBucketEntriesOnce(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestKVBucketEntriesOnceReturns400ForAnUnknownAccount(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/nope/dict-a/entries", nil)
	req.SetPathValue("account", "nope")
	req.SetPathValue("bucket", "dict-a")
	rec := httptest.NewRecorder()
	h.kvBucketEntriesOnce(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestKVBucketEntriesOnceResolvesThePlatformAccount confirms "platform" is
// special-cased to Deps.PlatformFullJS rather than looked up in
// TenantResources (which never has a "platform" entry — see
// nonTenantCredsFiles in tenant.go) — this is how the KV inspector reaches
// refdata-service's buckets, which live on the PLATFORM account, not any
// tenant's.
func TestKVBucketEntriesOnceResolvesThePlatformAccount(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "refdata-acme"}); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{PlatformFullJS: js, Log: slog.New(slog.DiscardHandler)})

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/platform/refdata-acme/entries", nil)
	req.SetPathValue("account", "platform")
	req.SetPathValue("bucket", "refdata-acme")
	rec := httptest.NewRecorder()
	h.kvBucketEntriesOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
