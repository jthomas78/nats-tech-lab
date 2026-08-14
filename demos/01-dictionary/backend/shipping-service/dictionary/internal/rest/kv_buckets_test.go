package rest

// Tests for listKVBuckets' cross-account aggregation — the KV inspector's
// bucket rail is deliberately NOT scoped to the topbar's active tenant (see
// listKVBuckets' doc comment): it reports every known tenant's buckets plus
// the PLATFORM account's, each tagged with the account it belongs to.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

func TestListKVBucketsTagsEachBucketWithItsAccount(t *testing.T) {
	ctx := context.Background()
	// Two genuinely separate embedded servers, standing in for two separate
	// NATS accounts — a single shared js would let each "account" see the
	// other's buckets too (same underlying store), which would defeat the
	// point of this test.
	_, tenantJS, cleanupTenant := newTestNATSJS(t)
	defer cleanupTenant()
	_, platformJS, cleanupPlatform := newTestNATSJS(t)
	defer cleanupPlatform()

	if _, err := tenantJS.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "dict-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := platformJS.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "refdata-acme"}); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: tenantJS}},
		PlatformFullJS:  platformJS,
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets", nil)
	rec := httptest.NewRecorder()
	h.listKVBuckets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body kvBucketsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	byBucket := map[string]string{}
	for _, b := range body.Buckets {
		byBucket[b.Bucket] = b.Account
	}
	if got := byBucket["dict-a"]; got != "acme" {
		t.Fatalf("expected dict-a tagged account=acme, got %q", got)
	}
	if got := byBucket["refdata-acme"]; got != "platform" {
		t.Fatalf("expected refdata-acme tagged account=platform, got %q", got)
	}
}

// TestListKVBucketsIsNotScopedToASingleTenant is the regression test for the
// bug this change fixes: every known tenant's buckets come back in one
// response, regardless of which single tenant (if any) REST's Deps.Tenant/
// Deps.JS mirror fields currently point at — those fields aren't even set
// here, and the handler doesn't consult them.
func TestListKVBucketsIsNotScopedToASingleTenant(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "meta"}); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{
			"acme":   {js: js},
			"globex": {js: js},
		},
		PlatformFullJS: js,
		Log:            slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/kv/buckets", nil)
	rec := httptest.NewRecorder()
	h.listKVBuckets(rec, req)

	var body kvBucketsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	seen := map[string]bool{}
	for _, b := range body.Buckets {
		seen[b.Account] = true
	}
	for _, want := range []string{"acme", "globex", "platform"} {
		if !seen[want] {
			t.Fatalf("expected a bucket tagged account=%q in the response, got accounts %v", want, seen)
		}
	}
}
