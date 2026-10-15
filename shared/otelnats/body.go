package otelnats

import (
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/trace"
)

// Span event names for captured bodies (decision 7, BR-036 as amended by
// Phase 54).
const (
	EventRequestBody = "rpc.request.body"
	EventReplyBody   = "rpc.reply.body"
)

// Attribute keys on a body event. body.bytes is the size before
// truncation, so a truncated body's true size is still known.
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
//
// STATUS: skeleton (54b-1). Specs are red until 54b-2.
func (p *Provider) RecordRequestBody(span trace.Span, body []byte, h nats.Header) {}

// RecordReplyBody adds the rpc.reply.body event, as RecordRequestBody.
//
// STATUS: skeleton (54b-1). Specs are red until 54b-2.
func (p *Provider) RecordReplyBody(span trace.Span, body []byte, h nats.Header) {}
