package jstream_test

// Specs for jstream.Publisher's Phase 28d additions (PublishMsg/
// PublishWithTrace, BR-D39) — this package had no tests before.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/jstream"
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
	sub, err := nc.Subscribe("evt.acme.refdata.currency.changed", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	pub := jstream.NewPublisher(js)
	if err := pub.PublishMsg(ctx, "evt.acme.refdata.currency.changed", nats.Header{"X-Test": []string{"1"}}, []byte(`{}`)); err != nil {
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
	sub, err := nc.Subscribe("evt.acme.refdata.currency.changed", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	pub := jstream.NewPublisher(js)
	tracer := natstrace.New(nc)
	sp := tracer.StartOutbound(nil, "evt.acme.refdata.currency.changed", nil, "acme", "refdata", "currency", "changed")

	if err := pub.PublishWithTrace(ctx, sp, "evt.acme.refdata.currency.changed", []byte(`{}`)); err != nil {
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
	sub, err := nc.Subscribe("evt.acme.refdata.currency.changed", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	pub := jstream.NewPublisher(js)
	if err := pub.PublishWithTrace(ctx, nil, "evt.acme.refdata.currency.changed", []byte(`{}`)); err != nil {
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

// ── Phase 43a (BR-D45): the evt.* observation seam ───────────────────────
//
// Same seam, same contract, as shipping-service's — the point of putting the
// hook in PublishWithTrace rather than at kvcache's call site is that this
// service's evt.* coverage is then structural.

func TestPublishWithTraceObservesRefdataChange(t *testing.T) {
	nc, js, cleanup := newTestJetStream(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := jstream.CreateChangeStream(ctx, js, "REFDATA", []string{"evt.*.refdata.>"}, time.Hour); err != nil {
		t.Fatal(err)
	}

	obs := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("obs.pubsub.>", func(m *nats.Msg) { obs <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	pub := jstream.NewPublisher(js)
	pub.EnableObservation(nc)

	tracer := natstrace.New(nc)
	sp := tracer.StartFromHeaders(nil, "rpc.acme.refdata.item.update.v1", nil, "acme", "refdata", "item", "update")

	if err := pub.PublishWithTrace(ctx, sp, "evt.acme.refdata.hazard-class.changed", []byte(`{"typeKey":"hazard-class","context":"acme","version":7}`)); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-obs:
		if got, want := m.Subject, "obs.pubsub.acme.refdata.hazard-class.changed"; got != want {
			t.Fatalf("observation subject = %q, want %q", got, want)
		}
		if m.Header.Get("Nats-Msg-Id") == "" {
			t.Fatal("expected Nats-Msg-Id — BR-047's dedup depends on it")
		}
		// traceparent is 00-{traceId}-{spanId}-{flags}; the span's trace id is
		// the only handle a test has on it from outside the package.
		if !strings.Contains(string(m.Data), `"traceId":"`+strings.Split(sp.Traceparent(), "-")[1]+`"`) {
			t.Fatalf("observation did not continue the causing trace: %s", m.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no obs.pubsub.* observation for an evt.* publish")
	}
}

func TestPublisherWithoutObservationStaysSilent(t *testing.T) {
	nc, js, cleanup := newTestJetStream(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := jstream.CreateChangeStream(ctx, js, "REFDATA", []string{"evt.*.refdata.>"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	obs := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("obs.>", func(m *nats.Msg) { obs <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	// No EnableObservation call — a Publisher built for a test or a one-off
	// tool must not emit onto a channel nobody wired.
	if err := jstream.NewPublisher(js).PublishWithTrace(ctx, nil, "evt.acme.refdata.hazard-class.changed", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-obs:
		t.Fatalf("an un-enabled Publisher emitted %s", m.Subject)
	case <-time.After(500 * time.Millisecond):
	}
}
