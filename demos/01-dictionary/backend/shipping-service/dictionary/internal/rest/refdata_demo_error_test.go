package rest

// Covers the Phase 12.11 (BR-D28) decision recorded in handlers.go's
// writeRefdataError: since refdataconsumer no longer has a REST fallback,
// a sustained rpc.* outage on a cache miss surfaces as
// refdataconsumer.ErrRPCUnavailable, which these demo endpoints must map to
// 503 (retry later) rather than the generic 500 (unexpected/internal fault).

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/refdataconsumer"
)

// newTestNATS starts an embedded in-process NATS server for refdataconsumer's
// rpc.* calls — same embedded-server convention as
// internal/refdataconsumer/consumer_test.go. No JetStream/KV needed: the
// RPC-only refactor (BR-D08) removed the consumer's KV dependency entirely.
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
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("rest-handlers-test"))
	if err != nil {
		t.Fatal(err)
	}
	return nc, func() { nc.Close(); srv.Shutdown() }
}

func TestGetRefdataDemoReturns503WhenRPCUnavailable(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()
	// No rpc.* responder — retries exhaust into ErrRPCUnavailable, not a
	// real not-found or success.
	consumer := refdataconsumer.New(nc,
		refdataconsumer.WithRPCTimeout(100*time.Millisecond),
		refdataconsumer.WithRPCRetries(1),
		refdataconsumer.WithRPCBackoff(10*time.Millisecond))
	h := NewHandlers(Deps{Refdata: consumer})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata-demo/emea-acme/hazard-class/3", nil)
	req.SetPathValue("context", "emea-acme")
	req.SetPathValue("type", "hazard-class")
	req.SetPathValue("code", "3")
	w := httptest.NewRecorder()

	h.getRefdataDemo(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListRefdataTypeReturns503WhenRPCUnavailable(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()
	consumer := refdataconsumer.New(nc,
		refdataconsumer.WithRPCTimeout(100*time.Millisecond),
		refdataconsumer.WithRPCRetries(1),
		refdataconsumer.WithRPCBackoff(10*time.Millisecond))
	h := NewHandlers(Deps{Refdata: consumer})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/types/hazard-class", nil)
	req.SetPathValue("type", "hazard-class")
	w := httptest.NewRecorder()

	h.listRefdataType(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListRefdataLocalesReturns503WhenRPCUnavailable(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()
	consumer := refdataconsumer.New(nc,
		refdataconsumer.WithRPCTimeout(100*time.Millisecond),
		refdataconsumer.WithRPCRetries(1),
		refdataconsumer.WithRPCBackoff(10*time.Millisecond))
	h := NewHandlers(Deps{Refdata: consumer})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/locales", nil)
	w := httptest.NewRecorder()

	h.listRefdataLocales(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}
