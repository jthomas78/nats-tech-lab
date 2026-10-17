package otelnats

import (
	"bytes"
	"io"
	"net/http"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware wraps next so each inbound REST request is one SERVER span
// on this Provider (BR-073). It is the standard otelhttp handler with this
// lab's policy on top, not a second HTTP tracer:
//
//   - Edge policy as BR-071's EdgeAPI. A REST caller is not a trusted
//     service, so every request starts a new, sampled root, and a valid
//     traceparent it sent becomes one link (otelhttp's public endpoint).
//   - The receiving account comes from the caller of this function, never
//     from the request (BR-069). No originating tenant is set.
//   - With CaptureBodies, the request body is an rpc.request.body event,
//     redacted then truncated (BR-036). The handler still reads the whole
//     body.
//
// A handler that publishes with r.Context() makes its PRODUCER span a child
// of this SERVER span, so one REST operation is one trace. Error status
// follows OTel's HTTP server convention: 5xx only.
func (p *Provider) HTTPMiddleware(receivingAccount string, next http.Handler) http.Handler {
	return p.HTTPMiddlewareFor(func(*http.Request) string { return receivingAccount }, next)
}

// HTTPMiddlewareFor is HTTPMiddleware with the receiving account read per
// request, for a service whose active account can change between requests
// (shipping-service's SwitchTenant, Phase 54d). receivingAccount is the
// caller's own state, never a value from the request (BR-069). A
// self-declared Nats-Requestor header becomes lab.requester (BR-041).
func (p *Provider) HTTPMiddlewareFor(receivingAccount func(*http.Request) string, next http.Handler) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(AttrReceivingAccount.String(receivingAccount(r)))
		if requester := r.Header.Get(RequestorHeader); requester != "" {
			span.SetAttributes(AttrRequester.String(requester))
		}
		if p.cfg.CaptureBodies && r.Body != nil && span.IsRecording() {
			body, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err == nil {
				p.RecordRequestBody(span, body, nats.Header(r.Header))
			}
		}
		next.ServeHTTP(w, r)
	})
	return otelhttp.NewHandler(inner, "http.server",
		otelhttp.WithTracerProvider(p.tp),
		otelhttp.WithPropagators(propagator),
		// Every request is public: no REST caller is a trusted service.
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}
