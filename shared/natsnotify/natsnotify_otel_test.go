package natsnotify_test

// Phase 54b: the notify.* seam on the OTel path (decision 5, decision 13).
// Publish keeps its signature; WithOTel picks the path.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func otelProvider(t *testing.T) (*otelnats.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "refdata-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p, exp
}

func producerSpan(t *testing.T, p *otelnats.Provider, exp *tracetest.InMemoryExporter) tracetest.SpanStub {
	t.Helper()
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var found []tracetest.SpanStub
	for _, s := range exp.GetSpans() {
		if s.SpanKind == trace.SpanKindProducer {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one PRODUCER span, got %d", len(found))
	}
	return found[0]
}

var portsChanged = natsnotify.Subject{
	Name:   "notify._platform.refdata.acme.ports.changed",
	Tokens: natsnotify.Tokens{Context: "acme", Service: "refdata", Entity: "ports", Action: "changed"},
}

func TestOTelPublishCarriesTheProducerSpansTraceparent(t *testing.T) {
	nc := natstest.Start(t, "natsnotify-otel-test")
	msgs := natstest.Subscribe(t, nc, "notify.>")
	p, exp := otelProvider(t)
	n := natsnotify.New(nc, nil, natsnotify.WithOTel(p))

	ctx, parent := p.Tracer().Start(context.Background(), "rpc.acme.refdata.item.put.v1")
	n.Publish(ctx, portsChanged, []byte(`{}`))
	parent.End()
	got := recv(t, msgs, "the notify message")

	producer := producerSpan(t, p, exp)
	if producer.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("producer parent = %s, want the span on ctx %s", producer.Parent.SpanID(), parent.SpanContext().SpanID())
	}
	tp := otelnats.HeaderCarrier(got.Header).Get("traceparent")
	if !strings.Contains(tp, producer.SpanContext.SpanID().String()) {
		t.Fatalf("traceparent = %q, want the producer span %s", tp, producer.SpanContext.SpanID())
	}
}

func TestOTelPublishObservesUnderItsTokensWithTheProducerSpansIDs(t *testing.T) {
	nc := natstest.Start(t, "natsnotify-otel-test")
	obs := natstest.Observations(t, nc)
	p, exp := otelProvider(t)
	n := natsnotify.New(nc, nil, natsnotify.WithOTel(p), natsnotify.WithObservation(nc))

	n.Publish(context.Background(), portsChanged, []byte(`{}`))
	m := recv(t, obs, "the observation")

	if m.Subject != "obs.pubsub.acme.refdata.ports.changed" {
		t.Fatalf("observation subject = %q", m.Subject)
	}
	var env struct {
		TraceID string `json:"traceId"`
		SpanID  string `json:"spanId"`
	}
	if err := json.Unmarshal(m.Data, &env); err != nil {
		t.Fatal(err)
	}
	producer := producerSpan(t, p, exp)
	if env.TraceID != producer.SpanContext.TraceID().String() || env.SpanID != producer.SpanContext.SpanID().String() {
		t.Fatalf("envelope ids %s/%s, want the producer span's", env.TraceID, env.SpanID)
	}
	if m.Header.Get(nats.MsgIdHdr) != env.SpanID {
		t.Fatalf("Nats-Msg-Id = %q, want spanId %q", m.Header.Get(nats.MsgIdHdr), env.SpanID)
	}
}

func TestOTelPublishThatNeverReachedTheWireMarksTheSpanErrorAndIsNotObserved(t *testing.T) {
	obsNC := natstest.Start(t, "natsnotify-otel-obs")
	obs := natstest.Observations(t, obsNC)
	closed := natstest.Start(t, "natsnotify-otel-closed")
	closed.Close()
	p, exp := otelProvider(t)
	n := natsnotify.New(closed, nil, natsnotify.WithOTel(p), natsnotify.WithObservation(obsNC))

	n.Publish(context.Background(), portsChanged, []byte(`{}`))

	if producer := producerSpan(t, p, exp); producer.Status.Code != codes.Error {
		t.Fatalf("producer status = %v, want Error", producer.Status.Code)
	}
	silence(t, obs, "an observation of a publish that failed")
}
