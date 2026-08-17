package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestListKVBucketsReportsPlatformBuckets(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "dict-a"}); err != nil {
		t.Fatalf("create kv bucket: %v", err)
	}

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets", nil)
	w := httptest.NewRecorder()

	h.listKVBuckets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body kvBucketsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d: %+v", len(body.Buckets), body.Buckets)
	}
	if body.Buckets[0].Bucket != "dict-a" || body.Buckets[0].Account != platformAccount {
		t.Fatalf("unexpected bucket: %+v", body.Buckets[0])
	}
	if len(body.Accounts) != 1 || body.Accounts[0].Name != platformAccount || body.Accounts[0].Status != platformAccountStatus {
		t.Fatalf("expected platform account %q in Accounts, got %+v", platformAccountStatus, body.Accounts)
	}
}

// TestListKVBucketsSkipsUnreachableAccountRatherThanFailingWholeResponse
// mirrors streams_test.go's matching case: a suspended tenant's
// cross-account $JS.API access must not abort the whole response and
// discard PLATFORM's already-successful buckets. It must also still appear
// in Accounts with its real status — only its bucket rows are missing.
func TestListKVBucketsSkipsUnreachableAccountRatherThanFailingWholeResponse(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "dict-a"}); err != nil {
		t.Fatalf("create kv bucket: %v", err)
	}

	accounts := newAccountsMock(t, []AccountsClientAccount{{Name: "acme", PublicKey: "AAAACME", Status: "suspended"}})
	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: accounts})
	w := httptest.NewRecorder()
	h.listKVBuckets(w, httptest.NewRequest(http.MethodGet, "/api/kv/buckets", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 despite acme being unreachable, got %d: %s", w.Code, w.Body.String())
	}
	var body kvBucketsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Buckets) != 1 || body.Buckets[0].Account != platformAccount {
		t.Fatalf("expected platform's dict-a to survive acme's failure, got %+v", body.Buckets)
	}
	got := map[string]string{}
	for _, a := range body.Accounts {
		got[a.Name] = a.Status
	}
	if got[platformAccount] != platformAccountStatus {
		t.Fatalf("expected platform in Accounts as %q, got %+v", platformAccountStatus, body.Accounts)
	}
	if got["acme"] != "suspended" {
		t.Fatalf("expected acme still in Accounts as suspended despite unreachable buckets, got %+v", body.Accounts)
	}
}

func TestListKVBucketsEmptyWhenNothingRegistered(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	w := httptest.NewRecorder()
	h.listKVBuckets(w, httptest.NewRequest(http.MethodGet, "/api/kv/buckets", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body kvBucketsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Buckets == nil || len(body.Buckets) != 0 {
		t.Fatalf("expected an empty (non-nil) bucket list, got %+v", body.Buckets)
	}
}

func TestKVBucketEntriesOnceReturnsSnapshot(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "dict-a"})
	if err != nil {
		t.Fatalf("create kv bucket: %v", err)
	}
	// KV values in this system are always JSON-encoded (internal/kvstore.
	// Store.Put marshals structs before storing) — kvChange.Value is typed
	// json.RawMessage, so a fixture must store real JSON, not a bare string
	// (PutString stores raw, non-JSON bytes and silently breaks encoding).
	if _, err := kv.Put(ctx, "key.one", []byte(`{"v":"one"}`)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := kv.Put(ctx, "key.two", []byte(`{"v":"two"}`)); err != nil {
		t.Fatalf("put: %v", err)
	}

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/platform/dict-a/entries", nil)
	req.SetPathValue("account", "platform")
	req.SetPathValue("bucket", "dict-a")
	w := httptest.NewRecorder()

	h.kvBucketEntriesOnce(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var entries []kvChange
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	byKey := map[string]kvChange{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	if string(byKey["key.one"].Value) != `{"v":"one"}` {
		t.Errorf("unexpected value for key.one: %s", byKey["key.one"].Value)
	}
	if byKey["key.one"].Op != "PUT" {
		t.Errorf("expected op PUT, got %s", byKey["key.one"].Op)
	}
}

func TestKVBucketEntriesOnceReturns400ForUnknownAccount(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/nope/dict-a/entries", nil)
	req.SetPathValue("account", "nope")
	req.SetPathValue("bucket", "dict-a")
	w := httptest.NewRecorder()

	h.kvBucketEntriesOnce(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestKVBucketEntriesOnceReturns400ForUnknownBucket(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets/platform/nope/entries", nil)
	req.SetPathValue("account", "platform")
	req.SetPathValue("bucket", "nope")
	w := httptest.NewRecorder()

	h.kvBucketEntriesOnce(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestJsForAccountResolvesKnownTenantAndRejectsUnknown is a fast, pure
// Go-level check (JetStream context construction is lazy — no NATS round
// trip happens here) of the account-name matching logic that replaced
// shipping-service's TenantResources map lookup. It does NOT prove the
// monitor.{tenant}.js prefix actually resolves cross-account on the wire —
// see testutil_test.go's package doc comment for why that's out of scope
// here.
func TestJsForAccountResolvesKnownTenantAndRejectsUnknown(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	accounts := newAccountsMock(t, []AccountsClientAccount{{Name: "acme", PublicKey: "AAAACME"}})
	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: accounts})

	if _, ok := h.jsForAccount(t.Context(), platformAccount); !ok {
		t.Error("expected platform account to always resolve")
	}
	if _, ok := h.jsForAccount(t.Context(), "acme"); !ok {
		t.Error("expected known tenant acme to resolve")
	}
	if _, ok := h.jsForAccount(t.Context(), "globex"); ok {
		t.Error("expected unknown tenant globex to be rejected")
	}
}

// TestIntrospectableAccountsTagsPlatformAndTenantStatus pins
// introspectableAccounts' status field: PLATFORM is always
// platformAccountStatus ("active"), never resolved from accounts-service,
// while each tenant carries whatever status accounts-service reports for
// it — the Streams/KV Buckets panels' account-dot now reflects this instead
// of the browser's own connected tenant.
func TestIntrospectableAccountsTagsPlatformAndTenantStatus(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	accounts := newAccountsMock(t, []AccountsClientAccount{
		{Name: "acme", PublicKey: "AAAACME", Status: "suspended"},
	})
	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: accounts})

	got := map[string]string{}
	for _, acct := range h.introspectableAccounts(t.Context()) {
		got[acct.name] = acct.status
	}
	if got[platformAccount] != platformAccountStatus {
		t.Fatalf("expected platform status %q, got %q", platformAccountStatus, got[platformAccount])
	}
	if got["acme"] != "suspended" {
		t.Fatalf("expected acme status suspended, got %q", got["acme"])
	}
}
