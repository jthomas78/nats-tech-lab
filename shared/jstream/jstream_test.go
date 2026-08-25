package jstream_test

// Specs for the evt.* seam. Phase 43e merged these from two copies that had
// been maintained side by side in shipping-service and refdata-service: the
// traceparent specs (Phase 28d, BR-037) and the gate spec (Phase 43a,
// BR-045) were the same test written twice, differing only in which domain's
// subject they happened to publish on. Each service keeps only the specs that
// assert something about ITS events; everything that asserts something about
// the seam lives here.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

const subject = "evt.acme.shipping.ship.s1.arrived"

func setup(t *testing.T) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	nc, js := natstest.StartJetStream(t, "jstream-test")
	if _, err := jstream.CreateStream(context.Background(), js, "EVT", []string{"evt.>"}); err != nil {
		t.Fatal(err)
	}
	return nc, js
}

type envelope struct {
	Direction    string          `json:"direction"`
	Subject      string          `json:"subject"`
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId"`
	Entity       string          `json:"entity"`
	Action       string          `json:"action"`
	Payload      json.RawMessage `json:"payload"`
}

func awaitMsg(t *testing.T, ch chan *nats.Msg) *nats.Msg {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
		return nil
	}
}

func decode(t *testing.T, m *nats.Msg) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(m.Data, &env); err != nil {
		t.Fatalf("decode obs.pubsub envelope: %v", err)
	}
	return env
}

// --- BR-037: the traceparent -------------------------------------------

func TestPublishWithTraceAttachesTraceparentWhenSpanPresent(t *testing.T) {
	nc, js := setup(t)
	received := natstest.Subscribe(t, nc, subject)

	sp := natstrace.New(nc).StartOutbound(nil, subject, nil, "acme", "shipping", "ship", "arrived")
	if err := jstream.NewPublisher(js).PublishWithTrace(context.Background(), sp, subject, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	if got := awaitMsg(t, received).Header.Get(natstrace.TraceparentHeader); got == "" {
		t.Fatal("expected a traceparent header")
	}
}

func TestPublishWithTraceOmitsTraceparentWhenSpanNil(t *testing.T) {
	nc, js := setup(t)
	received := natstest.Subscribe(t, nc, subject)

	if err := jstream.NewPublisher(js).PublishWithTrace(context.Background(), nil, subject, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	if got := awaitMsg(t, received).Header.Get(natstrace.TraceparentHeader); got != "" {
		t.Fatalf("expected no traceparent header when sp is nil, got %q", got)
	}
}

// --- BR-045: the observation gate --------------------------------------

func TestPublisherWithoutObservationStaysSilent(t *testing.T) {
	nc, js := setup(t)
	obs := natstest.Observations(t, nc)

	// No WithObservation — a Publisher built for a test or a one-off tool
	// must not emit onto a channel nobody wired.
	if err := jstream.NewPublisher(js).PublishWithTrace(context.Background(), nil, subject, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-obs:
		t.Fatalf("an unobserved Publisher emitted %q", m.Subject)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPublishWithTraceObservesUnderTheDerivedTokens(t *testing.T) {
	nc, js := setup(t)
	obs := natstest.Observations(t, nc)
	pub := jstream.NewPublisher(js, jstream.WithObservation(nc))

	if err := pub.PublishWithTrace(context.Background(), nil, subject, []byte(`{"shipID":"s1"}`)); err != nil {
		t.Fatal(err)
	}

	m := awaitMsg(t, obs)
	if m.Subject != "obs.pubsub.acme.shipping.ship.arrived" {
		t.Fatalf("observation subject = %q", m.Subject)
	}
	env := decode(t, m)
	if env.Direction != "publish" {
		t.Fatalf("direction = %q, want publish", env.Direction)
	}
	if env.Subject != subject {
		t.Fatalf("envelope subject = %q, want the real evt.* subject with its id", env.Subject)
	}
	if m.Header.Get(nats.MsgIdHdr) == "" || m.Header.Get(nats.MsgIdHdr) != env.SpanID {
		t.Fatalf("Nats-Msg-Id = %q, want the envelope spanId %q (BR-047 dedup)", m.Header.Get(nats.MsgIdHdr), env.SpanID)
	}
}

func TestPublishWithTraceContinuesTheCausingTrace(t *testing.T) {
	nc, js := setup(t)
	obs := natstest.Observations(t, nc)
	pub := jstream.NewPublisher(js, jstream.WithObservation(nc))

	parent := natstrace.New(nc).StartOutbound(nil, "api.acme.shipping.ship.arrive.v1", []byte(`{}`), "acme", "shipping", "ship", "arrive")
	if err := pub.PublishWithTrace(context.Background(), parent, subject, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	env := decode(t, awaitMsg(t, obs))
	tp := strings.Split(parent.Traceparent(), "-")
	if env.TraceID != tp[1] {
		t.Fatalf("traceId = %q, want the causing span's %q", env.TraceID, tp[1])
	}
	if env.ParentSpanID != tp[2] {
		t.Fatalf("parentSpanId = %q, want %q", env.ParentSpanID, tp[2])
	}
	if env.SpanID == tp[2] {
		t.Fatal("the observation must be its own span, not a re-emission of the causing one")
	}
}

func TestPublishWithTraceRedactsBeforeObserving(t *testing.T) {
	nc, js := setup(t)
	obs := natstest.Observations(t, nc)
	pub := jstream.NewPublisher(js, jstream.WithObservation(nc))

	if err := pub.PublishWithTrace(context.Background(), nil, subject, []byte(`{"password":"s3cr3t"}`)); err != nil {
		t.Fatal(err)
	}

	env := decode(t, awaitMsg(t, obs))
	if strings.Contains(string(env.Payload), "s3cr3t") {
		t.Fatalf("denylisted field survived into the observation: %s", env.Payload)
	}
}

// TestFailedPublishIsNotObserved: an event that never reached the stream did
// not happen. Publishing outside every stream's subject filter is JetStream's
// own "no responders" — the domain publish errors, so nothing may be
// observed.
func TestFailedPublishIsNotObserved(t *testing.T) {
	nc, js := setup(t)
	obs := natstest.Observations(t, nc)
	pub := jstream.NewPublisher(js, jstream.WithObservation(nc))

	err := pub.PublishWithTrace(context.Background(), nil, "nostream.acme.shipping.ship.s1.arrived", []byte(`{}`))
	if err == nil {
		t.Fatal("expected a publish error for a subject outside every stream")
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-obs:
		t.Fatalf("a failed publish was still observed: %s", m.Subject)
	case <-time.After(300 * time.Millisecond):
	}
}

// --- CreateStream -------------------------------------------------------

// TestCreateStreamIsUnboundedByDefault pins the shipping case: SHIPPING is
// the source of truth, and its history IS the aggregate, so retention must
// not quietly acquire a bound.
func TestCreateStreamIsUnboundedByDefault(t *testing.T) {
	_, js := natstest.StartJetStream(t, "jstream-test")
	s, err := jstream.CreateStream(context.Background(), js, "UNBOUNDED", []string{"evt.unbounded.>"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.CachedInfo().Config.MaxAge; got != 0 {
		t.Fatalf("MaxAge = %v, want unbounded", got)
	}
	if got := s.CachedInfo().Config.Retention; got != jetstream.LimitsPolicy {
		t.Fatalf("Retention = %v, want LimitsPolicy — replay depends on it", got)
	}
}

// TestCreateStreamWithMaxAgeBoundsRetention pins the refdata case: REFDATA is
// a notification feed, not a source of truth.
func TestCreateStreamWithMaxAgeBoundsRetention(t *testing.T) {
	_, js := natstest.StartJetStream(t, "jstream-test")
	s, err := jstream.CreateStream(context.Background(), js, "BOUNDED", []string{"evt.bounded.>"}, jstream.WithMaxAge(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.CachedInfo().Config.MaxAge; got != time.Hour {
		t.Fatalf("MaxAge = %v, want 1h", got)
	}
}
