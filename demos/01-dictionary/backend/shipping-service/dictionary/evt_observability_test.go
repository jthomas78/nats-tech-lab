package dictionary_test

// Phase 43e: what stayed behind when internal/jstream's specs merged into
// shared/jstream. The seam's own contract — traceparent, the observation
// gate, trace continuation, redaction, no-observation-on-failure — is
// asserted once, in shared/jstream/jstream_test.go. These two assert
// something about THIS service's events: that a ship and a container publish
// are filed under the obs.pubsub.* subject an operator will search for.

import (
	"context"
	"testing"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
)

func observedSubject(t *testing.T, evtSubject string, payload []byte) string {
	t.Helper()
	nc, js := natstest.StartJetStream(t, "shipping-evt-obs-test")
	if _, err := jstream.CreateStream(context.Background(), js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatal(err)
	}
	obs := natstest.Observations(t, nc)

	pub := jstream.NewPublisher(js, jstream.WithObservation(nc))
	if err := pub.PublishWithTrace(context.Background(), nil, evtSubject, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-obs:
		return m.Subject
	case <-time.After(2 * time.Second):
		t.Fatalf("no obs.pubsub.* observation for %s", evtSubject)
		return ""
	}
}

func TestShipEventIsObservedUnderItsEntityAndAction(t *testing.T) {
	got := observedSubject(t, "evt.acme.shipping.ship.s1.arrived", []byte(`{"shipID":"s1"}`))
	if want := "obs.pubsub.acme.shipping.ship.arrived"; got != want {
		t.Fatalf("observation subject = %q, want %q", got, want)
	}
}

func TestContainerEventIsObservedUnderItsEntityAndAction(t *testing.T) {
	got := observedSubject(t, "evt.acme.shipping.container.c-1.loaded", []byte(`{}`))
	if want := "obs.pubsub.acme.shipping.container.loaded"; got != want {
		t.Fatalf("observation subject = %q, want %q", got, want)
	}
}
