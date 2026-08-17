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

func TestJetstreamReplayOnceReturnsAllRetainedMessages(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "REFDATA", Subjects: []string{"evt.>"}}); err != nil {
		t.Fatalf("create stream: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := js.Publish(ctx, "evt.acme.refdata.item.changed", []byte(`{"n":`+string(rune('0'+i))+`}`)); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=platform&stream=REFDATA", nil)
	w := httptest.NewRecorder()

	h.jetstreamReplayOnce(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var events []jsEvent
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Seq != 1 || events[2].Seq != 3 {
		t.Fatalf("expected sequence order 1..3, got %+v", events)
	}

	// The ephemeral consumer must be cleaned up after use (BR-AC32's design
	// note — DeleteConsumer's name must come from the server, never the
	// request) — verify no consumer is left registered on the stream.
	stream, err := js.Stream(ctx, "REFDATA")
	if err != nil {
		t.Fatalf("lookup stream: %v", err)
	}
	names := stream.ConsumerNames(ctx)
	leftover := 0
	for range names.Name() {
		leftover++
	}
	if leftover != 0 {
		t.Fatalf("expected the replay consumer to be deleted after use, found %d leftover", leftover)
	}
}

func TestJetstreamReplayOnceReturnsEmptyForEmptyStream(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "EMPTY", Subjects: []string{"empty.>"}}); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=platform&stream=EMPTY", nil)
	w := httptest.NewRecorder()

	h.jetstreamReplayOnce(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var events []jsEvent
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if events == nil || len(events) != 0 {
		t.Fatalf("expected an empty (non-nil) event list, got %+v", events)
	}
}

func TestJetstreamReplayOnceReturns400WhenAccountOrStreamMissing(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})

	for _, url := range []string{
		"/api/jetstream/replay",
		"/api/jetstream/replay?account=platform",
		"/api/jetstream/replay?stream=REFDATA",
	} {
		w := httptest.NewRecorder()
		h.jetstreamReplayOnce(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", url, w.Code, w.Body.String())
		}
	}
}

func TestJetstreamReplayOnceReturns400ForUnknownAccount(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=nope&stream=REFDATA", nil)
	w := httptest.NewRecorder()

	h.jetstreamReplayOnce(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJetstreamReplayOnceReturns400ForUnknownStream(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	req := httptest.NewRequest(http.MethodGet, "/api/jetstream/replay?account=platform&stream=NOPE", nil)
	w := httptest.NewRecorder()

	h.jetstreamReplayOnce(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
