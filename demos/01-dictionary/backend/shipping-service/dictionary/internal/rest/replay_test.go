package rest

// Tests for Phase 23's one-shot REST bootstrap endpoints, replacing the
// full-history half of replayJetStream/watchRPCObs's SSE streams: a single
// JSON array snapshotted at request time, instead of holding a connection
// open. Live updates after this snapshot arrive via notify.* (see
// dictionary/internal/eventhandler's publishRawNotify/RegisterRPCTraceNotify),
// not this package.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
)

func TestJetstreamReplayOnceReturnsAllRetainedMessagesAsOneJSONArray(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}
	for _, subj := range []string{"evt.acme.shipping.ship.orient-express.arrived", "evt.acme.shipping.container.c1.registered"} {
		if _, err := js.Publish(ctx, subj, []byte(`{"context":"acme"}`)); err != nil {
			t.Fatal(err)
		}
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=acme", nil)
	rec := httptest.NewRecorder()
	h.jetstreamReplayOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var events []jsEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %s", len(events), rec.Body.String())
	}
}

// Stream names are only unique WITHIN an account — every tenant provisions its
// own SHIPPING — so a bare ?stream= is ambiguous. ?account= is what disambiguates
// it, exactly as the {account} path segment does for the KV entries endpoint.
func TestJetstreamReplayOnceResolvesTheStreamInTheRequestedAccount(t *testing.T) {
	ctx := context.Background()
	// Two separate embedded servers standing in for two accounts, each with
	// its OWN SHIPPING stream — a shared js would make this test vacuous.
	_, acmeJS, cleanupAcme := newTestNATSJS(t)
	defer cleanupAcme()
	_, globexJS, cleanupGlobex := newTestNATSJS(t)
	defer cleanupGlobex()

	for _, js := range []jetstream.JetStream{acmeJS, globexJS} {
		if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
			t.Fatal(err)
		}
	}
	// One event in ACME's SHIPPING, three in GLOBEX's.
	if _, err := acmeJS.Publish(ctx, "evt.acme.shipping.ship.orient-express.arrived", []byte(`{"context":"acme"}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c1", "c2", "c3"} {
		if _, err := globexJS.Publish(ctx, "evt.globex.shipping.container."+id+".registered", []byte(`{"context":"globex"}`)); err != nil {
			t.Fatal(err)
		}
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{
			"acme":   {js: acmeJS},
			"globex": {js: globexJS},
		},
		// The active-tenant mirror fields deliberately point at ACME: an
		// explicit ?account=globex must NOT fall back to them.
		Tenant: "acme",
		JS:     acmeJS,
		Log:    slog.New(slog.DiscardHandler),
	})

	for _, tc := range []struct {
		account string
		want    int
	}{{"acme", 1}, {"globex", 3}} {
		req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account="+tc.account, nil)
		rec := httptest.NewRecorder()
		h.jetstreamReplayOnce(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("account %s: expected 200, got %d: %s", tc.account, rec.Code, rec.Body.String())
		}
		var events []jsEvent
		if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
			t.Fatalf("account %s: unmarshal response: %v", tc.account, err)
		}
		if len(events) != tc.want {
			t.Fatalf("account %s: expected %d events, got %d: %s", tc.account, tc.want, len(events), rec.Body.String())
		}
	}
}

// Omitting ?account= keeps the pre-change behaviour: replay the stream in
// whichever tenant REST currently has active. Deps.JS is that same tenant's
// JetStream context (SwitchTenant sets both from one tenantResources bundle),
// so this is not a behaviour change for existing callers — only a
// disambiguation for new ones.
func TestJetstreamReplayOnceDefaultsToTheActiveTenantWhenAccountOmitted(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}
	if _, err := js.Publish(ctx, "evt.acme.shipping.ship.orient-express.arrived", []byte(`{"context":"acme"}`)); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{
		Tenant:          "acme",
		JS:              js,
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay", nil)
	rec := httptest.NewRecorder()
	h.jetstreamReplayOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var events []jsEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %s", len(events), rec.Body.String())
	}
}

func TestJetstreamReplayOnceReturns400ForAnUnknownAccount(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=nope", nil)
	rec := httptest.NewRecorder()
	h.jetstreamReplayOnce(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The two-aggregate FilterSubjects filter belongs to the tenant SHIPPING
// stream alone — PLATFORM's streams carry unrelated subject taxonomies
// (evt.*.refdata.*.changed, obs.rpc.>) that the ship/container filters would
// exclude entirely, leaving a permanently empty panel.
func TestJetstreamReplayOnceAppliesNoSubjectFilterToPlatformStreams(t *testing.T) {
	ctx := context.Background()
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	if _, err := jstream.CreateStream(ctx, js, "REFDATA", []string{"evt.*.refdata.>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := js.Publish(ctx, "evt.acme.refdata.port.changed", []byte(`{"typeKey":"port"}`)); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{PlatformFullJS: js, Log: slog.New(slog.DiscardHandler)})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=platform&stream=REFDATA", nil)
	rec := httptest.NewRecorder()
	h.jetstreamReplayOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var events []jsEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %s", len(events), rec.Body.String())
	}
}

func TestJetstreamReplayOnceReturnsEmptyArrayForAnEmptyStream(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=acme", nil)
	rec := httptest.NewRecorder()
	h.jetstreamReplayOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("expected an empty JSON array, got: %s", rec.Body.String())
	}
}

func TestJetstreamReplayOnceReturns400ForAnUnknownStream(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	h := NewHandlers(Deps{
		TenantResources: map[string]*tenantResources{"acme": {js: js}},
		Log:             slog.New(slog.DiscardHandler),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=acme&stream=NOPE", nil)
	rec := httptest.NewRecorder()
	h.jetstreamReplayOnce(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRPCTraceReplayOnceReturnsAllRetainedEntries(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := jstream.CreateStream(ctx, js, "RPCTRACE", []string{"obs.rpc.>"}); err != nil {
		t.Fatal(err)
	}
	backlog := `{"direction":"request","correlationId":"backlog-1"}`
	if _, err := js.Publish(ctx, "obs.rpc.acme.refdata.item.get.v1", []byte(backlog)); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{PlatformJS: js, Log: slog.New(slog.DiscardHandler)})

	req := httptest.NewRequest(http.MethodGet, "/api/rpctrace/replay", nil)
	rec := httptest.NewRecorder()
	h.rpcTraceReplayOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %s", len(entries), rec.Body.String())
	}
	if string(entries[0]) != backlog {
		t.Fatalf("expected backlog entry verbatim, got: %s", entries[0])
	}
}

func TestRPCTraceReplayOnceReturnsEmptyArrayWhenPlatformJSNil(t *testing.T) {
	h := NewHandlers(Deps{Log: slog.New(slog.DiscardHandler)})

	req := httptest.NewRequest(http.MethodGet, "/api/rpctrace/replay", nil)
	rec := httptest.NewRecorder()
	h.rpcTraceReplayOnce(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("expected an empty JSON array, got: %s", rec.Body.String())
	}
}
