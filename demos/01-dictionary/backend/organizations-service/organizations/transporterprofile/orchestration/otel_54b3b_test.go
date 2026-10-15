package orchestration

// Phase 54b-3b: the TRANSPORTER evt.* seam and its projector on OTel.
//
// BR-TP75 / BR-037: an append is a PRODUCER span, the child of the span on
// ctx. Its traceparent goes on the stored message, and its obs.pubsub.*
// envelope carries the PRODUCER span's IDs (BR-045). A rejected append marks
// the span Error and is not observed.
//
// BR-037 / BR-069: the projector consumes each event under a CONSUMER span
// that continues the event's traceparent, with the tenant as the receiving
// account. The projection writes run under that span (decision 6).

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func newProfileProvider(t *testing.T) (*otelnats.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "organizations-service", Exporter: exp})
	if err != nil {
		t.Fatalf("init otel: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p, exp
}

func profileSpansOfKind(t *testing.T, p *otelnats.Provider, exp *tracetest.InMemoryExporter, kind trace.SpanKind) []tracetest.SpanStub {
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

func TestAppendWithOTelIsAProducerSpanUnderTheCtxSpan(t *testing.T) {
	nc, js, cleanup := newObservabilityTestNATS(t)
	defer cleanup()
	p, exp := newProfileProvider(t)
	store := NewJetStreamEventStore(js, WithObservation(nc), WithOTel(p))

	const orgID = "01J0TRANSPORTER0000000101"
	parentCtx, parent := p.Tracer().Start(context.Background(), "api.acme.organizations.organization.register.v1")
	event := profiledomain.NewCreatedEvent("acme", orgID)
	if _, err := store.Append(parentCtx, "acme", orgID, event, 0); err != nil {
		t.Fatal(err)
	}
	parent.End()

	producers := profileSpansOfKind(t, p, exp, trace.SpanKindProducer)
	if len(producers) != 1 {
		t.Fatalf("want 1 PRODUCER span, got %d", len(producers))
	}
	pr := producers[0]
	subject := profiledomain.Subject("acme", orgID, event.Type)
	if pr.Name != subject {
		t.Fatalf("PRODUCER span name = %q, want %q", pr.Name, subject)
	}
	if pr.SpanContext.TraceID() != parent.SpanContext().TraceID() || pr.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatal("the PRODUCER span is not the child of the ctx span")
	}

	stored, err := js.Stream(context.Background(), profiledomain.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := stored.GetLastMsgForSubject(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	want := "00-" + pr.SpanContext.TraceID().String() + "-" + pr.SpanContext.SpanID().String() + "-01"
	if got := raw.Header.Get("traceparent"); got != want {
		t.Fatalf("stored traceparent = %q, want the PRODUCER span's %q", got, want)
	}
}

func TestAppendWithOTelObservesUnderTheProducerSpan(t *testing.T) {
	nc, js, cleanup := newObservabilityTestNATS(t)
	defer cleanup()
	obs := subscribeObservations(t, nc)
	p, exp := newProfileProvider(t)
	store := NewJetStreamEventStore(js, WithObservation(nc), WithOTel(p))

	const orgID = "01J0TRANSPORTER0000000102"
	if _, err := store.Append(context.Background(), "acme", orgID, profiledomain.NewCreatedEvent("acme", orgID), 0); err != nil {
		t.Fatal(err)
	}

	var env struct {
		TraceID string `json:"traceId"`
		SpanID  string `json:"spanId"`
	}
	select {
	case m := <-obs:
		if err := json.Unmarshal(m.Data, &env); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the append emitted no obs.pubsub.* observation")
	}
	producers := profileSpansOfKind(t, p, exp, trace.SpanKindProducer)
	if len(producers) != 1 {
		t.Fatalf("want 1 PRODUCER span, got %d", len(producers))
	}
	if env.TraceID != producers[0].SpanContext.TraceID().String() || env.SpanID != producers[0].SpanContext.SpanID().String() {
		t.Fatalf("envelope IDs %s/%s are not the PRODUCER span's", env.TraceID, env.SpanID)
	}
}

func TestAppendWithOTelMarksARejectedAppendAnErrorAndObservesNothing(t *testing.T) {
	nc, js, cleanup := newObservabilityTestNATS(t)
	defer cleanup()
	p, exp := newProfileProvider(t)
	store := NewJetStreamEventStore(js, WithObservation(nc), WithOTel(p))

	const orgID = "01J0TRANSPORTER0000000103"
	event := profiledomain.NewCreatedEvent("acme", orgID)
	if _, err := store.Append(context.Background(), "acme", orgID, event, 0); err != nil {
		t.Fatal(err)
	}
	obs := subscribeObservations(t, nc)
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), "acme", orgID, event, 0); err != ErrSequenceConflict {
		t.Fatalf("expected ErrSequenceConflict, got %v", err)
	}

	producers := profileSpansOfKind(t, p, exp, trace.SpanKindProducer)
	if len(producers) != 2 {
		t.Fatalf("want 2 PRODUCER spans, got %d", len(producers))
	}
	if producers[1].Status.Code != codes.Error {
		t.Fatalf("the rejected append's span status = %v, want Error", producers[1].Status.Code)
	}
	select {
	case m := <-obs:
		t.Fatalf("a rejected append was observed anyway: %s", m.Subject)
	case <-time.After(300 * time.Millisecond):
	}
}

// ctxRecordingProjection records the span on the ctx each Upsert receives.
type ctxRecordingProjection struct {
	mu    sync.Mutex
	spans []trace.SpanContext
}

func (r *ctxRecordingProjection) Upsert(ctx context.Context, _ profiledomain.State) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, trace.SpanContextFromContext(ctx))
	return nil
}

func (r *ctxRecordingProjection) Put(context.Context, profiledomain.State) error { return nil }

func (r *ctxRecordingProjection) recorded() []trace.SpanContext {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]trace.SpanContext(nil), r.spans...)
}

func TestProjectorWithOTelConsumesUnderAConsumerSpanThatContinuesTheEvtTrace(t *testing.T) {
	_, js, cleanup := newObservabilityTestNATS(t)
	defer cleanup()
	p, exp := newProfileProvider(t)

	projection := &ctxRecordingProjection{}
	projector := NewProjector(js, projection, projection).WithOTel(p, "acme")
	if err := projector.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer projector.Stop()

	const orgID = "01J0TRANSPORTER0000000104"
	store := NewJetStreamEventStore(js, WithOTel(p))
	if _, err := store.Append(context.Background(), "acme", orgID, profiledomain.NewCreatedEvent("acme", orgID), 0); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for len(projection.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(projection.recorded()) == 0 {
		t.Fatal("the projector never wrote the projection")
	}

	var consumers []tracetest.SpanStub
	for time.Now().Before(deadline) {
		consumers = profileSpansOfKind(t, p, exp, trace.SpanKindConsumer)
		if len(consumers) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(consumers) != 1 {
		t.Fatalf("want 1 CONSUMER span, got %d", len(consumers))
	}
	producers := profileSpansOfKind(t, p, exp, trace.SpanKindProducer)
	if len(producers) != 1 {
		t.Fatalf("want 1 PRODUCER span, got %d", len(producers))
	}
	c, pr := consumers[0], producers[0]
	if c.SpanContext.TraceID() != pr.SpanContext.TraceID() || c.Parent.SpanID() != pr.SpanContext.SpanID() {
		t.Fatal("the CONSUMER span does not continue the evt.* message's trace")
	}
	var account string
	for _, kv := range c.Attributes {
		if kv.Key == otelnats.AttrReceivingAccount {
			account = kv.Value.AsString()
		}
	}
	if account != "acme" {
		t.Fatalf("receiving account = %q, want acme", account)
	}
	if got := projection.recorded()[0]; got.SpanID() != c.SpanContext.SpanID() {
		t.Fatal("the projection write did not run under the CONSUMER span")
	}
}
