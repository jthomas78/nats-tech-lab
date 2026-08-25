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
	"strings"
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

// spanWithIdentity is fullSpan plus the two BR-AC35 identity fields — the
// self-declared caller (lifted onto its own envelope field by BR-041) and
// the answering instance (a header, like it is on an api.* reply).
type spanWithIdentity struct {
	fullSpan
	Requester string              `json:"requester"`
	Headers   map[string][]string `json:"headers"`
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

// TestHTTPTraceMiddlewareRecordsBothHalvesOfTheIdentityPair covers BR-AC35 on
// this service's REST entry point: the requestor half arrives as an HTTP
// header the browser set and is lifted onto the span's own Requester field,
// and the responder half is derived from the tenant connection's nats.Name.
// Before this, a REST span showed neither — the Admin UI's trace detail read
// "no Nats-Requestor / no Nats-Responder on this span" for every REST hop
// while showing both for every api.* hop beside it.
func TestHTTPTraceMiddlewareRecordsBothHalvesOfTheIdentityPair(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/refdata/locales", nil)
	req.Header.Set(natstrace.RequestorHeader, "refdata-app/abc123")
	rec := httptest.NewRecorder()
	wrapped(rec, req)

	select {
	case got := <-spans:
		var span spanWithIdentity
		if err := json.Unmarshal(got.Data, &span); err != nil {
			t.Fatal(err)
		}
		if span.Requester != "refdata-app/abc123" {
			t.Fatalf("expected requester lifted from the inbound header, got %q", span.Requester)
		}
		responder := span.Headers[natstrace.ResponderHeader]
		// "rest-test" is newTestNATSJS's nats.Name — the assertion is on the
		// name coming from the connection, not on a hardcoded service string.
		if len(responder) != 1 || !strings.HasPrefix(responder[0], "rest-test/") {
			t.Fatalf("expected a rest-test/<instance> responder header, got %v", responder)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for obs.trace.* span")
	}
}

// TestHTTPTraceMiddlewareResponderInstanceIsStableAcrossRequests guards the
// reason natstrace mints its instance ID at package level rather than per
// Tracer: this middleware builds a fresh Tracer on every request (to follow
// SwitchTenant), so a per-Tracer ID would report a new "instance" per
// request and tell nothing apart.
func TestHTTPTraceMiddlewareResponderInstanceIsStableAcrossRequests(t *testing.T) {
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

	var seen []string
	for range 2 {
		wrapped(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/refdata/locales", nil))
		select {
		case got := <-spans:
			var span spanWithIdentity
			if err := json.Unmarshal(got.Data, &span); err != nil {
				t.Fatal(err)
			}
			seen = append(seen, span.Headers[natstrace.ResponderHeader]...)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for obs.trace.* span")
		}
	}
	if len(seen) != 2 || seen[0] != seen[1] {
		t.Fatalf("expected one stable responder identity across requests, got %v", seen)
	}
}
