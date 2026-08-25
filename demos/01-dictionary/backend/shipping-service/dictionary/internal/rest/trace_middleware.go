package rest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// httpTraceMiddleware closes the gap where a browser HTTP request into this
// service's REST API had no span of its own: internal/refdataconsumer's
// StartOutbound (BR-037) looks for a parent span via
// natstrace.SpanFromContext(ctx) and, finding none, mints a fresh root span
// for its own outbound rpc.* call — so a trace like GET /api/refdata/types/
// {type} showed shipping-service as the apparent originator, with the real
// browser-originated request invisible. Wrapping every route here (mirroring
// accounts-service's natstrace.HTTPMiddleware, this package's http.Handler
// counterpart of natstrace.Tracer.Middleware for micro.Request) means every
// handler now has a span attached to r.Context() that refdataconsumer (and
// any future outbound call) can continue via ContextWithSpan/SpanFromContext.
//
// Unlike accounts-service's copy — which is wrapped once at startup because
// its NC never changes — this reads h.deps() on every request rather than
// closing over a single Deps snapshot, so the span always publishes on the
// CURRENTLY active tenant's connection (deps.TenantNC), tracking
// SwitchTenant exactly like every other per-request field on Deps. A nil
// TenantNC (no tenant resources yet — e.g. very first requests during
// Startup) skips tracing entirely rather than risk a nil-pointer publish;
// every other natstrace entry point in this codebase is similarly best-effort
// (BR-036: a tracing failure must never block or fail the business path it
// describes).
func (h *Handlers) httpTraceMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deps := h.deps()
		if deps.TenantNC == nil {
			next(w, r)
			return
		}

		// Body must be re-buffered before next(w, r) consumes the stream —
		// http.Request.Body is read-once, and the handler downstream still
		// needs the real bytes to decode its own request, so this captures a
		// copy for the span and restores an equivalent io.ReadCloser for the
		// handler rather than diverting the original.
		var reqBody []byte
		if r.Body != nil {
			reqBody, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(reqBody))
		}

		tracer := natstrace.New(deps.TenantNC)
		sp := tracer.StartFromHeaders(nats.Header(r.Header), r.URL.Path, reqBody, deps.Tenant, "shipping", httpEntity(r.URL.Path), strings.ToLower(r.Method))
		sp.SetAttribute("http.method", r.Method)
		sp.SetAttribute("http.path", r.URL.Path)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rec, r.WithContext(natstrace.ContextWithSpan(r.Context(), sp)))

		sp.SetAttribute("http.status_code", strconv.Itoa(rec.status))
		// The responder half of BR-AC35's identity pair, from the tenant
		// connection's own nats.Name — the REST counterpart of the
		// Nats-Responder header browserrpc.Respond puts on an api.* reply.
		// The requestor half arrives on the request itself and is lifted
		// onto the span by StartFromHeaders/finish, so it needs nothing here.
		respHeaders := tracer.ResponderHeaders()
		if rec.status >= 400 {
			sp.Fail(fmt.Errorf("http %d", rec.status), nil, respHeaders)
			return
		}
		sp.End(nil, respHeaders)
	}
}

// statusRecorder captures the HTTP status code a wrapped handler writes, so
// httpTraceMiddleware can tell End from Fail after the handler returns —
// net/http's ResponseWriter has no built-in way to read back what a handler
// wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// httpEntity derives a coarse span "entity" label from a request path —
// "/api/refdata/types/ship-status" and "/api/ships/arrive" become "refdata"
// and "ships" respectively. The exact path (which carries the real detail)
// is recorded as the "http.path" attribute instead of forced into this
// coarse field.
func httpEntity(path string) string {
	trimmed := strings.TrimPrefix(path, "/api/")
	parts := strings.SplitN(trimmed, "/", 2)
	if parts[0] == "" {
		return "root"
	}
	return parts[0]
}
