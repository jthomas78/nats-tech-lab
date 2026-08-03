package refdataconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// newTestNATS starts an embedded in-process NATS server for the rpc.*
// dual-transport tests — every Consumer in this file needs one: Phase 12.11
// (BR-D28) made NATS the consumer's only transport, and the RPC-only
// refactor (BR-D08) removed the KV tier entirely, so a NATS connection is
// this package's only external dependency now.
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

// ─── Lookup: rpc.*.refdata.item.get.v1 only, no KV tier (BR-D08) ───────────

func TestLookupUsesRPC(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	respondItemGet(t, nc, "acme-test", "3", "Flammable Liquids")

	c := New(nc)
	result, err := c.Lookup(context.Background(), "acme-test", "hazard-class", "3", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != rpcSource {
		t.Fatalf("expected %s, got %s", rpcSource, result.Source)
	}
	if result.Code != "3" {
		t.Fatalf("expected code 3, got %s", result.Code)
	}
	if result.Label != "Flammable Liquids" {
		t.Fatalf("expected label Flammable Liquids, got %q", result.Label)
	}
}

func TestLookupNotFound(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	respondItemGetError(t, nc, "acme-test", true, "dictionary item not found")

	c := New(nc)
	_, err := c.Lookup(context.Background(), "acme-test", "hazard-class", "unknown", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestLookupForwardsLocaleToRPC — Lookup passes the requested locale through
// to rpc.* and returns the server-resolved label as-is; label resolution for
// the plain protocol is refdata-service's job (BR-D08), not this consumer's.
func TestLookupForwardsLocaleToRPC(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	var gotLocale string
	sub, err := nc.Subscribe("rpc.acme-test.refdata.item.get.v1", func(msg *nats.Msg) {
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

	c := New(nc)
	result, err := c.Lookup(context.Background(), "acme-test", "ship-status", "docked", "es")
	if err != nil {
		t.Fatal(err)
	}
	if gotLocale != "es" {
		t.Fatalf("expected locale es forwarded to rpc.*, got %q", gotLocale)
	}
	if result.Label != "Atracado" {
		t.Fatalf("expected label Atracado, got %q", result.Label)
	}
}

// TestLookupCarriesInstanceQualifiedRequestorHeader — BR-027: every rpc.*
// request carries Nats-Requestor as "<nats.Name>/<instance ID>", so replicas
// of the same service stay distinguishable (symmetric with Nats-Responder's
// format). The instance half is fixed per Consumer, so two calls from the
// same process must carry the identical value.
func TestLookupCarriesInstanceQualifiedRequestorHeader(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	var requestors []string
	sub, err := nc.Subscribe("rpc.acme-test.refdata.item.get.v1", func(msg *nats.Msg) {
		requestors = append(requestors, msg.Header.Get(requestorHeader))
		var resp rpcItemGetResponse
		resp.Item.Code = "docked"
		resp.Item.Status = "active"
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(nc)
	for i := 0; i < 2; i++ {
		if _, err := c.Lookup(context.Background(), "acme-test", "ship-status", "docked", ""); err != nil {
			t.Fatal(err)
		}
	}

	if len(requestors) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(requestors))
	}
	const wantPrefix = "shipping-service-test-rpc/"
	if !strings.HasPrefix(requestors[0], wantPrefix) || len(requestors[0]) == len(wantPrefix) {
		t.Fatalf("expected Nats-Requestor %q + non-empty instance ID, got %q", wantPrefix, requestors[0])
	}
	if requestors[0] != requestors[1] {
		t.Fatalf("expected a stable per-Consumer instance ID, got %q then %q", requestors[0], requestors[1])
	}
}

// TestLookupReturnsErrRPCUnavailableWhenNoResponder — with no REST fallback
// (BR-D28), nothing listening on rpc.* must return ErrRPCUnavailable after
// exhausting its retries, not hang or silently succeed some other way.
func TestLookupReturnsErrRPCUnavailableWhenNoResponder(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	// Deliberately no subscriber on the rpc.* subject.

	c := New(nc, WithRPCTimeout(100*time.Millisecond), WithRPCRetries(1), WithRPCBackoff(10*time.Millisecond))
	_, err := c.Lookup(context.Background(), "acme-test", "hazard-class", "3", "")
	if !errors.Is(err, ErrRPCUnavailable) {
		t.Fatalf("expected ErrRPCUnavailable, got %v", err)
	}
}

// TestLookupRetriesBeforeSucceeding proves requestRPC actually retries
// rather than failing after a single attempt: the responder ignores the
// first two requests and only answers the third.
func TestLookupRetriesBeforeSucceeding(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	var attempts atomic.Int32
	sub, err := nc.Subscribe("rpc.acme-test.refdata.item.get.v1", func(msg *nats.Msg) {
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

	c := New(nc, WithRPCTimeout(150*time.Millisecond), WithRPCRetries(2), WithRPCBackoff(10*time.Millisecond))
	result, err := c.Lookup(context.Background(), "acme-test", "hazard-class", "3", "")
	if err != nil {
		t.Fatalf("expected the third attempt to succeed, got err: %v", err)
	}
	if result.Source != rpcSource {
		t.Fatalf("expected %s, got %s", rpcSource, result.Source)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", attempts.Load())
	}
}

// ─── ResolveType: rpc.*.refdata.type.list.v1 only, no KV tier (BR-D08) ─────

func TestResolveTypeUsesRPC(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	sub, err := nc.Subscribe("rpc.acme-test.refdata.type.list.v1", func(msg *nats.Msg) {
		docked := rpcTypeListItem{Label: "Atracado"}
		docked.Item.Code = "docked"
		docked.Item.Status = "active"
		transit := rpcTypeListItem{Label: "En tránsito"}
		transit.Item.Code = "in-transit"
		transit.Item.Status = "active"
		resp := rpcTypeListResponse{Items: []rpcTypeListItem{docked, transit}}
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(nc)
	results, err := c.ResolveType(context.Background(), "acme-test", "ship-status", "es")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 ship-status items, got %d", len(results))
	}
	labels := map[string]string{}
	for _, r := range results {
		labels[r.Code] = r.Label
		if r.Source != rpcSource {
			t.Fatalf("expected %s source for %s, got %s", rpcSource, r.Code, r.Source)
		}
	}
	if labels["docked"] != "Atracado" || labels["in-transit"] != "En tránsito" {
		t.Fatalf("unexpected resolved labels: %v", labels)
	}
}

// ─── LookupAtVersion: rpc.*.refdata.item.get-versioned.v1 only ─────────────
//
// The versioned protocol always returns every locale rather than a
// pre-resolved label (unlike the plain protocol above), so this consumer
// still applies the BR-D03 fallback chain locally via resolveLabel — these
// tests are the only remaining coverage of that function.

func respondItemGetVersioned(t *testing.T, nc *nats.Conn, subject string, locs map[string]localization) *int32 {
	t.Helper()
	var gotVersion int32
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var req rpcItemGetVersionedRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			t.Errorf("rpc responder: bad request: %v", err)
			return
		}
		atomic.StoreInt32(&gotVersion, int32(req.Version))
		var entry versionedCacheEntry
		entry.Item.Code = req.Code
		entry.Item.Status = "active"
		entry.Localizations = locs
		data, _ := json.Marshal(entry)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return &gotVersion
}

func TestLookupAtVersionUsesRPC(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	gotVersion := respondItemGetVersioned(t, nc, "rpc.acme-test.refdata.item.get-versioned.v1", map[string]localization{
		"en": {Locale: "en", Label: "US Dollar"},
	})

	c := New(nc)
	result, err := c.LookupAtVersion(context.Background(), "acme-test", 3, "currency", "usd", "en")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != rpcSource {
		t.Fatalf("expected %s, got %s", rpcSource, result.Source)
	}
	if result.Label != "US Dollar" {
		t.Fatalf("expected label US Dollar, got %s", result.Label)
	}
	if atomic.LoadInt32(gotVersion) != 3 {
		t.Fatalf("expected version 3 forwarded to rpc.*, got %d", atomic.LoadInt32(gotVersion))
	}
}

// TestLookupAtVersionResolvesDescriptionPerLocale — BR-D30 removed
// Item.Description from the versioned wire shape, so Description must now
// resolve from the localizations map via the same fallback chain as Label,
// not from a duplicated item-level field.
func TestLookupAtVersionResolvesDescriptionPerLocale(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	respondItemGetVersioned(t, nc, "rpc.acme-test.refdata.item.get-versioned.v1", map[string]localization{
		"en": {Locale: "en", Label: "US Dollar", Description: "The US Dollar"},
		"es": {Locale: "es", Label: "Dólar estadounidense", Description: "El dólar estadounidense"},
	})

	c := New(nc)
	result, err := c.LookupAtVersion(context.Background(), "acme-test", 1, "currency", "usd", "es")
	if err != nil {
		t.Fatal(err)
	}
	if result.Label != "Dólar estadounidense" || result.Description != "El dólar estadounidense" {
		t.Fatalf("expected es label/description, got label=%q description=%q", result.Label, result.Description)
	}
}

// TestLookupAtVersionDifferentVersionsCoexist — pinning to two different
// versions of the same item resolves each version's own data, confirming old
// and new corpus versions are independent per-call parameters rather than
// one clobbering the other.
func TestLookupAtVersionDifferentVersionsCoexist(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	sub, err := nc.Subscribe("rpc.acme-test.refdata.item.get-versioned.v1", func(msg *nats.Msg) {
		var req rpcItemGetVersionedRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			t.Errorf("rpc responder: bad request: %v", err)
			return
		}
		var entry versionedCacheEntry
		entry.Item.Code = req.Code
		entry.Item.Status = "active"
		label := "v1 label"
		if req.Version == 2 {
			label = "v2 label"
		}
		entry.Localizations = map[string]localization{"en": {Locale: "en", Label: label}}
		data, _ := json.Marshal(entry)
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(nc)
	v1, err := c.LookupAtVersion(context.Background(), "acme-test", 1, "currency", "usd", "en")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := c.LookupAtVersion(context.Background(), "acme-test", 2, "currency", "usd", "en")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Label != "v1 label" || v2.Label != "v2 label" {
		t.Fatalf("expected independent versions, got v1=%q v2=%q", v1.Label, v2.Label)
	}
}

// TestLookupAtVersionLabelFallsBackToBareLanguage — a region locale (es-ES)
// with no exact match in the versioned response falls back to the bare
// language (es), applying resolveLabel's BR-D03 chain locally.
func TestLookupAtVersionLabelFallsBackToBareLanguage(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	respondItemGetVersioned(t, nc, "rpc.acme-test.refdata.item.get-versioned.v1", map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
		"es": {Locale: "es", Label: "Atracado"},
	})

	c := New(nc)
	result, err := c.LookupAtVersion(context.Background(), "acme-test", 1, "ship-status", "docked", "es-ES")
	if err != nil {
		t.Fatal(err)
	}
	if result.Label != "Atracado" {
		t.Fatalf("expected es-ES to fall back to es label Atracado, got %q", result.Label)
	}
}

// TestLookupAtVersionLabelFallsBackToDefaultThenCode — an unknown locale
// falls back to the default locale (en); when even that is absent, to the
// code itself.
func TestLookupAtVersionLabelFallsBackToDefaultThenCode(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	respondItemGetVersioned(t, nc, "rpc.acme-test.refdata.item.get-versioned.v1", map[string]localization{
		"en": {Locale: "en", Label: "Docked"},
	})

	c := New(nc)
	docked, err := c.LookupAtVersion(context.Background(), "acme-test", 1, "ship-status", "docked", "ja-JP")
	if err != nil {
		t.Fatal(err)
	}
	if docked.Label != "Docked" {
		t.Fatalf("expected fallback to default locale label Docked, got %q", docked.Label)
	}
}

func TestLookupAtVersionLabelFallsBackToCodeWhenNoLocalizations(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	respondItemGetVersioned(t, nc, "rpc.acme-test.refdata.item.get-versioned.v1", nil) // no localizations at all

	c := New(nc)
	anchor, err := c.LookupAtVersion(context.Background(), "acme-test", 1, "ship-status", "at-anchor", "ja-JP")
	if err != nil {
		t.Fatal(err)
	}
	if anchor.Label != "at-anchor" {
		t.Fatalf("expected fallback to code at-anchor, got %q", anchor.Label)
	}
}

// ─── Locales: no KV tier at all, always rpc.* (Phase 12.11) ────────────────

func TestLocalesUsesRPC(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	sub, err := nc.Subscribe("rpc.acme-test.refdata.locales.list.v1", func(msg *nats.Msg) {
		data, _ := json.Marshal(rpcLocalesListResponse{Locales: []string{"en", "fr"}, DefaultLocale: "en"})
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(nc)
	result, err := c.Locales(context.Background(), "acme-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Locales) != 2 {
		t.Fatalf("expected 2 locales, got %v", result.Locales)
	}
	// BR-D32: the default locale must survive the hop to the frontend, so the
	// switcher can order it first and mark it — it used to be discarded here.
	if result.DefaultLocale != "en" {
		t.Fatalf("expected defaultLocale en, got %q", result.DefaultLocale)
	}
}

func TestLocalesReturnsErrRPCUnavailableWhenNoResponder(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	// Deliberately no subscriber on the rpc.* subject.

	c := New(nc, WithRPCTimeout(100*time.Millisecond), WithRPCRetries(1), WithRPCBackoff(10*time.Millisecond))
	_, err := c.Locales(context.Background(), "acme-test")
	if !errors.Is(err, ErrRPCUnavailable) {
		t.Fatalf("expected ErrRPCUnavailable, got %v", err)
	}
}

// ─── ListContexts: rpc._platform.refdata.context.list.v1 (Phase 16f) ──────

func TestListContextsUsesRPCAndForwardsTenant(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()

	var gotReq rpcContextListRequest
	sub, err := nc.Subscribe(contextListSubject, func(msg *nats.Msg) {
		if unmarshalErr := json.Unmarshal(msg.Data, &gotReq); unmarshalErr != nil {
			t.Error(unmarshalErr)
		}
		data, _ := json.Marshal(rpcContextListResponse{Contexts: []rpcContext{
			{Context: "_platform"}, {Context: "acme"}, {Context: "acme-atlantic-fleet"},
		}})
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	c := New(nc)
	contexts, err := c.ListContexts(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if gotReq.Tenant != "acme" {
		t.Fatalf("expected tenant %q forwarded in request, got %q", "acme", gotReq.Tenant)
	}
	want := []string{"_platform", "acme", "acme-atlantic-fleet"}
	if len(contexts) != len(want) {
		t.Fatalf("expected %v, got %v", want, contexts)
	}
	for i, wantCtx := range want {
		if contexts[i] != wantCtx {
			t.Fatalf("expected %v, got %v", want, contexts)
		}
	}
}

func TestListContextsReturnsErrRPCUnavailableWhenNoResponder(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	// Deliberately no subscriber on the rpc.* subject.

	c := New(nc, WithRPCTimeout(100*time.Millisecond), WithRPCRetries(1), WithRPCBackoff(10*time.Millisecond))
	_, err := c.ListContexts(context.Background(), "acme")
	if !errors.Is(err, ErrRPCUnavailable) {
		t.Fatalf("expected ErrRPCUnavailable, got %v", err)
	}
}
