package rest

// Covers the Phase 12.11 (BR-D28) decision recorded in handlers.go's
// writeRefdataError: since refdataconsumer no longer has a REST fallback,
// a sustained rpc.* outage on a cache miss surfaces as
// refdataconsumer.ErrRPCUnavailable, which these demo endpoints must map to
// 503 (retry later) rather than the generic 500 (unexpected/internal fault).

import (
	"encoding/json"
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

	req := httptest.NewRequest(http.MethodGet, "/api/refdata-demo/acme-test/hazard-class/3", nil)
	req.SetPathValue("context", "acme-test")
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

// TestListRefdataContextsReturns503WhenRPCUnavailable is the same BR-D28
// pattern as the two tests above, applied to the Phase 16f context-list
// endpoint.
func TestListRefdataContextsReturns503WhenRPCUnavailable(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()
	consumer := refdataconsumer.New(nc,
		refdataconsumer.WithRPCTimeout(100*time.Millisecond),
		refdataconsumer.WithRPCRetries(1),
		refdataconsumer.WithRPCBackoff(10*time.Millisecond))
	h := NewHandlers(Deps{Refdata: consumer, Tenant: "acme"})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/contexts", nil)
	w := httptest.NewRecorder()

	h.listRefdataContexts(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListRefdataContextsForwardsActiveTenant proves the Phase 16f wiring
// end to end: the currently active Deps.Tenant (not a hardcoded literal)
// is what travels in the rpc._platform.refdata.context.list.v1 request.
func TestListRefdataContextsForwardsActiveTenant(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	var gotTenant string
	sub, err := nc.Subscribe("rpc._platform.refdata.context.list.v1", func(msg *nats.Msg) {
		var req struct {
			Tenant string `json:"tenant"`
		}
		_ = json.Unmarshal(msg.Data, &req)
		gotTenant = req.Tenant
		data, _ := json.Marshal(struct {
			Contexts []struct {
				Context string `json:"context"`
			} `json:"contexts"`
		}{Contexts: []struct {
			Context string `json:"context"`
		}{{Context: "_platform"}, {Context: "acme-pacific-fleet"}}})
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	consumer := refdataconsumer.New(nc)
	h := NewHandlers(Deps{Refdata: consumer, Tenant: "acme"})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/contexts", nil)
	w := httptest.NewRecorder()

	h.listRefdataContexts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotTenant != "acme" {
		t.Fatalf("expected tenant %q forwarded, got %q", "acme", gotTenant)
	}
	var resp metaValuesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Values) != 2 {
		t.Fatalf("expected 2 contexts, got %v", resp.Values)
	}
}
