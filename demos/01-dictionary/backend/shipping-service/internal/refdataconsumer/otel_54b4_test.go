package refdataconsumer

// Phase 54b-4: with WithTracing(p) the Consumer's one span per logical call
// is an OTel CLIENT span (BR-064, BR-065), the same as organizations-service's
// refdataclient. Without it the call runs untraced (Phase 54d).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func newConsumerProvider(t *testing.T) (*otelnats.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "shipping-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p, exp
}

func clientSpans(t *testing.T, p *otelnats.Provider, exp *tracetest.InMemoryExporter) []tracetest.SpanStub {
	t.Helper()
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out []tracetest.SpanStub
	for _, s := range exp.GetSpans() {
		if s.SpanKind == trace.SpanKindClient {
			out = append(out, s)
		}
	}
	return out
}

func retryCount(s tracetest.SpanStub) (int64, bool) {
	for _, kv := range s.Attributes {
		if kv.Key == attribute.Key("rpc.retry_count") {
			return kv.Value.AsInt64(), true
		}
	}
	return 0, false
}

func TestLookupWithTracingIsOneClientSpanUnderTheCtxSpan(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	p, exp := newConsumerProvider(t)

	legacy := make(chan *nats.Msg, 4)
	legacySub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer legacySub.Unsubscribe() //nolint:errcheck

	var gotTraceparent string
	sub, err := nc.Subscribe("rpc.acme-test.refdata.item.get.v1", func(msg *nats.Msg) {
		gotTraceparent = msg.Header.Get("traceparent")
		data, _ := json.Marshal(rpcItemGetResponse{})
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	parentCtx, parent := p.Tracer().Start(context.Background(), "parent")
	c := New(nc, WithTracing(p))
	if _, err := c.Lookup(parentCtx, "acme-test", "hazard-class", "3", "en"); err != nil {
		t.Fatal(err)
	}
	parent.End()

	spans := clientSpans(t, p, exp)
	if len(spans) != 1 {
		t.Fatalf("expected one CLIENT span, got %d", len(spans))
	}
	s := spans[0]
	if s.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("expected the CLIENT span's parent to be the ctx span")
	}
	if gotTraceparent == "" || gotTraceparent[36:52] != s.SpanContext.SpanID().String() {
		t.Fatalf("expected the request's traceparent to name the CLIENT span, got %q", gotTraceparent)
	}
	if n, ok := retryCount(s); !ok || n != 0 {
		t.Fatalf("expected rpc.retry_count 0, got %d (set %v)", n, ok)
	}

	select {
	case m := <-legacy:
		t.Fatalf("expected no obs.trace.* message (retired in Phase 54d), got %s", m.Subject)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestLookupWithTracingMarksAnUnansweredCallAnError(t *testing.T) {
	nc, cleanupNC := newTestNATS(t)
	defer cleanupNC()
	p, exp := newConsumerProvider(t)

	c := New(nc, WithTracing(p), WithRPCTimeout(50*time.Millisecond), WithRPCRetries(1), WithRPCBackoff(time.Millisecond))
	if _, err := c.Lookup(context.Background(), "acme-test", "hazard-class", "3", "en"); err == nil {
		t.Fatal("expected an error with no responder")
	}

	spans := clientSpans(t, p, exp)
	if len(spans) != 1 {
		t.Fatalf("expected one CLIENT span across both attempts, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Fatalf("expected status Error, got %v", spans[0].Status.Code)
	}
	if n, _ := retryCount(spans[0]); n != 1 {
		t.Fatalf("expected rpc.retry_count 1, got %d", n)
	}
}
