package rest

// BR-036/BR-037 (Phase 28m): httpTraceMiddleware gives every REST request a
// span of its own so an outbound rpc.* call an handler triggers (via
// internal/refdataconsumer's StartOutbound) has a real parent to continue,
// instead of minting an untraceable root — see BUSINESS_RULES-SHIPPING.md's
// Phase 28m amendment for the full before/after.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// fullSpan decodes an obs.trace.* span payload (Phase 35 moved this here
// from the now-deleted tenant_lifecycle_trace_test.go — its own trace
// propagation coverage moved to shared/natstenants, but this file's own
// tests still need the shape).
type fullSpan struct {
	TraceID      string `json:"traceId"`
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId,omitempty"`
	Service      string `json:"service"`
	Entity       string `json:"entity"`
	Action       string `json:"action"`
	StatusCode   string `json:"statusCode"`
}

// spanWithSubject is fullSpan plus Subject — this file's third test needs to
// tell the HTTP span and its outbound child apart by subject, which fullSpan
// doesn't carry.
type spanWithSubject struct {
	fullSpan
	Subject string `json:"subject"`
}

func newTraceMiddlewareTestHandlers(t *testing.T, nc *nats.Conn) *Handlers {
	t.Helper()
	h := NewHandlers(Deps{Tenant: "acme", TenantNC: nc, Log: discardLogger()})
	return h
}

func TestHTTPTraceMiddlewarePublishesASpanCarryingTheRequestPathAndEntity(t *testing.T) {
	nc, _, cleanup := newTestNATSJS(t)
	defer cleanup()

	spans := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	h := newTraceMiddlewareTestHandlers(t, nc)
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/types/ship-status", nil)
	rec := httptest.NewRecorder()
	wrapped(rec, req)

	select {
	case got := <-spans:
		var span fullSpan
		if err := json.Unmarshal(got.Data, &span); err != nil {
			t.Fatal(err)
		}
		if span.Service != "shipping" || span.Entity != "refdata" {
			t.Fatalf("expected service/entity shipping/refdata, got %s/%s", span.Service, span.Entity)
		}
		if span.StatusCode != "OK" {
			t.Fatalf("expected statusCode OK for a 200 response, got %s", span.StatusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for obs.trace.* span")
	}
}

func TestHTTPTraceMiddlewareFailsTheSpanOnA4xxOr5xxResponse(t *testing.T) {
	nc, _, cleanup := newTestNATSJS(t)
	defer cleanup()

	spans := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	h := newTraceMiddlewareTestHandlers(t, nc)
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/locales", nil)
	rec := httptest.NewRecorder()
	wrapped(rec, req)

	select {
	case got := <-spans:
		var span fullSpan
		if err := json.Unmarshal(got.Data, &span); err != nil {
			t.Fatal(err)
		}
		if span.StatusCode != "ERROR" {
			t.Fatalf("expected statusCode ERROR for a 503 response, got %s", span.StatusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for obs.trace.* span")
	}
}

// TestHTTPTraceMiddlewareGivesDownstreamOutboundCallsAParentSpan is the
// specific gap this middleware closes: before it existed, an outbound rpc.*
// call made from inside a handler (via refdataconsumer.StartOutbound) found
// no span on r.Context() and minted its own untraceable root, hiding the
// real browser-originated request. This asserts a handler-level
// StartOutbound call now continues the HTTP span's trace instead.
func TestHTTPTraceMiddlewareGivesDownstreamOutboundCallsAParentSpan(t *testing.T) {
	nc, _, cleanup := newTestNATSJS(t)
	defer cleanup()

	spans := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	h := newTraceMiddlewareTestHandlers(t, nc)
	tracer := natstrace.New(nc)
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, r *http.Request) {
		child := tracer.StartOutbound(natstrace.SpanFromContext(r.Context()), "refdata.type.list.v1", nil, "acme", "refdata", "type", "list")
		child.End(nil, nil)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/types/ship-status", nil)
	rec := httptest.NewRecorder()
	wrapped(rec, req)

	got := map[string]spanWithSubject{}
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case msg := <-spans:
			var span spanWithSubject
			if err := json.Unmarshal(msg.Data, &span); err != nil {
				t.Fatal(err)
			}
			got[span.Subject] = span
		case <-timeout:
			t.Fatalf("timed out waiting for 2 spans, got %d", len(got))
		}
	}

	httpSpan, ok := got["/api/refdata/types/ship-status"]
	if !ok {
		t.Fatalf("no HTTP span published, got subjects: %#v", got)
	}
	childSpan, ok := got["refdata.type.list.v1"]
	if !ok {
		t.Fatalf("no outbound child span published, got subjects: %#v", got)
	}
	if childSpan.TraceID != httpSpan.TraceID {
		t.Fatalf("expected child traceId %q to match HTTP span traceId %q", childSpan.TraceID, httpSpan.TraceID)
	}
	if childSpan.ParentSpanID != httpSpan.SpanID {
		t.Fatalf("expected child parentSpanId %q to equal HTTP span's spanId %q — the gap this middleware closes", childSpan.ParentSpanID, httpSpan.SpanID)
	}
}

func TestHTTPTraceMiddlewareSkipsTracingWithNoTenantNC(t *testing.T) {
	h := NewHandlers(Deps{Log: discardLogger()}) // TenantNC nil — no tenant resources yet
	called := false
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/locales", nil)
	rec := httptest.NewRecorder()

	wrapped(rec, req) // must not panic despite a nil TenantNC

	if !called {
		t.Fatal("expected the wrapped handler to still run")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
