package rest

// BR-073 (Phase 54d): httpTraceMiddleware gives every REST request one OTel
// SERVER span, so an outbound rpc.* call a handler makes has a real parent.
// It replaces the Phase 28m legacy-tracer span. The receiving account is the
// active tenant, read per request so it follows SwitchTenant.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func restProvider(t *testing.T) (*otelnats.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "shipping-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p, exp
}

func restSpans(t *testing.T, p *otelnats.Provider, exp *tracetest.InMemoryExporter) []tracetest.SpanStub {
	t.Helper()
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return exp.GetSpans()
}

func spanAttr(s tracetest.SpanStub, key string) string {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value.Emit()
		}
	}
	return ""
}

func TestHTTPTraceMiddlewareIsOneServerSpanOnTheActiveTenant(t *testing.T) {
	p, exp := restProvider(t)
	h := NewHandlers(Deps{Tenant: "acme", Log: discardLogger(), Tracing: p})
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/tenant", nil))

	spans := restSpans(t, p, exp)
	if len(spans) != 1 || spans[0].SpanKind != trace.SpanKindServer {
		t.Fatalf("expected one SERVER span, got %d", len(spans))
	}
	if spans[0].Name != "GET /api/tenant" {
		t.Fatalf("expected span name GET /api/tenant, got %q", spans[0].Name)
	}
	if got := spanAttr(spans[0], string(otelnats.AttrReceivingAccount)); got != "acme" {
		t.Fatalf("expected receiving account acme, got %q", got)
	}
}

func TestHTTPTraceMiddlewareFollowsSwitchTenant(t *testing.T) {
	p, exp := restProvider(t)
	h := NewHandlers(Deps{Tenant: "acme", Log: discardLogger(), Tracing: p})
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h.SetDeps(Deps{Tenant: "globex", Log: discardLogger(), Tracing: p})

	wrapped(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/tenant", nil))

	spans := restSpans(t, p, exp)
	if got := spanAttr(spans[0], string(otelnats.AttrReceivingAccount)); got != "globex" {
		t.Fatalf("expected the switched tenant globex, got %q", got)
	}
}

func TestHTTPTraceMiddlewareMarksA5xxAsError(t *testing.T) {
	p, exp := restProvider(t)
	h := NewHandlers(Deps{Tenant: "acme", Log: discardLogger(), Tracing: p})
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	wrapped(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/tenant", nil))

	if got := restSpans(t, p, exp)[0].Status.Code; got != codes.Error {
		t.Fatalf("expected ERROR status for a 503, got %v", got)
	}
}

// The gap Phase 28m closed, kept on OTel: a handler's outbound call is a
// child of the REST span, not a new root.
func TestHTTPTraceMiddlewareGivesDownstreamOutboundCallsAParentSpan(t *testing.T) {
	p, exp := restProvider(t)
	h := NewHandlers(Deps{Tenant: "acme", Log: discardLogger(), Tracing: p})
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, r *http.Request) {
		_, child := p.StartClient(r.Context(), "refdata.type.list.v1", "refdata")
		child.End()
		w.WriteHeader(http.StatusOK)
	})

	wrapped(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/admin/ports/acme", nil))

	var server, client tracetest.SpanStub
	for _, s := range restSpans(t, p, exp) {
		switch s.SpanKind {
		case trace.SpanKindServer:
			server = s
		case trace.SpanKindClient:
			client = s
		}
	}
	if client.Parent.SpanID() != server.SpanContext.SpanID() {
		t.Fatal("expected the outbound CLIENT span to be a child of the REST SERVER span")
	}
}

// BR-041: the caller's self-declared requester is kept, for display only.
func TestHTTPTraceMiddlewareKeepsTheSelfDeclaredRequester(t *testing.T) {
	p, exp := restProvider(t)
	h := NewHandlers(Deps{Tenant: "acme", Log: discardLogger(), Tracing: p})
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r := httptest.NewRequest(http.MethodGet, "/api/tenant", nil)
	r.Header.Set(otelnats.RequestorHeader, "refdata-app/abc123")

	wrapped(httptest.NewRecorder(), r)

	if got := spanAttr(restSpans(t, p, exp)[0], string(otelnats.AttrRequester)); got != "refdata-app/abc123" {
		t.Fatalf("expected lab.requester refdata-app/abc123, got %q", got)
	}
}

func TestHTTPTraceMiddlewareWithNoProviderJustServes(t *testing.T) {
	h := NewHandlers(Deps{Log: discardLogger()})
	called := false
	wrapped := h.httpTraceMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()

	wrapped(rec, httptest.NewRequest(http.MethodGet, "/api/tenant", nil))

	if !called || rec.Code != http.StatusOK {
		t.Fatalf("expected the handler to run and return 200, got called=%v code=%d", called, rec.Code)
	}
}
