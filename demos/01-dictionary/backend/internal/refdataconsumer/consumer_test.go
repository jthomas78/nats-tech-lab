package refdataconsumer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

// newTestKV starts an embedded in-process NATS server (real JetStream KV) —
// same convention as dictionary/integration_test.go's newJetStream().
func newTestKV(t *testing.T) (*kvstore.Store, func()) {
	t.Helper()
	opts := &server.Options{JetStream: true, StoreDir: t.TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return kvstore.New(js, "refdata"), func() { nc.Close(); srv.Shutdown() }
}

func putCacheEntry(t *testing.T, kv *kvstore.Store, itemContext, typeKey, code string, version int) {
	t.Helper()
	entry := cacheEntry{Version: version}
	entry.Item.Code = code
	entry.Item.Status = "active"
	entry.Item.Attrs = map[string]any{"name": code}
	data, _ := json.Marshal(entry)
	if _, err := kv.Put(context.Background(), itemContext, typeKey+"."+code, data); err != nil {
		t.Fatal(err)
	}
}

func putMeta(t *testing.T, kv *kvstore.Store, itemContext, typeKey string, version int) {
	t.Helper()
	data, _ := json.Marshal(metaEntry{Version: version})
	if _, err := kv.Put(context.Background(), itemContext, typeKey+"._meta", data); err != nil {
		t.Fatal(err)
	}
}

func TestLookupCacheHit(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	putCacheEntry(t, kv, "emea-acme", "hazard-class", "3", 1)
	putMeta(t, kv, "emea-acme", "hazard-class", 1)

	apiCalled := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := New(kv, api.URL)
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "kv-cache" {
		t.Fatalf("expected kv-cache, got %s", result.Source)
	}
	if apiCalled {
		t.Fatal("expected the API not to be called on a cache hit")
	}
}

func TestLookupMissFallsThroughToAPI(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()
	// No cache entry written — a cold-start / never-cached miss.

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp apiItemResponse
		resp.Item.Code = "3"
		resp.Item.Status = "active"
		resp.Item.Attrs = map[string]any{"name": "Flammable Liquids"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer api.Close()

	c := New(kv, api.URL)
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "api-refetch" {
		t.Fatalf("expected api-refetch, got %s", result.Source)
	}
	if result.Code != "3" {
		t.Fatalf("expected code 3, got %s", result.Code)
	}
}

// TestLookupVersionMismatchFallsThroughToAPI is the versioned-read protocol's
// core behavior: a cache entry stamped with an older version than the type's
// current _meta version is treated as stale, not trusted.
func TestLookupVersionMismatchFallsThroughToAPI(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	putCacheEntry(t, kv, "emea-acme", "hazard-class", "3", 1) // entry stamped v1
	putMeta(t, kv, "emea-acme", "hazard-class", 2)            // set has since moved to v2

	apiCalled := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		var resp apiItemResponse
		resp.Item.Code = "3"
		resp.Item.Status = "active"
		json.NewEncoder(w).Encode(resp)
	}))
	defer api.Close()

	c := New(kv, api.URL)
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3")
	if err != nil {
		t.Fatal(err)
	}
	if !apiCalled {
		t.Fatal("expected the version mismatch to trigger an API re-fetch")
	}
	if result.Source != "api-refetch" {
		t.Fatalf("expected api-refetch, got %s", result.Source)
	}
}

func TestLookupNotFound(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer api.Close()

	c := New(kv, api.URL)
	_, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "unknown")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
