package otelnats

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// The span wrappers (Phase 57, BR-078). Service code says what work is
// observed and returns what happened; the wrapper starts the span, moves the
// trace context, records the outcome and ends the span. Each wrapper is
// nil-safe: on a nil Provider it only runs the callback (BR-079).
//
// The callback gets the span's ctx, derived from the caller's ctx, so it
// keeps the caller's cancellation, deadline and values (BR-082). A result
// other than the error travels by closure.

// Label is an extra span attribute a caller adds, such as entity_id. A label
// whose key the wrapper owns is dropped (BR-081).
type Label struct {
	Key   string
	Value string
}

// reservedKeys are the attributes a wrapper sets from trusted input. Every
// lab.tenant.* key is reserved as well (BR-069).
var reservedKeys = map[string]bool{
	string(attrMessagingSystem):      true,
	string(attrMessagingDestination): true,
	string(attrPeerService):          true,
	string(attrRetryCount):           true,
	string(AttrRequester):            true,
}

// attrRetryCount is the CLIENT span's attempt count (BR-037).
const attrRetryCount = attribute.Key("rpc.retry_count")

func reserved(key string) bool {
	return reservedKeys[key] || strings.HasPrefix(key, "lab.tenant.")
}

// labelAttrs turns labels into attributes and drops each one with a reserved
// key, so the trusted value always wins, even when it is empty (BR-081).
func labelAttrs(labels []Label) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(labels))
	for _, l := range labels {
		if reserved(l.Key) {
			slog.Debug("otelnats: caller label dropped, the key is reserved", "key", l.Key)
			continue
		}
		attrs = append(attrs, attribute.String(l.Key, l.Value))
	}
	return attrs
}

// fail marks span Error for err (BR-080).
func fail(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// end ends span. Deferred by every wrapper: on a panic it marks the span
// Error and ends it, then panics again with the same value (BR-079, BR-080).
func end(span trace.Span) {
	if r := recover(); r != nil {
		span.SetStatus(codes.Error, fmt.Sprint(r))
		span.End()
		panic(r)
	}
	span.End()
}

// Call runs one logical rpc.* call, the whole retry loop, under one CLIENT
// span (BR-037, BR-065). It writes the span's traceparent into msg before
// call runs, so every attempt carries it. call does the requests and
// returns the final reply, the number of retries and the error; the service
// owns the retry policy. The span records rpc.retry_count, and is Error when
// call fails or the reply carries micro's Nats-Service-Error header (BR-055).
func (p *Provider) Call(
	ctx context.Context,
	msg *nats.Msg,
	peerService string,
	call func(ctx context.Context) (reply *nats.Msg, retries int, err error),
	labels ...Label,
) (*nats.Msg, error) {
	if p == nil {
		reply, _, err := call(ctx)
		return reply, err
	}
	ctx, span := p.StartClient(ctx, msg.Subject, peerService)
	defer end(span)
	span.SetAttributes(labelAttrs(labels)...)
	Inject(ctx, msg)

	reply, retries, err := call(ctx)
	span.SetAttributes(attrRetryCount.Int(retries))
	switch {
	case err != nil:
		fail(span, err)
	case reply != nil && reply.Header.Get(micro.ErrorHeader) != "":
		span.SetStatus(codes.Error, reply.Header.Get(micro.ErrorHeader))
	}
	return reply, err
}

// Consume runs handle for one inbound evt.*, notify.* or lifecycle message
// under a CONSUMER span named for subject (decision 5). The span's parent is
// the traceparent in header, and only that: a span already on ctx is removed
// first, and with no header the span is a new root (decision 6). The rest of
// ctx is kept (BR-082). The tenant attributes come from trust only (BR-069).
// The span is Error when handle returns an error (BR-080).
func (p *Provider) Consume(
	ctx context.Context,
	subject string,
	header nats.Header,
	trust Trust,
	handle func(ctx context.Context) error,
	labels ...Label,
) error {
	if p == nil {
		return handle(ctx)
	}
	ctx = trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	ctx = Extract(ctx, header)
	ctx, span := p.tracer.Start(ctx, subject,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(tenantAttrs(subject, trust)...),
		trace.WithAttributes(labelAttrs(labels)...),
	)
	defer end(span)

	err := handle(ctx)
	if err != nil {
		fail(span, err)
	}
	return err
}

// Produce runs publish for one outgoing evt.* or notify.* message under a
// PRODUCER span, the child of the span in ctx or a new root (decision 5).
// It writes the span's traceparent into msg before publish runs. publish
// sends msg and maps its own errors; the span is Error when it returns one
// (BR-080).
func (p *Provider) Produce(
	ctx context.Context,
	msg *nats.Msg,
	publish func(ctx context.Context) error,
	labels ...Label,
) error {
	if p == nil {
		return publish(ctx)
	}
	ctx, span := p.StartProducer(ctx, msg.Subject)
	defer end(span)
	span.SetAttributes(labelAttrs(labels)...)
	Inject(ctx, msg)

	err := publish(ctx)
	if err != nil {
		fail(span, err)
	}
	return err
}
