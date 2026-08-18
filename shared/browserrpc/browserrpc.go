// Package browserrpc is the shared infra tail for every service's api.*
// frontend-to-service adapter — extracted in Phase 35 from four
// near-identical copies (shipping-service, pricing-service,
// trading-partner-service, refdata-service's own internal/browserrpc
// packages). What's shared is purely mechanical NATS/JSON plumbing:
// pulling {context} off an api.* subject, marshaling a reply, finishing the
// natstrace span, and stamping the Nats-Responder header micro.Request's
// own Respond doesn't set. What genuinely differs per service — Deps,
// Adapter, subject constants, wire-shape request/response structs, and
// every handle* endpoint method — stays in each service's own
// internal/browserrpc package. This package never imports any service's
// domain or command/query types; the direction of dependency is service ->
// shared/browserrpc, never the reverse (mirrors shared/natstenants' own
// rule).
//
// A service's "not found" sentinel errors are its own domain types, so
// Reply/RespondError take an isNotFound predicate rather than hard-coding
// any one service's errors.Is chain.
package browserrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// ResponderHeader identifies which service — and which running instance,
// via micro's own auto-generated per-process instance ID — answered a
// request. Mirrors the Nats-Requestor header on the caller-identity side:
// NATS doesn't propagate responder identity onto a reply either, and the
// subject alone doesn't distinguish which replica of a horizontally-scaled
// service actually handled the call.
const ResponderHeader = "Nats-Responder"

// ErrorResponse is the wire shape for every failed api.* call, shared
// across every service's adapter so a browser client handles all of their
// errors identically.
type ErrorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound,omitempty"`
}

// ContextFromSubject extracts the {context} token from an api.{context}....
// subject. This is the ONLY source of truth for which CONTEXT — the company
// / business-unit scope — a request belongs to: every handler overwrites
// any "context" field a request body might carry with this value before
// calling into the application layer, so scoping can't be spoofed via the
// body.
//
// Context is NOT the tenant. The tenant boundary is the NATS ACCOUNT the
// connection authenticated into, and the tenant name never appears in this
// subject (nor does the region, which is a separate regional deployment).
// See ARCHITECTURE-COMMUNICATIONS.md § 2.3 and BR-023.
func ContextFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// ResponderIdentity is "<service name>/<instance ID>" — svc.Info().ID is a
// fresh, unique value per running process (assigned by micro.AddService),
// so this changes across restarts/replicas without any config of the
// caller's own.
func ResponderIdentity(svc micro.Service) string {
	info := svc.Info()
	return fmt.Sprintf("%s/%s", info.Name, info.ID)
}

// Respond marshals out, sends the reply, and only then finishes this
// request's natstrace span (BR-036/BR-037). Reply-before-trace is
// deliberate, not incidental: End does a redact scan, a full JSON-marshal
// of the span, and a Publish, and none of that may sit between the caller
// getting its answer and the caller actually getting it — an rpc.*/api.*
// call's measured duration must not include natstrace's own overhead. If
// req.Respond itself errors, the span still finishes so the failure isn't
// silently untraced. ResponderHeader is attached to both the real wire
// reply and the span's headers (mirroring how RespondError attaches the
// real error headers to both). natstrace.SpanFrom is nil-safe, so a
// handler invoked directly (not through Tracer.Middleware, e.g. a unit
// test calling a handler with a bare micro.Request) still replies
// correctly, it just publishes no span.
func Respond(req micro.Request, svc micro.Service, log *slog.Logger, subject, correlationID string, out any) {
	data, err := json.Marshal(out)
	if err != nil {
		RespondError(req, svc, log, subject, correlationID, err, false)
		return
	}
	headers := map[string][]string{ResponderHeader: {ResponderIdentity(svc)}}
	if err := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); err != nil && log != nil {
		log.Error("browserrpc: respond failed", "subject", subject, "err", err)
	}
	natstrace.SpanFrom(req).End(data, headers)
}

// RespondError sends the reply and only then finishes this request's
// natstrace span on failure (BR-036/BR-037) — same reply-before-trace
// ordering as Respond, for the same reason: a failed call's measured
// duration must not include natstrace's own redact/marshal/publish
// overhead either. The reply carries real Nats-Service-Error/
// Nats-Service-Error-Code headers (micro's own error-header convention) via
// WithHeaders — additive to the existing JSON error body, so no client that
// reads the body (NotFound bool, error string) needs to change. notFound
// controls both the JSON body's NotFound field and whether the error code
// header reads "404" instead of "500" — callers derive it from their own
// service-specific isNotFound predicate.
func RespondError(req micro.Request, svc micro.Service, log *slog.Logger, subject, correlationID string, err error, notFound bool) {
	data, _ := json.Marshal(ErrorResponse{Error: err.Error(), NotFound: notFound})
	code := "500"
	if notFound {
		code = "404"
	}
	headers := map[string][]string{
		micro.ErrorHeader:     {err.Error()},
		micro.ErrorCodeHeader: {code},
		ResponderHeader:       {ResponderIdentity(svc)},
	}
	if respErr := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); respErr != nil && log != nil {
		log.Error("browserrpc: respond failed", "subject", subject, "err", respErr)
	}
	natstrace.SpanFrom(req).Fail(err, data, headers)
}

// Reply is the shared tail end of every handler: nil error replies with
// result, any error replies with the mapped error response. subject and
// correlationID are derived from req itself (req.Subject()/req.Reply()),
// matching the convention three of the four original per-service copies
// already used. isNotFound classifies err as a domain not-found condition
// (a 404, not a 500) — nil is treated as "never not-found".
func Reply(req micro.Request, svc micro.Service, log *slog.Logger, isNotFound func(error) bool, result any, err error) {
	subject := req.Subject()
	correlationID := req.Reply()
	if err != nil {
		RespondError(req, svc, log, subject, correlationID, err, isNotFound != nil && isNotFound(err))
		return
	}
	Respond(req, svc, log, subject, correlationID, result)
}

// SpanContext attaches req's natstrace span (nil-safe) onto a fresh
// background context — the call every handler makes to get a ctx worth
// passing into its application-layer call, since req's own context is
// scoped to the NATS request lifecycle, not to the handler's downstream
// work.
func SpanContext(req micro.Request) context.Context {
	return natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))
}

// Decode unmarshals req's body into a fresh T, tolerating an empty body
// (leaving T's zero value) rather than failing — most endpoints here take
// no arguments beyond {context}, which travels in the subject, not the
// body.
func Decode[T any](req micro.Request) (T, error) {
	var in T
	if len(req.Data()) > 0 {
		if err := json.Unmarshal(req.Data(), &in); err != nil {
			return in, err
		}
	}
	return in, nil
}
