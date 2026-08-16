// Package natstrace is accounts-service's Phase 28e copy of the hand-rolled
// distributed tracing prototyped in trading-partner-service (Phase 28a) —
// no go.opentelemetry.io/* dependency, W3C-compatible on the wire,
// OTLP-shaped in its fields. It is deliberately duplicated per service (not
// a shared module), the same way obsEnvelope/errorResponse/obsSubjectFor/
// contextFromSubject already are in this codebase — see
// ARCHITECTURE-COMMUNICATIONS.md § 6 for why.
//
// This copy diverges from the other four in one deliberate way:
// accounts-service has no NATS micro.Service at all (its primary transport
// is REST, not rpc.*/api.*), so there is no micro.Request-shaped Start/
// Middleware/SpanFrom here. Instead, HTTPMiddleware wraps net/http routes —
// the http.Handler-decorator symmetric counterpart the Phase 28 plan calls
// for, covering every accounts/auth REST endpoint from one wiring point in
// cmd/main.go. The shared span/redaction/truncation mechanism (BR-036) is
// otherwise byte-identical to the other four services' copies.
package natstrace

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// TraceparentHeader is the header name carrying W3C trace context on the
// wire: "00-<32 hex trace id>-<16 hex parent span id>-<2 hex flags>". Used
// both as a NATS header key (nats.Header.Get is case-sensitive, hence the
// capitalized form, matching Nats-Requestor/Nats-Responder/X-Actor) and as
// an HTTP header name — net/http.Header canonicalizes any casing of
// "traceparent" to exactly this string via textproto.CanonicalMIMEHeaderKey,
// so the same constant is correct on both sides with no special-casing.
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
	// StartFromHeaders()/HTTPMiddleware construction time, redacted and
	// truncated the same way Payload (the reply body) already is. Separate
	// Redacted/Truncated tracking from the reply side since the two payloads
	// are independently sized/shaped — e.g. a large request with a tiny reply
	// truncates only RequestPayload.
	RequestPayload      json.RawMessage `json:"requestPayload,omitempty"`
	RequestPayloadBytes int             `json:"requestPayloadBytes,omitempty"`
	RequestRedacted     []string        `json:"requestRedacted,omitempty"`
	RequestTruncated    bool            `json:"requestTruncated,omitempty"`

	// DurationMs is the span's own measured duration (Phase 28g) — from
	// StartFromHeaders/StartOutbound's construction moment to End/Fail's
	// finish() call, never derived by a consumer subtracting two timestamps
	// (Timestamp above is only the finish moment; there is no separate wire
	// "start" timestamp — a span's start time is recoverable as Timestamp
	// minus DurationMs, if ever needed). Not omitempty: 0ms is meaningful
	// data (a fast span), not absence.
	DurationMs int64 `json:"durationMs"`
}

// Tracer publishes obs.trace.* spans on one NATS connection. Constructed
// once in cmd/main.go (never a package-level singleton, matching the other
// four services' convention even though accounts-service itself has only
// one process-wide connection, not one per tenant).
type Tracer struct {
	nc *nats.Conn
}

// New builds a Tracer publishing on nc.
func New(nc *nats.Conn) *Tracer {
	return &Tracer{nc: nc}
}

// Span is one in-flight span, created by StartFromHeaders/StartOutbound/
// HTTPMiddleware and finished exactly once via End or Fail. All methods are
// nil-safe so callers don't need a defensive nil check at every call site.
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

	// startedAt is stamped at construction (StartFromHeaders/StartOutbound)
	// so finish() can compute DurationMs (Phase 28g) — never exposed on the
	// wire itself, only the derived duration is.
	startedAt time.Time
}

// StartFromHeaders mints a span continuing headers' traceparent if present,
// else a root span. The caller supplies the label fields explicitly rather
// than this function parsing them off subject — accounts-service's REST
// routes have no rpc.*/api.* subject to parse from at all.
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

// statusRecorder captures the HTTP status code a wrapped handler writes, so
// HTTPMiddleware can tell End from Fail after ServeHTTP returns — net/http's
// ResponseWriter has no built-in way to read back what a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// httpEntity derives a coarse span "entity" label from a request path —
// "/api/accounts/{name}/suspend" and "/api/auth/connectInfo" become
// "accounts" and "auth" respectively. The exact path (which carries the real
// detail: {name}, the specific sub-route) is recorded as the "http.path"
// attribute instead of forced into this coarse field.
func httpEntity(path string) string {
	trimmed := strings.TrimPrefix(path, "/api/")
	parts := strings.SplitN(trimmed, "/", 2)
	if parts[0] == "" {
		return "root"
	}
	return parts[0]
}

// HTTPMiddleware wraps an http.Handler so every request starts (and
// finishes) exactly one span — the http.Handler-decorator symmetric
// counterpart of the other four services' micro.Handler Middleware,
// covering every accounts-service REST endpoint from one wiring point
// (cmd/main.go wraps the top-level *http.ServeMux once, rather than
// decorating each mux.Handle call individually, since both auth.Handlers and
// accounts.Handlers register onto the same mux). Continues an inbound
// Traceparent header if present (net/http.Header canonicalizes any casing to
// exactly the form TraceparentHeader already uses), else mints a root span.
// A response status >= 400 finishes the span as Fail, matching how a
// natstrace-instrumented NATS reply distinguishes success from error.
func (t *Tracer) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entity := httpEntity(r.URL.Path)
		action := strings.ToLower(r.Method)

		// Body must be re-buffered before next.ServeHTTP consumes the
		// stream — http.Request.Body is read-once, and the handler
		// downstream still needs the real bytes to decode its own request,
		// so this captures a copy for the span and restores an equivalent
		// io.ReadCloser for the handler rather than diverting the original.
		var reqBody []byte
		if r.Body != nil {
			reqBody, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(reqBody))
		}

		sp := t.StartFromHeaders(nats.Header(r.Header), r.URL.Path, reqBody, "_platform", "accounts", entity, action)
		sp.SetAttribute("http.method", r.Method)
		sp.SetAttribute("http.path", r.URL.Path)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ContextWithSpan(r.Context(), sp)))

		sp.SetAttribute("http.status_code", strconv.Itoa(rec.status))
		if rec.status >= 400 {
			sp.Fail(fmt.Errorf("http %d", rec.status), nil, nil)
			return
		}
		sp.End(nil, nil)
	})
}

// spanContextKey is an unexported type so ContextWithSpan's value can never
// collide with a key set by another package.
type spanContextKey struct{}

// ContextWithSpan attaches sp to ctx so a lower layer with no direct access
// to the originating *http.Request — e.g. publishAccountEvent, several
// calls down from the HTTP handler — can still continue this trace. This is
// deliberately the *only* thing riding ctx.Value in this codebase: it
// requires no signature change beyond threading ctx itself (already
// idiomatic Go), and the domain/provisioning layer never has to know tracing
// exists. No-op (returns ctx unchanged) if sp is nil.
func ContextWithSpan(ctx context.Context, sp *Span) context.Context {
	if sp == nil {
		return ctx
	}
	return context.WithValue(ctx, spanContextKey{}, sp)
}

// SpanFromContext recovers a Span attached by ContextWithSpan, or nil if none
// was attached — nil-safe, so a caller with no parent span still gets a
// nil-safe *Span back.
func SpanFromContext(ctx context.Context) *Span {
	sp, _ := ctx.Value(spanContextKey{}).(*Span)
	return sp
}

// StartOutbound mints a client-side span for an outbound NATS publish this
// service is about to make (BR-037), continuing parent's trace — or minting
// a fresh root span if parent is nil. The caller supplies the label fields
// explicitly (see StartFromHeaders's doc comment on why subject can't be
// parsed for this).
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
// onto an outbound message this span's handling causes. Safe to call on a
// nil Span — returns "".
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

// SetAttribute records a cross-cutting key/value on the span. No-op on a nil
// Span.
func (sp *Span) SetAttribute(key, value string) {
	if sp == nil {
		return
	}
	sp.attributes[key] = value
}

// End finishes sp successfully, publishing the reply-side span. No-op on a
// nil Span.
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

// parseTraceparent extracts (traceId, spanId) from a W3C traceparent header
// value ("00-<32 hex>-<16 hex>-<2 hex>"). Returns ("", "") for anything
// malformed or absent, which callers treat as "mint a new root span" — a
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
