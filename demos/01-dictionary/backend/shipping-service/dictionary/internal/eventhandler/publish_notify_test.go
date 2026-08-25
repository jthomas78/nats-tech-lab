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
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
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

// ── Phase 43a widening (BR-045): every notify.* call site emits an
// obs.pubsub.* observation of its own publish ─────────────────────────────
//
// The evt.* half is structural (the jstream seam); this half is per call
// site, so each site gets its own spec — that per-site coverage is the whole
// reason BR-049's source scan exists as well.

func TestPublishNotifyObservesItsOwnPublish(t *testing.T) {
	nc, cleanup := newPublishNotifyTestConn(t)
	defer cleanup()

	obs := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("obs.pubsub.>", func(m *nats.Msg) { obs <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	tracer := natstrace.New(nc)
	sp := tracer.StartFromHeaders(nil, "evt.acme.shipping.ship.s1.arrived", nil, "acme", "shipping", "ship", "arrived")

	publishNotify(nc, nil, "acme", "ship", []byte(`{"shipID":"s1"}`), sp)

	select {
	case m := <-obs:
		if got, want := m.Subject, "obs.pubsub.acme.shipping.ship.changed"; got != want {
			t.Fatalf("observation subject = %q, want %q", got, want)
		}
		var env map[string]any
		if err := json.Unmarshal(m.Data, &env); err != nil {
			t.Fatal(err)
		}
		if got, want := env["subject"], "notify.acme.shipping.ship.changed"; got != want {
			t.Fatalf("envelope subject = %v, want %q", got, want)
		}
		if env["direction"] != "publish" {
			t.Fatalf("direction = %v, want publish", env["direction"])
		}
		// Continues the projector's span rather than rooting an orphan.
		if env["parentSpanId"] == "" || env["parentSpanId"] == nil {
			t.Fatal("expected the causing span to be the observation's parent")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publishNotify emitted no obs.pubsub.* observation")
	}
}

func TestPublishRawNotifyObservesWithTheVerbAsAction(t *testing.T) {
	nc, cleanup := newPublishNotifyTestConn(t)
	defer cleanup()

	obs := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("obs.pubsub.>", func(m *nats.Msg) { obs <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	publishRawNotify(nc, nil, "acme", "ship", "arrived", []byte(`{"shipID":"s1"}`), nil)

	select {
	case m := <-obs:
		// notify.acme.shipping.raw.ship.arrived — "raw" sits where the
		// positional deriver would read the entity, so this site names its
		// tokens explicitly: the entity is the ship, the action the verb.
		if got, want := m.Subject, "obs.pubsub.acme.shipping.ship.arrived"; got != want {
			t.Fatalf("observation subject = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publishRawNotify emitted no obs.pubsub.* observation")
	}
}

func TestNotifyObservationIsSkippedWhenThePublishFails(t *testing.T) {
	nc, cleanup := newPublishNotifyTestConn(t)
	cleanup() // a closed conn fails every publish

	obs := make(chan *nats.Msg, 1)
	// Nothing can be received on a closed conn; the assertion that matters is
	// that publishNotify neither panics nor blocks when the domain publish
	// failed — an event that never reached the wire must not be observed.
	publishNotify(nc, nil, "acme", "ship", []byte(`{}`), nil)
	publishRawNotify(nc, nil, "acme", "ship", "arrived", []byte(`{}`), nil)
	select {
	case <-obs:
		t.Fatal("unreachable")
	default:
	}
}
