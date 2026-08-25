package refdata_test

// Phase 43e: what stayed behind when internal/jstream's specs merged into
// shared/jstream. The seam's contract is asserted once, there; this asserts
// something about THIS service's event — that a refdata change is filed under
// its typeKey, which is the entity token an operator filters the Messages
// panel by.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
)

func TestRefdataChangeIsObservedUnderItsTypeKey(t *testing.T) {
	nc, js := natstest.StartJetStream(t, "refdata-evt-obs-test")
	ctx := context.Background()
	if _, err := jstream.CreateStream(ctx, js, kvcache.ChangeStreamName, []string{kvcache.ChangeSubjectWildcard}, jstream.WithMaxAge(time.Hour)); err != nil {
		t.Fatal(err)
	}
	obs := natstest.Observations(t, nc)

	pub := jstream.NewPublisher(js, jstream.WithObservation(nc))
	sp := natstrace.New(nc).StartFromHeaders(nil, "rpc.acme.refdata.item.update.v1", nil, "acme", "refdata", "item", "update")
	if err := pub.PublishWithTrace(ctx, sp, "evt.acme.refdata.hazard-class.changed",
		[]byte(`{"typeKey":"hazard-class","context":"acme","version":7}`)); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-obs:
		if want := "obs.pubsub.acme.refdata.hazard-class.changed"; m.Subject != want {
			t.Fatalf("observation subject = %q, want %q", m.Subject, want)
		}
		if m.Header.Get("Nats-Msg-Id") == "" {
			t.Fatal("expected Nats-Msg-Id — BR-047's dedup depends on it")
		}
		traceID := strings.Split(sp.Traceparent(), "-")[1]
		if !strings.Contains(string(m.Data), `"traceId":"`+traceID+`"`) {
			t.Fatalf("observation did not continue the causing trace: %s", m.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no obs.pubsub.* observation for an evt.* publish")
	}
}
