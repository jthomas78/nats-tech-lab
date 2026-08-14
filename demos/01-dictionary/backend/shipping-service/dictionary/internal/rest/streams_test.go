package rest

// Tests for listStreams' cross-account aggregation — the JetStream panel's
// stream rail is deliberately NOT scoped to the topbar's active tenant (see
// listStreams' doc comment): it reports every known tenant's streams plus the
// PLATFORM account's, each tagged with the account it belongs to. Same shape
// as kv_buckets_test.go, for the same reason.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
)

// listStreamsBody runs the handler and decodes its response, failing the test
// on anything other than a 200.
func listStreamsBody(t *testing.T, h *Handlers) jsStreamsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/streams", nil)
	rec := httptest.NewRecorder()
	h.listStreams(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body jsStreamsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return body
}

func TestListStreamsTagsEachStreamWithItsAccount(t *testing.T) {
	ctx := context.Background()
	// Two genuinely separate embedded servers, standing in for two separate
	// NATS accounts — a single shared js would let each "account" see the
	// other's streams too (same underlying store), which would defeat the
	// point of this test.
	_, tenantJS, cleanupTenant := newTestNATSJS(t)
	defer cleanupTenant()
	_, platformJS, cleanupPlatform := newTestNATSJS(t)
	defer cleanupPlatform()

	if _, err := jstream.CreateStream(ctx, tenantJS, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}
	if _, err := jstream.CreateStream(ctx, platformJS, "REFDATA", []string{"evt.*.refdata.>"}); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: tenantJS}},
		PlatformFullJS:  platformJS,
		Log:             slog.New(slog.DiscardHandler),
	})

	byStream := map[string]string{}
	for _, s := range listStreamsBody(t, h).Streams {
		byStream[s.Stream] = s.Account
	}
	if got := byStream[domain.StreamName]; got != "acme" {
		t.Fatalf("expected %s tagged account=acme, got %q", domain.StreamName, got)
	}
	if got := byStream["REFDATA"]; got != "platform" {
		t.Fatalf("expected REFDATA tagged account=platform, got %q", got)
	}
}

// TestListStreamsIsNotScopedToASingleTenant is the regression test for the bug
// this change fixes: every known tenant's streams come back in one response,
// regardless of which single tenant (if any) REST's Deps.Tenant/Deps.JS mirror
// fields currently point at — those fields aren't even set here, and the
// handler doesn't consult them.
func TestListStreamsIsNotScopedToASingleTenant(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
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

	seen := map[string]bool{}
	for _, s := range listStreamsBody(t, h).Streams {
		seen[s.Account] = true
	}
	for _, want := range []string{"acme", "globex", "platform"} {
		if !seen[want] {
			t.Fatalf("expected a stream tagged account=%q in the response, got accounts %v", want, seen)
		}
	}
}

// Every KV bucket is backed by a KV_<bucket> JetStream stream. Without the
// prefix filter this panel would list all of them and simply duplicate the KV
// Buckets panel next door, so the filter is a rule of the endpoint, not an
// incidental detail.
func TestListStreamsExcludesKVBackingStreams(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}
	if _, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "dict-a"}); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	body := listStreamsBody(t, h)
	if len(body.Streams) != 1 || body.Streams[0].Stream != domain.StreamName {
		t.Fatalf("expected only %s, got %+v", domain.StreamName, body.Streams)
	}
}

func TestListStreamsReportsPerStreamStatus(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}
	for _, subj := range []string{
		"evt.acme.shipping.ship.orient-express.arrived",
		"evt.acme.shipping.container.c1.registered",
	} {
		if _, err := js.Publish(ctx, subj, []byte(`{"context":"acme"}`)); err != nil {
			t.Fatal(err)
		}
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	body := listStreamsBody(t, h)
	if len(body.Streams) != 1 {
		t.Fatalf("expected 1 stream, got %+v", body.Streams)
	}
	got := body.Streams[0]
	if got.Messages != 2 {
		t.Fatalf("expected 2 messages, got %d", got.Messages)
	}
	if got.FirstSeq != 1 || got.LastSeq != 2 {
		t.Fatalf("expected seq range 1..2, got %d..%d", got.FirstSeq, got.LastSeq)
	}
	if got.Bytes == 0 {
		t.Fatal("expected a non-zero byte count")
	}
	if got.Subjects != len(domain.StreamSubjects()) {
		t.Fatalf("expected %d configured subject filters, got %d", len(domain.StreamSubjects()), got.Subjects)
	}
}

// The frontend polls this endpoint every 15s and re-renders the rail from the
// result, so an unstable order would visibly reshuffle the list under the
// operator's cursor. Map iteration over TenantResources is random, so the sort
// is what makes the response deterministic.
func TestListStreamsSortsByAccountThenStream(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	for _, name := range []string{domain.StreamName, "RPCTRACE"} {
		if _, err := jstream.CreateStream(ctx, js, name, []string{"evt." + name + ".>"}); err != nil {
			t.Fatal(err)
		}
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{
			"globex": {js: js},
			"acme":   {js: js},
		},
		PlatformFullJS: js,
		Log:            slog.New(slog.DiscardHandler),
	})

	want := []string{
		"acme/RPCTRACE", "acme/" + domain.StreamName,
		"globex/RPCTRACE", "globex/" + domain.StreamName,
		"platform/RPCTRACE", "platform/" + domain.StreamName,
	}
	// Repeated: a random map order that happens to come out sorted once
	// proves nothing.
	for attempt := 0; attempt < 3; attempt++ {
		got := []string{}
		for _, s := range listStreamsBody(t, h).Streams {
			got = append(got, s.Account+"/"+s.Stream)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("attempt %d: expected %v, got %v", attempt, want, got)
		}
	}
}

func TestListStreamsSkipsThePlatformAccountWhenPlatformFullJSIsNil(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}

	// PlatformFullJS unset — local dev outside Docker, or the connection
	// failed at Startup. PLATFORM's streams just don't appear; the tenant's
	// still do, and the request still succeeds.
	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	for _, s := range listStreamsBody(t, h).Streams {
		if s.Account == "platform" {
			t.Fatalf("expected no platform-tagged streams, got %+v", s)
		}
	}
}
