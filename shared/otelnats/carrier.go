package otelnats

import (
	"context"
	"strings"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/propagation"
)

// propagator is the one wire format: W3C Trace Context. It is held here, not
// read from the OTel globals, so nothing else in the process can change it.
var propagator = propagation.TraceContext{}

// HeaderCarrier adapts nats.Header to OTel's propagation.TextMapCarrier, so
// the standard W3C propagator works on NATS messages as it does on HTTP.
//
// nats.Header keys are case-sensitive, but shared/natstrace writes
// "Traceparent" and the W3C propagator asks for "traceparent". So Get matches
// a key in any case, and Set replaces every case variant of its key.
type HeaderCarrier nats.Header

// Get returns the first value for key, matched without regard to case.
func (c HeaderCarrier) Get(key string) string {
	if vs, ok := c[key]; ok && len(vs) > 0 {
		return vs[0]
	}
	for k, vs := range c {
		if strings.EqualFold(k, key) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

// Set stores value under key, and removes the same key in any other case.
func (c HeaderCarrier) Set(key, value string) {
	for k := range c {
		if k != key && strings.EqualFold(k, key) {
			delete(c, k)
		}
	}
	c[key] = []string{value}
}

// Keys lists the header keys.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// Inject writes the current span context into msg's headers (BR-064).
func Inject(ctx context.Context, msg *nats.Msg) {
	if msg.Header == nil {
		msg.Header = nats.Header{}
	}
	propagator.Inject(ctx, HeaderCarrier(msg.Header))
}

// Extract reads a span context from h into ctx (BR-064). A nil or empty
// header, or a malformed traceparent, leaves ctx as it was.
func Extract(ctx context.Context, h nats.Header) context.Context {
	if len(h) == 0 {
		return ctx
	}
	return propagator.Extract(ctx, HeaderCarrier(h))
}
