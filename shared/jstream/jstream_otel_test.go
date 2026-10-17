package jstream_test

// Phase 54b: the evt.* seam on the OTel path (decision 5, decision 13).
// Publish is the ctx-only method callers move to; WithOTel picks the path.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func otelProvider(t *testing.T) (*otelnats.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "shipping-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p, exp
}

func flushedSpans(t *testing.T, p *otelnats.Provider, exp *tracetest.InMemoryExporter) tracetest.SpanStubs {
	t.Helper()
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return exp.GetSpans()
}

func onlySpan(t *testing.T, spans tracetest.SpanStubs, kind trace.SpanKind) tracetest.SpanStub {
	t.Helper()
	var found []tracetest.SpanStub
	for _, s := range spans {
		if s.SpanKind == kind {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one %s span, got %d", kind, len(found))
	}
	return found[0]
}

func TestOTelPublishStartsAProducerSpanUnderTheSpanOnCtx(t *testing.T) {
	nc, js := setup(t)
	msgs := natstest.Subscribe(t, nc, subject)
	p, exp := otelProvider(t)
	pub := jstream.NewPublisher(js, jstream.WithOTel(p))

	ctx, parent := p.Tracer().Start(context.Background(), "api.acme.shipping.ship.arrive.v1")
	if err := pub.Publish(ctx, subject, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	parent.End()
	got := awaitMsg(t, msgs)

	producer := onlySpan(t, flushedSpans(t, p, exp), trace.SpanKindProducer)
	if producer.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("producer parent = %s, want the span on ctx %s", producer.Parent.SpanID(), parent.SpanContext().SpanID())
	}
	tp := otelnats.HeaderCarrier(got.Header).Get("traceparent")
	if want := producer.SpanContext.SpanID().String(); !strings.Contains(tp, want) {
		t.Fatalf("traceparent = %q, want the producer span %s", tp, want)
	}
}

func TestOTelPublishObservesWithTheProducerSpansIDs(t *testing.T) {
	nc, js := setup(t)
	obs := natstest.Observations(t, nc)
	p, exp := otelProvider(t)
	pub := jstream.NewPublisher(js, jstream.WithOTel(p), jstream.WithObservation(nc))

	ctx, parent := p.Tracer().Start(context.Background(), "api.acme.shipping.ship.arrive.v1")
	if err := pub.Publish(ctx, subject, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	parent.End()
	m := awaitMsg(t, obs)
	env := decode(t, m)

	producer := onlySpan(t, flushedSpans(t, p, exp), trace.SpanKindProducer)
	if env.TraceID != producer.SpanContext.TraceID().String() || env.SpanID != producer.SpanContext.SpanID().String() {
		t.Fatalf("envelope ids %s/%s, want the producer span's %s/%s", env.TraceID, env.SpanID,
			producer.SpanContext.TraceID(), producer.SpanContext.SpanID())
	}
	if env.ParentSpanID != parent.SpanContext().SpanID().String() {
		t.Fatalf("parentSpanId = %s, want the causing span %s", env.ParentSpanID, parent.SpanContext().SpanID())
	}
	if m.Header.Get(nats.MsgIdHdr) != env.SpanID {
		t.Fatalf("Nats-Msg-Id = %q, want spanId %q", m.Header.Get(nats.MsgIdHdr), env.SpanID)
	}
}

func TestOTelFailedPublishMarksTheProducerSpanErrorAndIsNotObserved(t *testing.T) {
	nc, js := setup(t)
	obs := natstest.Observations(t, nc)
	p, exp := otelProvider(t)
	pub := jstream.NewPublisher(js, jstream.WithOTel(p), jstream.WithObservation(nc))

	if err := pub.Publish(context.Background(), "nostream.acme.shipping.ship.s1.arrived", []byte(`{}`)); err == nil {
		t.Fatal("want an error publishing to a subject no stream captures")
	}

	producer := onlySpan(t, flushedSpans(t, p, exp), trace.SpanKindProducer)
	if producer.Status.Code != codes.Error {
		t.Fatalf("producer status = %v, want Error", producer.Status.Code)
	}
	assertSilent(t, obs)
}

func assertSilent(t *testing.T, obs chan *nats.Msg) {
	t.Helper()
	select {
	case m := <-obs:
		t.Fatalf("want no observation, got %s", m.Subject)
	case <-time.After(300 * time.Millisecond):
	}
}
