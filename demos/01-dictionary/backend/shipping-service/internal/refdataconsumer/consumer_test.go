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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
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
	putCacheEntryLoc(t, kv, itemContext, typeKey, code, version, nil)
}

// putCacheEntryLoc writes a cache entry with an optional localizations map
// (keyed by locale), mirroring what refdata-service's projector writes.
func putCacheEntryLoc(t *testing.T, kv *kvstore.Store, itemContext, typeKey, code string, version int, locs map[string]localization) {
	t.Helper()
	entry := cacheEntry{Version: version, Localizations: locs}
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
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
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
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
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
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
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
	_, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "unknown", "")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ─── BR-D08: KV-first label resolution with the BR-D03 fallback chain ──────────

// TestLookupResolvesLabelFromKV — a cache hit resolves the requested locale's
// label locally from the cached localizations map, with no API call.
func TestLookupResolvesLabelFromKV(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "docked", 1, map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
		"es": {Locale: "es", Label: "Atracado"},
	})
	putMeta(t, kv, "emea-acme", "ship-status", 1)

	apiCalled := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := New(kv, api.URL)
	result, err := c.Lookup(context.Background(), "emea-acme", "ship-status", "docked", "es")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "kv-cache" {
		t.Fatalf("expected kv-cache, got %s", result.Source)
	}
	if result.Label != "Atracado" {
		t.Fatalf("expected label Atracado, got %q", result.Label)
	}
	if apiCalled {
		t.Fatal("expected no API call on a cache hit")
	}
}

// TestLookupLabelFallsBackToBareLanguage — a region locale (es-ES) with no
// exact match falls back to the bare language (es).
func TestLookupLabelFallsBackToBareLanguage(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "docked", 1, map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
		"es": {Locale: "es", Label: "Atracado"},
	})
	putMeta(t, kv, "emea-acme", "ship-status", 1)

	c := New(kv, "unused")
	result, err := c.Lookup(context.Background(), "emea-acme", "ship-status", "docked", "es-ES")
	if err != nil {
		t.Fatal(err)
	}
	if result.Label != "Atracado" {
		t.Fatalf("expected es-ES to fall back to es label Atracado, got %q", result.Label)
	}
}

// TestLookupLabelFallsBackToDefaultThenCode — an unknown locale falls back to
// the default locale (en); when even that is absent, to the code itself.
func TestLookupLabelFallsBackToDefaultThenCode(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "docked", 1, map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
	})
	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "at-anchor", 1, nil) // no localizations at all
	putMeta(t, kv, "emea-acme", "ship-status", 1)

	c := New(kv, "unused")

	docked, err := c.Lookup(context.Background(), "emea-acme", "ship-status", "docked", "ja-JP")
	if err != nil {
		t.Fatal(err)
	}
	if docked.Label != "Docked" {
		t.Fatalf("expected fallback to default locale label Docked, got %q", docked.Label)
	}

	anchor, err := c.Lookup(context.Background(), "emea-acme", "ship-status", "at-anchor", "ja-JP")
	if err != nil {
		t.Fatal(err)
	}
	if anchor.Label != "at-anchor" {
		t.Fatalf("expected fallback to code at-anchor, got %q", anchor.Label)
	}
}

// TestLookupMissForwardsLocaleToAPI — a KV miss re-fetches over REST passing
// the ?locale= query param, and returns the server-resolved label.
func TestLookupMissForwardsLocaleToAPI(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	var gotLocale string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLocale = r.URL.Query().Get("locale")
		var resp apiItemResponse
		resp.Item.Code = "docked"
		resp.Item.Status = "active"
		resp.Label = "Atracado"
		json.NewEncoder(w).Encode(resp)
	}))
	defer api.Close()

	c := New(kv, api.URL)
	result, err := c.Lookup(context.Background(), "emea-acme", "ship-status", "docked", "es")
	if err != nil {
		t.Fatal(err)
	}
	if gotLocale != "es" {
		t.Fatalf("expected ?locale=es forwarded to API, got %q", gotLocale)
	}
	if result.Source != "api-refetch" {
		t.Fatalf("expected api-refetch, got %s", result.Source)
	}
	if result.Label != "Atracado" {
		t.Fatalf("expected label Atracado, got %q", result.Label)
	}
}

// TestResolveTypeReturnsAllCodesFromKV — ResolveType enumerates the bucket and
// resolves every item of the type KV-first, ignoring other types and _meta.
func TestResolveTypeReturnsAllCodesFromKV(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "docked", 1, map[string]localization{
		"es": {Locale: "es", Label: "Atracado"},
	})
	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "in-transit", 1, map[string]localization{
		"es": {Locale: "es", Label: "En tránsito"},
	})
	putMeta(t, kv, "emea-acme", "ship-status", 1)
	// A different type in the same bucket must be ignored.
	putCacheEntry(t, kv, "emea-acme", "currency", "USD", 1)
	putMeta(t, kv, "emea-acme", "currency", 1)

	c := New(kv, "unused")
	results, err := c.ResolveType(context.Background(), "emea-acme", "ship-status", "es")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 ship-status items, got %d", len(results))
	}
	labels := map[string]string{}
	for _, r := range results {
		labels[r.Code] = r.Label
		if r.Source != "kv-cache" {
			t.Fatalf("expected kv-cache source for %s, got %s", r.Code, r.Source)
		}
	}
	if labels["docked"] != "Atracado" || labels["in-transit"] != "En tránsito" {
		t.Fatalf("unexpected resolved labels: %v", labels)
	}
}

// TestResolveTypeFallsBackToAPIListWhenBucketEmpty — with no KV entries, it
// falls through to the REST list endpoint (which also backfills the cache).
func TestResolveTypeFallsBackToAPIListWhenBucketEmpty(t *testing.T) {
	kv, cleanup := newTestKV(t)
	defer cleanup()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiListResponse{Items: []apiItemResponse{
			func() apiItemResponse {
				var it apiItemResponse
				it.Item.Code = "docked"
				it.Item.Status = "active"
				it.Label = "Atracado"
				return it
			}(),
		}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer api.Close()

	c := New(kv, api.URL)
	results, err := c.ResolveType(context.Background(), "emea-acme", "ship-status", "es")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Label != "Atracado" || results[0].Source != "api-refetch" {
		t.Fatalf("expected 1 api-refetch item Atracado, got %+v", results)
	}
}
