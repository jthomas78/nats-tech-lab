package refdata

// Phase 54b-3a: the BR-D42 notify path on OTel. The bridge's consume of an
// evt.* change pointer is a CONSUMER span that continues the pointer's trace,
// and each tenant's notify.* publish is a PRODUCER span under it.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/notifybridge"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

const (
	notifySentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	notifySentSpanID  = "00f067aa0ba902b7"
)

func newNotifyProvider(t *testing.T) (*otelnats.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "refdata-service", Exporter: exp})
	if err != nil {
		t.Fatalf("init otel: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p, exp
}

func exportedOfKind(t *testing.T, p *otelnats.Provider, exp *tracetest.InMemoryExporter, kind trace.SpanKind) []tracetest.SpanStub {
	t.Helper()
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	var found []tracetest.SpanStub
	for _, s := range exp.GetSpans() {
		if s.SpanKind == kind {
			found = append(found, s)
		}
	}
	return found
}

func TestTenantNotifyPublishIsAProducerSpanUnderTheCtxSpan(t *testing.T) {
	srv := runEmbeddedNATS(t)
	tenant, err := nats.Connect(srv.ClientURL(), nats.Name("acme"))
	if err != nil {
		t.Fatalf("connect tenant: %v", err)
	}
	defer tenant.Close()
	notified := make(chan *nats.Msg, 4)
	if _, err := tenant.ChanSubscribe("notify.>", notified); err != nil {
		t.Fatalf("subscribe notify.>: %v", err)
	}
	if err := tenant.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	p, exp := newNotifyProvider(t)
	parentCtx, parent := p.Tracer().Start(context.Background(), "evt.acme.refdata.port.changed")
	tenantPublisher{tracing: p}.publishTo(parentCtx, "acme", tenant, notifybridge.Changed("acme", "port"), []byte(`{"typeKey":"port"}`))
	parent.End()

	var msg *nats.Msg
	select {
	case msg = <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("the notify.* publish never arrived")
	}

	producers := exportedOfKind(t, p, exp, trace.SpanKindProducer)
	if len(producers) != 1 {
		t.Fatalf("PRODUCER spans = %d, want 1", len(producers))
	}
	pr := producers[0]
	if pr.Name != "notify.acme.refdata.port.changed" {
		t.Fatalf("PRODUCER span name = %q", pr.Name)
	}
	if pr.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("PRODUCER span is not a child of the ctx span")
	}
	want := "00-" + pr.SpanContext.TraceID().String() + "-" + pr.SpanContext.SpanID().String() + "-01"
	if got := msg.Header.Get("traceparent"); got != want {
		t.Fatalf("notify.* traceparent = %q, want %q", got, want)
	}
}

// recordingPublisher is a notifybridge.Publisher that keeps the ctx of each
// fan-out.
type recordingPublisher struct {
	mu   sync.Mutex
	ctxs []context.Context
}

func (r *recordingPublisher) PublishToAll(ctx context.Context, _ natsnotify.Subject, _ []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ctxs = append(r.ctxs, ctx)
}

func (r *recordingPublisher) first() (context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ctxs) == 0 {
		return nil, false
	}
	return r.ctxs[0], true
}

func TestNotifyBridgeConsumesUnderAConsumerSpanThatContinuesTheEvtTrace(t *testing.T) {
	_, js := natstest.StartJetStream(t, "refdata-notifybridge-otel-test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := jstream.CreateStream(ctx, js, kvcache.ChangeStreamName, []string{kvcache.ChangeSubjectWildcard}, jstream.WithMaxAge(time.Hour)); err != nil {
		t.Fatal(err)
	}

	p, exp := newNotifyProvider(t)
	pub := &recordingPublisher{}
	notifybridge.Run(ctx, js, pub, nil, notifybridge.WithOTel(p))

	// The bridge uses DeliverNewPolicy, and its consumer starts in the
	// background, so publish until the first message lands.
	var fanOutCtx context.Context
	deadline := time.Now().Add(5 * time.Second)
	for fanOutCtx == nil && time.Now().Before(deadline) {
		m := nats.NewMsg("evt.acme.refdata.port.changed")
		m.Data = []byte(`{"typeKey":"port","context":"acme","version":1}`)
		m.Header.Set("traceparent", "00-"+notifySentTraceID+"-"+notifySentSpanID+"-01")
		if _, err := js.PublishMsg(ctx, m); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		fanOutCtx, _ = pub.first()
	}
	if fanOutCtx == nil {
		t.Fatal("the bridge never fanned out")
	}
	// The CONSUMER span ends after the fan-out returns, so wait for it.
	var consumers []tracetest.SpanStub
	for end := time.Now().Add(2 * time.Second); len(consumers) == 0 && time.Now().Before(end); {
		consumers = exportedOfKind(t, p, exp, trace.SpanKindConsumer)
		if len(consumers) == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if len(consumers) == 0 {
		t.Fatal("no CONSUMER span for the bridged evt.* message")
	}
	c := consumers[0]
	if c.Name != "evt.acme.refdata.port.changed" {
		t.Fatalf("CONSUMER span name = %q", c.Name)
	}
	if c.SpanContext.TraceID().String() != notifySentTraceID {
		t.Fatalf("CONSUMER span trace = %s, want the evt.* trace %s", c.SpanContext.TraceID(), notifySentTraceID)
	}
	if c.Parent.SpanID().String() != notifySentSpanID {
		t.Fatalf("CONSUMER span parent = %s, want %s", c.Parent.SpanID(), notifySentSpanID)
	}
	var account string
	for _, kv := range c.Attributes {
		if kv.Key == otelnats.AttrReceivingAccount {
			account = kv.Value.AsString()
		}
	}
	if account != "PLATFORM" {
		t.Fatalf("CONSUMER receiving account = %q, want PLATFORM", account)
	}

	// The fan-out runs under the CONSUMER span, so each notify.* PRODUCER
	// span is its child.
	if got := trace.SpanContextFromContext(fanOutCtx).SpanID(); got != c.SpanContext.SpanID() {
		t.Fatalf("fan-out ctx span = %s, want the CONSUMER span %s", got, c.SpanContext.SpanID())
	}
}
