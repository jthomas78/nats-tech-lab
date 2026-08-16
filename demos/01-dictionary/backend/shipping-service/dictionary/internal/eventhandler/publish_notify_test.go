// White-box tests (package eventhandler, not eventhandler_test) for
// publishNotify/publishRawNotify's Phase 28d addition: both now accept the
// per-message *natstrace.Span the calling Consume callback started
// (handler.go's register(), RegisterContainers, RegisterMeta) and, when one
// is present, attach a Traceparent header to the notify.* publish exactly
// the way jstream.Publisher.PublishWithTrace does for the evt.* publish
// (BR-037) — nil-safe both ways: a nil sp publishes with no header at all,
// identical to pre-28d behavior.
package eventhandler

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/natstrace"
)

func newPublishNotifyTestConn(t *testing.T) (*nats.Conn, func()) {
	t.Helper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("publish-notify-test"))
	if err != nil {
		t.Fatal(err)
	}
	return nc, func() { nc.Close(); srv.Shutdown() }
}

func TestPublishNotifyAttachesTraceparentWhenSpanPresent(t *testing.T) {
	nc, cleanup := newPublishNotifyTestConn(t)
	defer cleanup()

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("notify.acme.shipping.ship.changed", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	tracer := natstrace.New(nc)
	sp := tracer.StartFromHeaders(nil, "evt.acme.shipping.ship.s1.arrived", nil, "acme", "shipping", "ship", "arrived")

	publishNotify(nc, nil, "acme", "ship", []byte(`{}`), sp)

	select {
	case m := <-received:
		if m.Header.Get(natstrace.TraceparentHeader) == "" {
			t.Fatal("expected a traceparent header when sp is present")
		}
		if got, want := m.Header.Get(natstrace.TraceparentHeader), sp.Traceparent(); got != want {
			t.Fatalf("traceparent = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notify publish")
	}
}

func TestPublishNotifyOmitsTraceparentWhenSpanNil(t *testing.T) {
	nc, cleanup := newPublishNotifyTestConn(t)
	defer cleanup()

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("notify.acme.shipping.ship.changed", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	publishNotify(nc, nil, "acme", "ship", []byte(`{}`), nil)

	select {
	case m := <-received:
		if m.Header.Get(natstrace.TraceparentHeader) != "" {
			t.Fatal("expected no traceparent header when sp is nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notify publish")
	}
}

func TestPublishRawNotifyAttachesTraceparentWhenSpanPresent(t *testing.T) {
	nc, cleanup := newPublishNotifyTestConn(t)
	defer cleanup()

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("notify.acme.shipping.raw.ship.arrived", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	tracer := natstrace.New(nc)
	sp := tracer.StartFromHeaders(nil, "evt.acme.shipping.ship.s1.arrived", nil, "acme", "shipping", "ship", "arrived")

	publishRawNotify(nc, nil, "acme", "ship", "arrived", []byte(`{}`), sp)

	select {
	case m := <-received:
		if m.Header.Get(natstrace.TraceparentHeader) == "" {
			t.Fatal("expected a traceparent header when sp is present")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for raw notify publish")
	}
}

func TestPublishRawNotifyOmitsTraceparentWhenSpanNil(t *testing.T) {
	nc, cleanup := newPublishNotifyTestConn(t)
	defer cleanup()

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe("notify.acme.shipping.raw.ship.arrived", func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	publishRawNotify(nc, nil, "acme", "ship", "arrived", []byte(`{}`), nil)

	select {
	case m := <-received:
		if m.Header.Get(natstrace.TraceparentHeader) != "" {
			t.Fatal("expected no traceparent header when sp is nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for raw notify publish")
	}
}

func TestPublishNotifyNoopWhenNCNil(t *testing.T) {
	var nc *nats.Conn
	tracer := natstrace.New(nil)
	sp := tracer.StartFromHeaders(nil, "evt.acme.shipping.ship.s1.arrived", nil, "acme", "shipping", "ship", "arrived")

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("publishNotify must not panic when nc is nil: %v", r)
			}
		}()
		publishNotify(nc, nil, "acme", "ship", []byte(`{}`), sp)
		publishRawNotify(nc, nil, "acme", "ship", "arrived", []byte(`{}`), sp)
	}()
}
