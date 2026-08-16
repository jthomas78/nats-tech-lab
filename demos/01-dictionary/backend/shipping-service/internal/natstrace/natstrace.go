// Package natstrace is shipping-service's Phase 28b copy of
// hand-rolled distributed tracing (BR-036/BR-037) — no
// go.opentelemetry.io/* dependency, W3C-compatible on the wire,
// OTLP-shaped in its fields. It is deliberately duplicated per service (not a
// shared module), the same way obsEnvelope/errorResponse/obsSubjectFor/
// contextFromSubject already are in this codebase — see
// ARCHITECTURE-COMMUNICATIONS.md § 6 for why.
//
// The pattern: a Tracer (constructed once per Adapter, never a package
// singleton — Adapter is itself one per tenant NATS connection) wraps every
// micro.HandlerFunc at its single svc.AddEndpoint call site. The wrap starts a
// span from the inbound request (continuing an incoming traceparent header if
// present, otherwise minting a root span) and hands the handler a wrapped
// micro.Request carrying that span. The handler's existing respond/
// respondError tail retrieves it via SpanFrom (nil-safe: a request that was
// never wrapped — e.g. called directly in a unit test — yields a nil *Span,
// and every Span method is a no-op on nil) and calls End or Fail exactly
// once, which is what actually publishes the span to
// obs.trace.{context}.{service}.{entity}.{action} — PLATFORM account only,
// per BR-036.
package natstrace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// TraceparentHeader is the header name carrying W3C trace context on the
// wire: "00-<32 hex trace id>-<16 hex parent span id>-<2 hex flags>".
// Capitalized to match this codebase's other NATS header names
// (Nats-Requestor, Nats-Responder, X-Actor) — nats.Header.Get is
// case-sensitive (unlike net/http.Header), so this must match exactly what
// callers set.
const TraceparentHeader = "Traceparent"

// maxPayloadBytes is BR-036's 4 KiB cap, applied after redaction, never
// before — truncating first could leave a partially-redacted field's tail
// bytes in the published payload.
const maxPayloadBytes = 4096

// redactDenylist is the set of JSON object keys (case-insensitive, matched at
// any nesting depth) stripped from a span's payload before publishing.
var redactDenylist = map[string]struct{}{
	"password":      {},
	"secret":        {},
	"token":         {},
	"apikey":        {},
	"api_key":       {},
	"ssn":           {},
	"creditcard":    {},
	"credit_card":   {},
	"authorization": {},
	"privatekey":    {},
	"private_key":   {},
}

// traceSpan is BR-036's wire envelope — a strict superset of the pre-existing
// obsEnvelope (direction/correlationId/subject/payload/error/headers/
// timestamp/payloadBytes: no field renamed or retyped) plus OTLP-shaped
// tracing fields, all omitempty so a pre-Phase-28 consumer decoding an old
// obs.rpc.*/obs.api.* envelope is unaffected by this type ever existing.
type traceSpan struct {
	Direction     string              `json:"direction"`
	CorrelationID string              `json:"correlationId"`
	Subject       string              `json:"subject"`
	Payload       json.RawMessage     `json:"payload,omitempty"`
	Error         string              `json:"error,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
	PayloadBytes  int                 `json:"payloadBytes"`

	TraceID       string            `json:"traceId,omitempty"`
	SpanID        string            `json:"spanId,omitempty"`
	ParentSpanID  string            `json:"parentSpanId,omitempty"`
	Service       string            `json:"service,omitempty"`
	Entity        string            `json:"entity,omitempty"`
	Action        string            `json:"action,omitempty"`
	StatusCode    string            `json:"statusCode,omitempty"`
	StatusMessage string            `json:"statusMessage,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	Redacted      []string          `json:"redacted,omitempty"`
	Truncated     bool              `json:"truncated,omitempty"`

	// RequestPayload closes the gap flagged in TraceWaterfall.vue's "Request
	// body — not captured yet" note: the inbound request body captured at
	// Start()/StartFromHeaders()/StartOutbound() construction time, redacted
	// and truncated the same way Payload (the reply body) already is. Separate
	// Redacted/Truncated tracking from the reply side since the two payloads
	// are independently sized/shaped — e.g. a large request with a tiny reply
	// truncates only RequestPayload.
	RequestPayload      json.RawMessage `json:"requestPayload,omitempty"`
	RequestPayloadBytes int             `json:"requestPayloadBytes,omitempty"`
	RequestRedacted     []string        `json:"requestRedacted,omitempty"`
	RequestTruncated    bool            `json:"requestTruncated,omitempty"`
	// DurationMs is the span's own measured duration (Phase 28g) — from
	// Start/StartFromHeaders/StartOutbound's construction moment to
	// End/Fail's finish() call, never derived by a consumer subtracting two
	// timestamps (Timestamp above is only the finish moment; there is no
	// separate wire "start" timestamp — a span's start time is recoverable
	// as Timestamp minus DurationMs, if ever needed). Not omitempty: 0ms is
	// meaningful data (a fast span), not absence.
	DurationMs int64 `json:"durationMs"`
}

// Tracer publishes obs.trace.* spans on one NATS connection. Constructed
// inside Adapter.New(nc, deps) — never a package-level singleton, since
// shipping/pricing/trading-partner each hold one Adapter (and connection) per
// tenant account. It carries no service name of its own: every api.*/rpc.*
// subject already encodes {context}.{service}.{entity}.{action} by fixed
// position (CLAUDE.md's subject taxonomy), so Start derives all four from the
// inbound request rather than needing a constructor argument that could drift
// from the subject's own token (e.g. this service's $SRV identity is
// "trading-partner-service", but its subjects use the short form
// "trading-partner" — deriving from the subject avoids that mismatch).
type Tracer struct {
	nc *nats.Conn
}

// New builds a Tracer publishing on nc.
func New(nc *nats.Conn) *Tracer {
	return &Tracer{nc: nc}
}

// Span is one in-flight span, created by Tracer.Start and finished exactly
// once via End or Fail. All methods are nil-safe: SpanFrom returns nil for a
// micro.Request that was never wrapped (e.g. a handler called directly in a
// unit test), and a nil *Span's End/Fail/SetAttribute are no-ops so callers
// don't need a defensive nil check at every call site.
type Span struct {
	tracer *Tracer

	traceID      string
	spanID       string
	parentSpanID string

	context string
	service string
	entity  string
	action  string

	subject       string
	correlationID string
	reqHeaders    map[string][]string
	reqPayload    []byte

	attributes map[string]string

	// startedAt is stamped at construction (Start/StartFromHeaders/
	// StartOutbound) so finish() can compute DurationMs (Phase 28g) — never
	// exposed on the wire itself, only the derived duration is.
	startedAt time.Time
}

// Start derives (context, service, entity, action) from req's own subject —
// the fixed {family}.{context}.{service}.{entity}.{action}.v{n} arity every
// api.*/rpc.* subject already has (CLAUDE.md's subject taxonomy) — and either
// continues an inbound traceparent header or mints a new root span. It
// returns a wrapped micro.Request; pass that (not the original req) to the
// handler so SpanFrom can recover the span later.
func (t *Tracer) Start(req micro.Request) micro.Request {
	subject := req.Subject()
	ctx, service, entity, action := splitSubject(subject)

	traceID, parentSpanID := parseTraceparent(req.Headers().Get(TraceparentHeader))
	if traceID == "" {
		traceID = newTraceID()
		parentSpanID = ""
	}

	sp := &Span{
		tracer:        t,
		traceID:       traceID,
		spanID:        newSpanID(),
		parentSpanID:  parentSpanID,
		context:       ctx,
		service:       service,
		entity:        entity,
		action:        action,
		subject:       subject,
		correlationID: req.Reply(),
		reqHeaders:    map[string][]string(req.Headers()),
		reqPayload:    req.Data(),
		attributes:    map[string]string{},
		startedAt:     time.Now(),
	}
	return &spanRequest{Request: req, span: sp}
}

// Middleware wraps a micro.HandlerFunc so every call through it starts (and,
// via the handler's own respond/respondError tail calling End/Fail, finishes)
// exactly one span. This is the "single wrap at the AddEndpoint call site"
// that replaces a hand-pasted observability call in every handler.
func (t *Tracer) Middleware(handler micro.HandlerFunc) micro.HandlerFunc {
	return func(req micro.Request) {
		handler(t.Start(req))
	}
}

// StartFromHeaders mints a span continuing headers' traceparent if present,
// else a root span — for an entry point that isn't micro.Request-shaped
// (Phase 28d: a JetStream Consume callback's inbound jetstream.Msg, whose
// Headers() returns the same nats.Header type a micro.Request does, but
// isn't a micro.Request itself). Like StartOutbound, the caller supplies the
// label fields explicitly rather than this function parsing them off
// subject — evt.* subjects don't share rpc.*/api.*'s fixed
// {family}.{context}.{service}.{entity}.{action}.v{n} arity (shipping's is
// evt.{context}.{service}.{entity}.{entity-id}.{event}, six tokens with a
// distinct id segment; refdata's own evt.* change-pointer feed is five,
// dropping the id segment entirely), so a single positional parse can't fit
// both.
func (t *Tracer) StartFromHeaders(headers nats.Header, subject string, payload []byte, contextValue, service, entity, action string) *Span {
	traceID, parentSpanID := parseTraceparent(headers.Get(TraceparentHeader))
	if traceID == "" {
		traceID = newTraceID()
		parentSpanID = ""
	}
	return &Span{
		tracer:       t,
		traceID:      traceID,
		spanID:       newSpanID(),
		parentSpanID: parentSpanID,
		context:      contextValue,
		service:      service,
		entity:       entity,
		action:       action,
		subject:      subject,
		reqPayload:   payload,
		reqHeaders:   map[string][]string(headers),
		attributes:   map[string]string{},
		startedAt:    time.Now(),
	}
}

// spanContextKey is an unexported type so ContextWithSpan's value can never
// collide with a key set by another package.
type spanContextKey struct{}

// ContextWithSpan attaches sp to ctx so a lower layer with no direct access
// to the originating micro.Request — e.g. an outbound rpc.* client several
// calls down through the application/command layer — can still continue this
// trace (Phase 28c). This is deliberately the *only* thing riding ctx.Value
// in this codebase: it requires no command/query signature to change (ctx is
// already threaded everywhere), and the domain layer never has to know
// tracing exists. No-op (returns ctx unchanged) if sp is nil.
func ContextWithSpan(ctx context.Context, sp *Span) context.Context {
	if sp == nil {
		return ctx
	}
	return context.WithValue(ctx, spanContextKey{}, sp)
}

// SpanFromContext recovers a Span attached by ContextWithSpan, or nil if none
// was attached — nil-safe exactly like SpanFrom, so a caller with no parent
// span (e.g. a call this service makes with no originating request behind
// it) still gets a nil-safe *Span back.
func SpanFromContext(ctx context.Context) *Span {
	sp, _ := ctx.Value(spanContextKey{}).(*Span)
	return sp
}

// StartOutbound mints a client-side span for an outbound rpc.* call this
// service is about to make (BR-037), continuing parent's trace — or minting
// a fresh root span if parent is nil (e.g. this call has no known
// originating request, such as one triggered from a REST handler with no
// natstrace instrumentation of its own yet).
//
// Unlike Start (used for inbound requests, whose req.Subject() is always the
// real wire subject the server matched), an outbound caller's subject is not
// reliably the real 6-token {family}.{context}.{service}.{entity}.{action}
// shape: refdata-service's cross-account import remaps a tenant-local alias
// like "refdata.item.get.v1" to the real "rpc.{tenant}.refdata.item.get.v1"
// entirely inside the NATS server (accounts-service's provisioner.go
// tenantImports — a security boundary: the server inserts the account's own
// identity at that token, a caller-supplied value never can, exactly why
// refdata-service's own ItemGetRequest carries Context in the body instead
// of trusting the subject). Parsing that local alias positionally would
// silently mislabel or blank out the span's fields, so the caller supplies
// (contextValue, service, entity, action) explicitly instead of this
// function trying to infer them from subject.
//
// Call this ONCE, before any retry loop — one span per *logical* call, not
// one per attempt (BR-037). Record the attempt count via SetAttribute
// ("rpc.retry_count") and finish the returned span with End/Fail exactly
// once, after the last attempt.
func (t *Tracer) StartOutbound(parent *Span, subject string, payload []byte, contextValue, service, entity, action string) *Span {
	traceID := newTraceID()
	parentSpanID := ""
	if parent != nil {
		traceID = parent.traceID
		parentSpanID = parent.spanID
	}
	return &Span{
		tracer:       t,
		traceID:      traceID,
		spanID:       newSpanID(),
		parentSpanID: parentSpanID,
		context:      contextValue,
		service:      service,
		entity:       entity,
		action:       action,
		subject:      subject,
		reqPayload:   payload,
		attributes:   map[string]string{},
		startedAt:    time.Now(),
	}
}

// Traceparent returns the W3C traceparent header value for propagating sp
// onto an outbound message this span's handling causes (Phase 28c/28d). Safe
// to call on a nil Span — returns "".
func (sp *Span) Traceparent() string {
	if sp == nil {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-01", sp.traceID, sp.spanID)
}

// SetRequestHeaders records the headers this span's own outbound message
// carries — the missing half of "who called this" for a StartOutbound span.
// Start captures the inbound request's headers automatically, and
// StartFromHeaders retains the ones it is handed, but an OUTBOUND span's
// headers can't be captured at construction: the caller cannot build them
// until it has the span (Traceparent() is one of the values that goes in),
// so the span necessarily exists first. Without this, a client-side span
// published the Nats-Requestor identity on the wire — the callee's own span
// proves it arrived — while showing "no Nats-Requestor" for itself, the
// one hop that definitively knows who the requestor is. Call it right after
// building the outbound header map. No-op on a nil Span or nil headers.
func (sp *Span) SetRequestHeaders(headers map[string][]string) {
	if sp == nil || len(headers) == 0 {
		return
	}
	sp.reqHeaders = headers
}

// SetAttribute records a cross-cutting key/value on the span (e.g. BR-037's
// rpc.retry_count). No-op on a nil Span.
func (sp *Span) SetAttribute(key, value string) {
	if sp == nil {
		return
	}
	sp.attributes[key] = value
}

// End finishes sp successfully, publishing the reply-side span. No-op on a
// nil Span (an unwrapped request never started one).
func (sp *Span) End(payload []byte, headers map[string][]string) {
	sp.finish("OK", "", payload, headers)
}

// Fail finishes sp as an error, publishing the reply-side span with err's
// message as both the payload error and the span's statusMessage. No-op on a
// nil Span.
func (sp *Span) Fail(err error, payload []byte, headers map[string][]string) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	sp.finish("ERROR", msg, payload, headers)
}

func (sp *Span) finish(statusCode, errMsg string, payload []byte, headers map[string][]string) {
	if sp == nil {
		return
	}
	defer func() { _ = recover() }() // publishing a span must never fail the business path (BR-036 inherits BR-D26)

	respPayload, payloadBytes, redactedFields, truncated := preparePayload(payload)
	reqPayload, reqPayloadBytes, reqRedactedFields, reqTruncated := preparePayload(sp.reqPayload)
	mergedHeaders, redactedHeaders := redactHeaders(mergeHeaders(sp.reqHeaders, headers))
	redactedFields = append(redactedFields, redactedHeaders...)

	span := traceSpan{
		Direction:     "reply",
		CorrelationID: sp.correlationID,
		Subject:       sp.subject,
		Payload:       respPayload,
		Error:         errMsg,
		Headers:       mergedHeaders,
		Timestamp:     time.Now().UTC(),
		PayloadBytes:  payloadBytes,

		TraceID:       sp.traceID,
		SpanID:        sp.spanID,
		ParentSpanID:  sp.parentSpanID,
		Service:       sp.service,
		Entity:        sp.entity,
		Action:        sp.action,
		StatusCode:    statusCode,
		StatusMessage: errMsg,
		Attributes:    sp.attributes,
		Redacted:      redactedFields,
		Truncated:     truncated,
		DurationMs:    time.Since(sp.startedAt).Milliseconds(),

		RequestPayload:      reqPayload,
		RequestPayloadBytes: reqPayloadBytes,
		RequestRedacted:     reqRedactedFields,
		RequestTruncated:    reqTruncated,
	}
	data, err := json.Marshal(span)
	if err != nil {
		return
	}
	subject := fmt.Sprintf("obs.trace.%s.%s.%s.%s", sp.context, sp.service, sp.entity, sp.action)
	_ = sp.tracer.nc.Publish(subject, data)
}

// spanCarrier lets SpanFrom recover a Span from a micro.Request without an
// import cycle or a package-level registry keyed by correlation id.
type spanCarrier interface {
	natsTraceSpan() *Span
}

// spanRequest wraps a micro.Request, delegating every micro.Request method to
// the original via embedding (so Respond/Error/etc. behave identically —
// $SRV.STATS and the real wire reply are untouched) while additionally
// carrying the Span the Middleware started.
type spanRequest struct {
	micro.Request
	span *Span
}

func (s *spanRequest) natsTraceSpan() *Span { return s.span }

// SpanFrom recovers the Span a Tracer.Middleware-wrapped request is carrying.
// Returns nil for any request that was never wrapped (e.g. a handler called
// directly in a unit test) — every Span method tolerates a nil receiver, so
// callers can use the result unconditionally.
func SpanFrom(req micro.Request) *Span {
	if sc, ok := req.(spanCarrier); ok {
		return sc.natsTraceSpan()
	}
	return nil
}

var versionSuffix = regexp.MustCompile(`\.v\d+$`)

// splitSubject reads (context, service, entity, action) by position from a
// {family}.{context}.{service}.{entity}.{action}[.v{n}] subject — the fixed
// arity every api.*/rpc.* subject in this codebase has (CLAUDE.md's subject
// taxonomy: parsers read tokens by position, never by splitting a value).
func splitSubject(subject string) (context, service, entity, action string) {
	trimmed := versionSuffix.ReplaceAllString(subject, "")
	parts := strings.Split(trimmed, ".")
	if len(parts) < 5 {
		return "", "", "", ""
	}
	return parts[1], parts[2], parts[3], parts[4]
}

// parseTraceparent extracts (traceId, spanId) from a W3C traceparent header
// value ("00-<32 hex>-<16 hex>-<2 hex>"). Returns ("", "") for anything
// malformed or absent, which Start treats as "mint a new root span" — a
// missing or garbled header must never fail the request it arrived on.
func parseTraceparent(header string) (traceID, spanID string) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return "", ""
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 {
		return "", ""
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", ""
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", ""
	}
	return parts[1], parts[2]
}

// mergeHeaders combines the inbound request's own headers (captured at
// Start() — e.g. Nats-Requestor, the caller identity) with the caller-
// supplied outgoing headers (e.g. Nats-Responder) so a span published on
// obs.trace.* carries both "who called this" and "who answered", not just
// the latter. Outgoing takes precedence on key collision since it reflects
// what the handler explicitly chose to publish. Returns headers unchanged
// (possibly nil) when there's nothing to merge, so a StartOutbound/
// StartFromHeaders span — which never populates reqHeaders — behaves
// exactly as it did before this existed.
func mergeHeaders(reqHeaders, headers map[string][]string) map[string][]string {
	if len(reqHeaders) == 0 {
		return headers
	}
	merged := make(map[string][]string, len(reqHeaders)+len(headers))
	for k, v := range reqHeaders {
		merged[k] = v
	}
	for k, v := range headers {
		merged[k] = v
	}
	return merged
}

func newTraceID() string { return randomHex(16) }
func newSpanID() string  { return randomHex(8) }

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// redactHeaders strips any header whose name is denylisted (the same
// case-insensitive set redact() applies to payload keys), returning the
// surviving headers and the dotted "headers.<Name>" paths removed — the same
// path convention redact() uses for nested payload fields, so one Redacted
// list can carry both without a second wire field. Headers became worth
// redacting once spans started retaining the FULL inbound header set rather
// than just the two Nats-Requestor/Nats-Responder identity headers: an HTTP
// entry point (accounts-service's HTTPMiddleware) hands over whatever the
// browser sent, which is exactly where an Authorization/Cookie value would
// otherwise ride into a trace payload. Returns the map unchanged when
// nothing is denylisted, so the common case allocates nothing new.
func redactHeaders(headers map[string][]string) (map[string][]string, []string) {
	var removed []string
	for k := range headers {
		if _, denied := redactDenylist[strings.ToLower(k)]; denied {
			removed = append(removed, "headers."+k)
		}
	}
	if len(removed) == 0 {
		return headers, nil
	}
	kept := make(map[string][]string, len(headers)-len(removed))
	for k, v := range headers {
		if _, denied := redactDenylist[strings.ToLower(k)]; denied {
			continue
		}
		kept[k] = v
	}
	return kept, removed
}

// preparePayload runs the redact-then-truncate pipeline BR-036 applies to
// every payload published on a span — factored out of finish() so the
// request-side payload (Phase 28h's RequestPayload) gets the exact same
// treatment as the reply-side one, independently: each payload is redacted
// and size-checked on its own, so e.g. a large request body can truncate
// while a small reply body doesn't, and vice versa.
func preparePayload(payload []byte) (out json.RawMessage, byteLen int, redactedFields []string, truncated bool) {
	redactedPayload, redactedFields := redact(payload)
	byteLen = len(redactedPayload)
	if len(redactedPayload) > maxPayloadBytes {
		// A raw JSON object/array truncated mid-byte is no longer valid JSON,
		// and json.Marshal refuses to embed an invalid json.RawMessage (it
		// would fail the whole span, not just this field) — so a truncated
		// payload is re-represented as a quoted JSON *string* of the cut
		// bytes instead of left as broken inline object/array syntax. The
		// shape genuinely differs between the two cases; that's why
		// Truncated exists — a consumer must check it before assuming
		// Payload is the original object shape.
		quoted, err := json.Marshal(string(redactedPayload[:maxPayloadBytes]))
		if err != nil {
			quoted = []byte(`""`)
		}
		redactedPayload = quoted
		truncated = true
	}
	return redactedPayload, byteLen, redactedFields, truncated
}

// redact strips any denylisted key (case-insensitive, any nesting depth) from
// a JSON payload, returning the re-marshaled payload and the list of
// dotted-path field names removed. Best-effort: a payload that isn't a JSON
// object/array (or isn't valid JSON at all) is returned unchanged with no
// redacted fields — this is a debugging side-channel, not a validator, so a
// malformed payload must still publish rather than block the span.
func redact(payload []byte) (json.RawMessage, []string) {
	if len(payload) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return payload, nil
	}
	var removed []string
	scrubbed := redactValue(v, "", &removed)
	out, err := json.Marshal(scrubbed)
	if err != nil {
		return payload, nil
	}
	return out, removed
}

func redactValue(v any, path string, removed *[]string) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			if _, denied := redactDenylist[strings.ToLower(k)]; denied {
				*removed = append(*removed, childPath)
				continue
			}
			out[k] = redactValue(child, childPath, removed)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = redactValue(child, fmt.Sprintf("%s[%d]", path, i), removed)
		}
		return out
	default:
		return v
	}
}
