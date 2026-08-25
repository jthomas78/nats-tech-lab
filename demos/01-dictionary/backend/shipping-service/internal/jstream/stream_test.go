package jstream_test

// Specs for jstream.Publisher's Phase 28d additions (PublishMsg/
// PublishWithTrace, BR-037) — this package had no tests before.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

func newTestJetStream(t *testing.T) (*nats.Conn, jetstream.JetStream, func()) {
	t.Helper()
	opts := &server.Options{JetStream: true, StoreDir: t.TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("jstream-test"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js, func() { nc.Close(); srv.Shutdown() }
}

func TestPublishMsgCarriesHeaders(t *testing.T) {
	nc, js, cleanup := newTestJetStream(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: "EVT", Subjects: []string{"evt.>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.MemoryStorage,
	}); err != nil {
		t.Fatal(err)
	}

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("evt.acme.shipping.ship.s1.arrived", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	pub := jstream.NewPublisher(js)
	if err := pub.PublishMsg(ctx, "evt.acme.shipping.ship.s1.arrived", nats.Header{"X-Test": []string{"1"}}, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-received:
		if m.Header.Get("X-Test") != "1" {
			t.Fatalf("expected header X-Test=1, got %q", m.Header.Get("X-Test"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

func TestPublishWithTraceAttachesTraceparentWhenSpanPresent(t *testing.T) {
	nc, js, cleanup := newTestJetStream(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: "EVT2", Subjects: []string{"evt.>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.MemoryStorage,
	}); err != nil {
		t.Fatal(err)
	}

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("evt.acme.shipping.ship.s1.arrived", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	pub := jstream.NewPublisher(js)
	tracer := natstrace.New(nc)
	sp := tracer.StartOutbound(nil, "evt.acme.shipping.ship.s1.arrived", nil, "acme", "shipping", "ship", "arrived")

	if err := pub.PublishWithTrace(ctx, sp, "evt.acme.shipping.ship.s1.arrived", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-received:
		if m.Header.Get(natstrace.TraceparentHeader) == "" {
			t.Fatal("expected a traceparent header")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

func TestPublishWithTraceOmitsTraceparentWhenSpanNil(t *testing.T) {
	nc, js, cleanup := newTestJetStream(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: "EVT3", Subjects: []string{"evt.>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.MemoryStorage,
	}); err != nil {
		t.Fatal(err)
	}

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("evt.acme.shipping.ship.s1.arrived", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	pub := jstream.NewPublisher(js)
	if err := pub.PublishWithTrace(ctx, nil, "evt.acme.shipping.ship.s1.arrived", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-received:
		if m.Header.Get(natstrace.TraceparentHeader) != "" {
			t.Fatal("expected no traceparent header when sp is nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

// --- Phase 43a (BR-045): the evt.* seam publishes obs.pubsub.* ------------
//
// PublishWithTrace is the seam every evt.* publish in this service already
// goes through (commands.go, container.go, kvcache.go), which is why the
// observation hangs here rather than at each call site — ADR-047's amendment
// A3. Observation is opt-in per Publisher via EnableObservation so a test or
// a tool constructing a bare Publisher stays silent.

func observeSetup(t *testing.T) (*nats.Conn, jetstream.JetStream, *jstream.Publisher, chan *nats.Msg, func()) {
	t.Helper()
	nc, js, cleanup := newTestJetStream(t)
	if _, err := js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name: "EVT", Subjects: []string{"evt.>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.MemoryStorage,
	}); err != nil {
		cleanup()
		t.Fatal(err)
	}
	observed := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("obs.pubsub.>", func(m *nats.Msg) { observed <- m })
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	pub := jstream.NewPublisher(js)
	pub.EnableObservation(nc)
	return nc, js, pub, observed, func() { sub.Unsubscribe(); cleanup() } //nolint:errcheck
}

type observedSpan struct {
	Direction    string `json:"direction"`
	Subject      string `json:"subject"`
	TraceID      string `json:"traceId"`
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId"`
	Entity       string `json:"entity"`
	Action       string `json:"action"`
	Redacted     []string
	Payload      json.RawMessage `json:"payload"`
}

func awaitObserved(t *testing.T, observed chan *nats.Msg) (*nats.Msg, observedSpan) {
	t.Helper()
	select {
	case m := <-observed:
		var env observedSpan
		if err := json.Unmarshal(m.Data, &env); err != nil {
			t.Fatalf("decode obs.pubsub envelope: %v", err)
		}
		return m, env
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an obs.pubsub.* observation")
		return nil, observedSpan{}
	}
}

func TestPublishWithTraceObservesShipEvent(t *testing.T) {
	_, _, pub, observed, cleanup := observeSetup(t)
	defer cleanup()

	if err := pub.PublishWithTrace(context.Background(), nil, "evt.acme.shipping.ship.s1.arrived", []byte(`{"shipID":"s1"}`)); err != nil {
		t.Fatal(err)
	}

	m, env := awaitObserved(t, observed)
	if m.Subject != "obs.pubsub.acme.shipping.ship.arrived" {
		t.Fatalf("observation subject = %q", m.Subject)
	}
	if env.Direction != "publish" {
		t.Fatalf("direction = %q, want publish", env.Direction)
	}
	if env.Subject != "evt.acme.shipping.ship.s1.arrived" {
		t.Fatalf("envelope subject = %q, want the real evt.* subject with its ship id", env.Subject)
	}
	if m.Header.Get(nats.MsgIdHdr) == "" || m.Header.Get(nats.MsgIdHdr) != env.SpanID {
		t.Fatalf("Nats-Msg-Id = %q, want the envelope spanId %q (BR-047 dedup)", m.Header.Get(nats.MsgIdHdr), env.SpanID)
	}
}

func TestPublishWithTraceObservesContainerEvent(t *testing.T) {
	_, _, pub, observed, cleanup := observeSetup(t)
	defer cleanup()

	if err := pub.PublishWithTrace(context.Background(), nil, "evt.acme.shipping.container.c-1.loaded", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	m, _ := awaitObserved(t, observed)
	if m.Subject != "obs.pubsub.acme.shipping.container.loaded" {
		t.Fatalf("observation subject = %q", m.Subject)
	}
}

func TestPublishWithTraceContinuesTheCausingTrace(t *testing.T) {
	nc, _, pub, observed, cleanup := observeSetup(t)
	defer cleanup()

	parent := natstrace.New(nc).StartOutbound(nil, "api.acme.shipping.ship.arrive.v1", []byte(`{}`), "acme", "shipping", "ship", "arrive")
	if err := pub.PublishWithTrace(context.Background(), parent, "evt.acme.shipping.ship.s1.arrived", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	_, env := awaitObserved(t, observed)
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
	_, _, pub, observed, cleanup := observeSetup(t)
	defer cleanup()

	if err := pub.PublishWithTrace(context.Background(), nil, "evt.acme.shipping.ship.s1.arrived", []byte(`{"password":"s3cr3t"}`)); err != nil {
		t.Fatal(err)
	}

	_, env := awaitObserved(t, observed)
	if strings.Contains(string(env.Payload), "s3cr3t") {
		t.Fatalf("denylisted field survived into the observation: %s", env.Payload)
	}
}

func TestPublisherWithoutObservationStaysSilent(t *testing.T) {
	nc, js, cleanup := newTestJetStream(t)
	defer cleanup()
	if _, err := js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name: "EVT", Subjects: []string{"evt.>"}, Retention: jetstream.LimitsPolicy, Storage: jetstream.MemoryStorage,
	}); err != nil {
		t.Fatal(err)
	}
	observed := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("obs.pubsub.>", func(m *nats.Msg) { observed <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	if err := jstream.NewPublisher(js).PublishWithTrace(context.Background(), nil, "evt.acme.shipping.ship.s1.arrived", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-observed:
		t.Fatalf("unobserved Publisher emitted %q", m.Subject)
	case <-time.After(200 * time.Millisecond):
	}
}
