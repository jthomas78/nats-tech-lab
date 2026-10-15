package otelnats

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/trace"
)

// StartProducer starts the PRODUCER span for an evt.* or notify.* publish,
// as the child of the span in ctx, or as a new root when ctx holds none
// (decision 5). The span is named for the subject. The caller injects the
// returned context into the message, publishes, and ends the span.
//
// STATUS: skeleton (54b-1). Specs are red until 54b-2.
func (p *Provider) StartProducer(ctx context.Context, subject string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(context.Background())
}

// StartConsumer starts the CONSUMER span for an inbound evt.* or notify.*
// message. A publish between trusted services is a trusted edge, so a
// traceparent in msg is the parent (decision 5). The tenant attributes come
// from trust only, as in StartServer (BR-069).
//
// STATUS: skeleton (54b-1). Specs are red until 54b-2.
func (p *Provider) StartConsumer(ctx context.Context, msg *nats.Msg, trust Trust) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(context.Background())
}
