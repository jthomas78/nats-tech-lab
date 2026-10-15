package otelnats

import (
	"encoding/json"
	"strings"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Span event names for captured bodies (decision 7, BR-036 as amended by
// Phase 54).
const (
	EventRequestBody = "rpc.request.body"
	EventReplyBody   = "rpc.reply.body"
)

// Attribute keys on a body event. body.bytes is the size after redaction
// and before truncation, as natstrace's payloadBytes, so a truncated body's
// true size is still known.
const (
	AttrBody          = "body"
	AttrBodyBytes     = "body.bytes"
	AttrBodyRedacted  = "redacted"
	AttrBodyTruncated = "truncated"
	AttrBodyHeaders   = "headers"
)

// MaxBodyBytes is BR-036's 4 KiB cap, applied after redaction, never before.
const MaxBodyBytes = 4096

// RecordRequestBody adds the rpc.request.body event to span, when the
// provider was built with CaptureBodies. The body is redacted, then
// truncated, before it is set; h is redacted the same way.
func (p *Provider) RecordRequestBody(span trace.Span, body []byte, h nats.Header) {
	p.recordBody(span, EventRequestBody, body, h)
}

// RecordReplyBody adds the rpc.reply.body event, as RecordRequestBody.
func (p *Provider) RecordReplyBody(span trace.Span, body []byte, h nats.Header) {
	p.recordBody(span, EventReplyBody, body, h)
}

func (p *Provider) recordBody(span trace.Span, name string, body []byte, h nats.Header) {
	if p == nil || !p.cfg.CaptureBodies || span == nil || !span.IsRecording() {
		return
	}

	// Redact first, so a secret past the cap is still found (BR-036).
	out, redacted := redact(body)
	attrs := []attribute.KeyValue{attribute.Int(AttrBodyBytes, len(out))}
	if len(out) > MaxBodyBytes {
		out = out[:MaxBodyBytes]
		attrs = append(attrs, attribute.Bool(AttrBodyTruncated, true))
	}
	// A cut can split a rune; an attribute string must be valid UTF-8.
	attrs = append(attrs, attribute.String(AttrBody, strings.ToValidUTF8(string(out), "")))

	if len(h) > 0 {
		kept, removed := redactHeaders(h)
		redacted = append(redacted, removed...)
		if hdrs, err := json.Marshal(kept); err == nil {
			attrs = append(attrs, attribute.String(AttrBodyHeaders, string(hdrs)))
		}
	}
	if len(redacted) > 0 {
		attrs = append(attrs, attribute.StringSlice(AttrBodyRedacted, redacted))
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}
