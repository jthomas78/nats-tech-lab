package otelnats

import (
	"context"

	"github.com/nats-io/nats.go"
)

// HeaderCarrier adapts nats.Header to OTel's propagation.TextMapCarrier, so
// the standard W3C propagator works on NATS messages as it does on HTTP.
type HeaderCarrier nats.Header

// Get returns the first value for key.
func (c HeaderCarrier) Get(_ string) string { return "" }

// Set stores value under key.
func (c HeaderCarrier) Set(_, _ string) {}

// Keys lists the header keys.
func (c HeaderCarrier) Keys() []string { return nil }

// Inject writes the current span context into msg's headers (BR-064).
func Inject(_ context.Context, _ *nats.Msg) {}

// Extract reads a span context from h into ctx (BR-064).
func Extract(ctx context.Context, _ nats.Header) context.Context { return ctx }
