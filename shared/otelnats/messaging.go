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
func (p *Provider) StartProducer(ctx context.Context, subject string) (context.Context, trace.Span) {
	return p.tracer.Start(ctx, subject,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attrMessagingSystem.String("nats"),
			attrMessagingDestination.String(subject),
		),
	)
}

// StartConsumer starts the CONSUMER span for an inbound evt.* or notify.*
// message. A publish between trusted services is a trusted edge, so a
// traceparent in msg is the parent (decision 5). The tenant attributes come
// from trust only, as in StartServer (BR-069).
//
// When ctx already holds a span it is the parent, as StartServer's EdgeRPC.
func (p *Provider) StartConsumer(ctx context.Context, msg *nats.Msg, trust Trust) (context.Context, trace.Span) {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		ctx = Extract(ctx, msg.Header)
	}
	return p.tracer.Start(ctx, msg.Subject,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(tenantAttrs(msg.Subject, trust)...),
	)
}
