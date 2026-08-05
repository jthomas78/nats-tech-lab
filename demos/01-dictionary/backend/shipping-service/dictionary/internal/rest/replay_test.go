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

	h := NewHandlers(Deps{JS: js, Log: slog.New(slog.DiscardHandler)})

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
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %s", len(events), rec.Body.String())
	}
}

func TestJetstreamReplayOnceReturnsEmptyArrayForAnEmptyStream(t *testing.T) {
	_, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{JS: js, Log: slog.New(slog.DiscardHandler)})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay", nil)
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

	h := NewHandlers(Deps{JS: js, Log: slog.New(slog.DiscardHandler)})

	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?stream=NOPE", nil)
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
