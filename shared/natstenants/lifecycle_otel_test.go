package natstenants

// Phase 54b: lifecycle deliveries on the OTel path (decision 5). Each
// delivery is one CONSUMER span, and it is the span on the handler's ctx.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

const (
	sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	sentSpanID  = "00f067aa0ba902b7"
)

// lifecycleOTel subscribes with WithOTel, publishes one created event and
// returns the one CONSUMER span and the span context the handler saw.
func lifecycleOTel(t *testing.T, traceparent string, body string, ensureErr error) (tracetest.SpanStub, trace.SpanContext) {
	t.Helper()
	srv := newTestServer(t)
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("test"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "organizations-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	seen := make(chan trace.SpanContext, 1)
	h := LifecycleHandlers{
		Ensure: func(ctx context.Context, _ string) error {
			seen <- trace.SpanContextFromContext(ctx)
			return ensureErr
		},
		Teardown: func(context.Context, string) error { return nil },
	}
	if err := SubscribeLifecycle(context.Background(), nc, nil, h, WithOTel(p, "acme")); err != nil {
		t.Fatalf("SubscribeLifecycle: %v", err)
	}

	msg := nats.NewMsg("notify.accounts.account.created")
	msg.Data = []byte(body)
	if traceparent != "" {
		msg.Header.Set("traceparent", traceparent)
	}
	if err := nc.PublishMsg(msg); err != nil {
		t.Fatal(err)
	}

	var handlerSpan trace.SpanContext
	if body != "" && body != "{" {
		select {
		case handlerSpan = <-seen:
		case <-time.After(2 * time.Second):
			t.Fatal("handler was not called")
		}
	}

	var consumers []tracetest.SpanStub
	deadline := time.Now().Add(2 * time.Second)
	for len(consumers) == 0 && time.Now().Before(deadline) {
		if err := p.ForceFlush(context.Background()); err != nil {
			t.Fatal(err)
		}
		consumers = consumers[:0]
		for _, s := range exp.GetSpans() {
			if s.SpanKind == trace.SpanKindConsumer {
				consumers = append(consumers, s)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(consumers) != 1 {
		t.Fatalf("want exactly one CONSUMER span, got %d", len(consumers))
	}
	return consumers[0], handlerSpan
}

func TestLifecycleOTelDeliveryIsAConsumerSpanThatContinuesTheEventsTrace(t *testing.T) {
	consumer, handler := lifecycleOTel(t, "00-"+sentTraceID+"-"+sentSpanID+"-01", `{"name":"acme"}`, nil)

	if consumer.Name != "notify.accounts.account.created" {
		t.Errorf("span name = %q, want the subject", consumer.Name)
	}
	if consumer.SpanContext.TraceID().String() != sentTraceID || consumer.Parent.SpanID().String() != sentSpanID {
		t.Errorf("consumer %s/%s, want trace %s under parent %s",
			consumer.SpanContext.TraceID(), consumer.Parent.SpanID(), sentTraceID, sentSpanID)
	}
	if handler.SpanID() != consumer.SpanContext.SpanID() {
		t.Errorf("handler ctx span = %s, want the consumer span %s", handler.SpanID(), consumer.SpanContext.SpanID())
	}
	var account string
	for _, kv := range consumer.Attributes {
		if kv.Key == otelnats.AttrReceivingAccount {
			account = kv.Value.AsString()
		}
	}
	if account != "acme" {
		t.Errorf("receiving account = %q, want acme (BR-069)", account)
	}
}

func TestLifecycleOTelHandlerErrorMarksTheSpanError(t *testing.T) {
	consumer, _ := lifecycleOTel(t, "", `{"name":"acme"}`, errors.New("ensure failed"))

	if consumer.Status.Code != codes.Error {
		t.Errorf("status = %v, want Error", consumer.Status.Code)
	}
}

func TestLifecycleOTelUndecodableEventMarksTheSpanError(t *testing.T) {
	consumer, _ := lifecycleOTel(t, "", "{", nil)

	if consumer.Status.Code != codes.Error {
		t.Errorf("status = %v, want Error", consumer.Status.Code)
	}
}
