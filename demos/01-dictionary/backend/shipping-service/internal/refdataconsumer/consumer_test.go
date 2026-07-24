package refdataconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
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

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("shipping-service-test"))
	if err != nil {
		t.Fatal(err)
	}
	if nc.Opts.Name == "" {
		t.Fatal("expected nats connection to be named")
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return kvstore.New(js, "refdata"), func() { nc.Close(); srv.Shutdown() }
}

// newTestNATS starts a second embedded in-process NATS server for the rpc.*
// dual-transport tests — separate from newTestKV's server since those tests
// don't need JetStream, just core NATS request/reply. Every Consumer in this
// file needs one: Phase 12.11 (BR-D28) made NATS the consumer's only
// transport, so it's a required constructor argument, not an option.
func newTestNATS(t *testing.T) (*nats.Conn, func()) {
	t.Helper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("shipping-service-test-rpc"))
	if err != nil {
		t.Fatal(err)
	}
	return nc, func() { nc.Close(); srv.Shutdown() }
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

// respondItemGet subscribes a one-shot rpc.* item.get responder for
// itemCtx, always answering with the given code/label.
func respondItemGet(t *testing.T, nc *nats.Conn, itemCtx, code, label string) {
	t.Helper()
	sub, err := nc.Subscribe("rpc."+itemCtx+".refdata.item.get.v1", func(msg *nats.Msg) {
		var req rpcItemGetRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			t.Errorf("rpc responder: bad request: %v", err)
			return
		}
		var resp rpcItemGetResponse
		resp.Item.Code = code
		resp.Item.Status = "active"
		resp.Label = label
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

// respondItemGetError subscribes a one-shot rpc.* item.get responder that
// always answers with the natsrpc error-response shape.
func respondItemGetError(t *testing.T, nc *nats.Conn, itemCtx string, notFound bool, errMsg string) {
	t.Helper()
	sub, err := nc.Subscribe("rpc."+itemCtx+".refdata.item.get.v1", func(msg *nats.Msg) {
		data, _ := json.Marshal(rpcErrorResponse{Error: errMsg, NotFound: notFound})
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

func TestLookupCacheHit(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	putCacheEntry(t, kv, "emea-acme", "hazard-class", "3", 1)
	putMeta(t, kv, "emea-acme", "hazard-class", 1)
	// Deliberately no rpc.* responder — a cache hit must never call it.

	c := New(kv, nc)
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "kv-cache" {
		t.Fatalf("expected kv-cache, got %s", result.Source)
	}
}

func TestLookupMissUsesRPC(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	// No cache entry written — a cold-start / never-cached miss.

	respondItemGet(t, nc, "emea-acme", "3", "Flammable Liquids")

	c := New(kv, nc)
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "rpc-refetch" {
		t.Fatalf("expected rpc-refetch, got %s", result.Source)
	}
	if result.Code != "3" {
		t.Fatalf("expected code 3, got %s", result.Code)
	}
}

// TestLookupVersionMismatchUsesRPC is the versioned-read protocol's core
// behavior: a cache entry stamped with an older version than the type's
// current _meta version is treated as stale, not trusted.
func TestLookupVersionMismatchUsesRPC(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	putCacheEntry(t, kv, "emea-acme", "hazard-class", "3", 1) // entry stamped v1
	putMeta(t, kv, "emea-acme", "hazard-class", 2)            // set has since moved to v2

	var rpcCalled atomic.Bool
	sub, err := nc.Subscribe("rpc.emea-acme.refdata.item.get.v1", func(msg *nats.Msg) {
		rpcCalled.Store(true)
		var resp rpcItemGetResponse
		resp.Item.Code = "3"
		resp.Item.Status = "active"
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(kv, nc)
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
	if err != nil {
		t.Fatal(err)
	}
	if !rpcCalled.Load() {
		t.Fatal("expected the version mismatch to trigger an rpc.* re-fetch")
	}
	if result.Source != "rpc-refetch" {
		t.Fatalf("expected rpc-refetch, got %s", result.Source)
	}
}

func TestLookupNotFound(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	respondItemGetError(t, nc, "emea-acme", true, "dictionary item not found")

	c := New(kv, nc)
	_, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "unknown", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ─── BR-D08: KV-first label resolution with the BR-D03 fallback chain ──────────

// TestLookupResolvesLabelFromKV — a cache hit resolves the requested locale's
// label locally from the cached localizations map, with no rpc.* call.
func TestLookupResolvesLabelFromKV(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "docked", 1, map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
		"es": {Locale: "es", Label: "Atracado"},
	})
	putMeta(t, kv, "emea-acme", "ship-status", 1)
	// Deliberately no rpc.* responder — a cache hit must never call it.

	c := New(kv, nc)
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
}

// TestLookupLabelFallsBackToBareLanguage — a region locale (es-ES) with no
// exact match falls back to the bare language (es).
func TestLookupLabelFallsBackToBareLanguage(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "docked", 1, map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
		"es": {Locale: "es", Label: "Atracado"},
	})
	putMeta(t, kv, "emea-acme", "ship-status", 1)

	c := New(kv, nc)
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
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "docked", 1, map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
	})
	putCacheEntryLoc(t, kv, "emea-acme", "ship-status", "at-anchor", 1, nil) // no localizations at all
	putMeta(t, kv, "emea-acme", "ship-status", 1)

	c := New(kv, nc)

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

// TestLookupMissForwardsLocaleToRPC — a KV miss re-fetches over rpc.*
// passing the requested locale, and returns the server-resolved label.
func TestLookupMissForwardsLocaleToRPC(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	var gotLocale string
	sub, err := nc.Subscribe("rpc.emea-acme.refdata.item.get.v1", func(msg *nats.Msg) {
		var req rpcItemGetRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			t.Errorf("rpc responder: bad request: %v", err)
			return
		}
		gotLocale = req.Locale
		var resp rpcItemGetResponse
		resp.Item.Code = "docked"
		resp.Item.Status = "active"
		resp.Label = "Atracado"
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(kv, nc)
	result, err := c.Lookup(context.Background(), "emea-acme", "ship-status", "docked", "es")
	if err != nil {
		t.Fatal(err)
	}
	if gotLocale != "es" {
		t.Fatalf("expected locale es forwarded to rpc.*, got %q", gotLocale)
	}
	if result.Source != "rpc-refetch" {
		t.Fatalf("expected rpc-refetch, got %s", result.Source)
	}
	if result.Label != "Atracado" {
		t.Fatalf("expected label Atracado, got %q", result.Label)
	}
}

// TestLookupReturnsErrRPCUnavailableWhenNoResponder — with no REST fallback
// (BR-D28), a KV miss with nothing listening on rpc.* must return
// ErrRPCUnavailable after exhausting its retries, not hang or silently
// succeed some other way.
func TestLookupReturnsErrRPCUnavailableWhenNoResponder(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	// Deliberately no subscriber on the rpc.* subject.

	c := New(kv, nc, WithRPCTimeout(100*time.Millisecond), WithRPCRetries(1), WithRPCBackoff(10*time.Millisecond))
	_, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
	if !errors.Is(err, ErrRPCUnavailable) {
		t.Fatalf("expected ErrRPCUnavailable, got %v", err)
	}
}

// TestLookupRetriesBeforeSucceeding proves requestRPC actually retries
// rather than failing after a single attempt: the responder ignores the
// first two requests and only answers the third.
func TestLookupRetriesBeforeSucceeding(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	var attempts atomic.Int32
	sub, err := nc.Subscribe("rpc.emea-acme.refdata.item.get.v1", func(msg *nats.Msg) {
		n := attempts.Add(1)
		if n < 3 {
			return // deliberately don't respond — simulates a dropped/slow attempt
		}
		var resp rpcItemGetResponse
		resp.Item.Code = "3"
		resp.Item.Status = "active"
		resp.Label = "Flammable Liquids"
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(kv, nc, WithRPCTimeout(150*time.Millisecond), WithRPCRetries(2), WithRPCBackoff(10*time.Millisecond))
	result, err := c.Lookup(context.Background(), "emea-acme", "hazard-class", "3", "")
	if err != nil {
		t.Fatalf("expected the third attempt to succeed, got err: %v", err)
	}
	if result.Source != "rpc-refetch" {
		t.Fatalf("expected rpc-refetch, got %s", result.Source)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", attempts.Load())
	}
}

// TestResolveTypeReturnsAllCodesFromKV — ResolveType enumerates the bucket and
// resolves every item of the type KV-first, ignoring other types and _meta.
func TestResolveTypeReturnsAllCodesFromKV(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

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

	c := New(kv, nc)
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

// TestResolveTypeUsesRPCWhenBucketEmpty — with no KV entries, it calls
// rpc.*'s type.list endpoint (which also backfills the cache server-side).
func TestResolveTypeUsesRPCWhenBucketEmpty(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	sub, err := nc.Subscribe("rpc.emea-acme.refdata.type.list.v1", func(msg *nats.Msg) {
		var item rpcTypeListItem
		item.Item.Code = "docked"
		item.Item.Status = "active"
		item.Label = "Atracado"
		resp := rpcTypeListResponse{Items: []rpcTypeListItem{item}}
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(kv, nc)
	results, err := c.ResolveType(context.Background(), "emea-acme", "ship-status", "es")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Label != "Atracado" || results[0].Source != "rpc-refetch" {
		t.Fatalf("expected 1 rpc-refetch item Atracado, got %+v", results)
	}
}

func putVersionedCacheEntry(t *testing.T, kv *kvstore.Store, itemContext string, version int, typeKey, code string, locs map[string]localization) {
	t.Helper()
	entry := versionedCacheEntry{Version: version, Localizations: locs, SourceContext: itemContext}
	entry.Item.Code = code
	entry.Item.Status = "active"
	entry.Item.Attrs = map[string]any{"name": code}
	data, _ := json.Marshal(entry)
	if _, err := kv.PutVersioned(context.Background(), itemContext, version, typeKey+"."+code, data); err != nil {
		t.Fatal(err)
	}
}

// TestLookupAtVersionCacheHit — a consumer pinned to version 1 reads that
// version's bucket directly, without calling rpc.*, even though it never
// wrote a "current" entry to the unversioned bucket at all (Phase 12.5's
// versioned buckets are a separate read path from the plain Q5 cache).
func TestLookupAtVersionCacheHit(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	putVersionedCacheEntry(t, kv, "emea-acme", 1, "currency", "usd", map[string]localization{
		"en": {Locale: "en", Label: "US Dollar"},
	})
	// Deliberately no rpc.* responder — a versioned cache hit must never call it.

	c := New(kv, nc)
	result, err := c.LookupAtVersion(context.Background(), "emea-acme", 1, "currency", "usd", "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "kv-cache" {
		t.Fatalf("expected kv-cache, got %s", result.Source)
	}
	if result.Label != "US Dollar" {
		t.Fatalf("expected label US Dollar, got %s", result.Label)
	}
}

// TestLookupAtVersionDifferentVersionsCoexist — pinning to two different
// versions of the same item returns each version's own data, confirming old
// and new corpus versions coexist independently rather than one clobbering
// the other's bucket.
func TestLookupAtVersionDifferentVersionsCoexist(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	putVersionedCacheEntry(t, kv, "emea-acme", 1, "currency", "usd", map[string]localization{"en": {Locale: "en", Label: "v1 label"}})
	putVersionedCacheEntry(t, kv, "emea-acme", 2, "currency", "usd", map[string]localization{"en": {Locale: "en", Label: "v2 label"}})

	c := New(kv, nc)
	v1, err := c.LookupAtVersion(context.Background(), "emea-acme", 1, "currency", "usd", "en")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := c.LookupAtVersion(context.Background(), "emea-acme", 2, "currency", "usd", "en")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Label != "v1 label" || v2.Label != "v2 label" {
		t.Fatalf("expected independent versions, got v1=%q v2=%q", v1.Label, v2.Label)
	}
}

// TestLookupAtVersionMissUsesRPC — a miss on the versioned bucket calls
// rpc.*'s item.get-versioned endpoint, which returns the full materialized
// entry (every locale, no server-side resolution) — this consumer must
// resolve the label itself, same as the KV-hit path.
func TestLookupAtVersionMissUsesRPC(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	// No versioned cache entry written — a cold miss.

	var gotVersion int
	sub, err := nc.Subscribe("rpc.emea-acme.refdata.item.get-versioned.v1", func(msg *nats.Msg) {
		var req rpcItemGetVersionedRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			t.Errorf("rpc responder: bad request: %v", err)
			return
		}
		gotVersion = req.Version
		var entry versionedCacheEntry
		entry.Item.Code = "usd"
		entry.Item.Status = "active"
		entry.Item.Attrs = map[string]any{"name": "US Dollar"}
		entry.Localizations = map[string]localization{"en": {Locale: "en", Label: "US Dollar"}}
		data, _ := json.Marshal(entry)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(kv, nc)
	result, err := c.LookupAtVersion(context.Background(), "emea-acme", 3, "currency", "usd", "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "rpc-refetch" {
		t.Fatalf("expected rpc-refetch, got %s", result.Source)
	}
	if result.Label != "US Dollar" {
		t.Fatalf("expected label US Dollar, got %s", result.Label)
	}
	if gotVersion != 3 {
		t.Fatalf("expected version 3 forwarded to rpc.*, got %d", gotVersion)
	}
}

// ─── Locales: no KV tier at all, always rpc.* (Phase 12.11) ────────────────

func TestLocalesUsesRPC(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	sub, err := nc.Subscribe("rpc.emea-acme.refdata.locales.list.v1", func(msg *nats.Msg) {
		data, _ := json.Marshal(rpcLocalesListResponse{Locales: []string{"en", "fr"}, DefaultLocale: "en"})
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(kv, nc)
	locales, err := c.Locales(context.Background(), "emea-acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(locales) != 2 {
		t.Fatalf("expected 2 locales, got %v", locales)
	}
}

func TestLocalesReturnsErrRPCUnavailableWhenNoResponder(t *testing.T) {
	kv, cleanupKV := newTestKV(t)
	defer cleanupKV()
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	// Deliberately no subscriber on the rpc.* subject.

	c := New(kv, nc, WithRPCTimeout(100*time.Millisecond), WithRPCRetries(1), WithRPCBackoff(10*time.Millisecond))
	_, err := c.Locales(context.Background(), "emea-acme")
	if !errors.Is(err, ErrRPCUnavailable) {
		t.Fatalf("expected ErrRPCUnavailable, got %v", err)
	}
}
