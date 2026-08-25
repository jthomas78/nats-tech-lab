package orchestration

// Phase 43a (BUSINESS_RULES-ORGANIZATIONS.md's BR-TP75, pointing at
// BUSINESS_RULES-SHIPPING.md's BR-045). This service had no rule at all in
// ADR-047 as originally approved — an entire service's evt.* traffic would
// have been invisible to the Messages panel; the 2026-08-25 pre-implementation
// review added one (A2).
//
// White-box (package orchestration) so the unexported append is exercised
// directly: it is the seam, and Append/AppendWorkflowEvent are two thin
// callers of it.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

func newObservabilityTestNATS(t *testing.T) (*nats.Conn, jetstream.JetStream, func()) {
	t.Helper()
	srv, err := server.NewServer(&server.Options{JetStream: true, StoreDir: t.TempDir(), Port: -1})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("transporter-pubsub-test"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStream(context.Background(), js); err != nil {
		t.Fatal(err)
	}
	return nc, js, func() { nc.Close(); srv.Shutdown() }
}

func subscribeObservations(t *testing.T, nc *nats.Conn) chan *nats.Msg {
	t.Helper()
	obs := make(chan *nats.Msg, 8)
	sub, err := nc.Subscribe("obs.>", func(m *nats.Msg) { obs <- m })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return obs
}

func TestAppendIsTheEvtSeamAndIsObserved(t *testing.T) {
	nc, js, cleanup := newObservabilityTestNATS(t)
	defer cleanup()
	obs := subscribeObservations(t, nc)

	store := NewJetStreamEventStore(js, WithObservation(nc))

	event := profiledomain.NewCreatedEvent("acme", "01J0TRANSPORTER0000000001")
	if _, err := store.Append(context.Background(), "acme", "01J0TRANSPORTER0000000001", event, 0); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-obs:
		want := "obs.pubsub.acme.organizations." + profiledomain.AggregateName + "." + event.Type
		if m.Subject != want {
			t.Fatalf("observation subject = %q, want %q", m.Subject, want)
		}
		if m.Header.Get(nats.MsgIdHdr) == "" {
			t.Fatal("expected Nats-Msg-Id — BR-047's dedup depends on it")
		}
		var env struct {
			Subject   string `json:"subject"`
			Direction string `json:"direction"`
		}
		if err := json.Unmarshal(m.Data, &env); err != nil {
			t.Fatal(err)
		}
		if env.Subject != profiledomain.Subject("acme", "01J0TRANSPORTER0000000001", event.Type) {
			t.Fatalf("envelope subject = %q", env.Subject)
		}
		if env.Direction != "publish" {
			t.Fatalf("direction = %q, want publish", env.Direction)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("append emitted no obs.pubsub.* observation — this service's evt.* traffic is invisible")
	}
}

func TestAppendObservationNeverFailsOrDelaysTheDomainAppend(t *testing.T) {
	nc, js, cleanup := newObservabilityTestNATS(t)
	defer cleanup()
	obs := subscribeObservations(t, nc)

	store := NewJetStreamEventStore(js, WithObservation(nil)) // a nil conn must degrade to silence, not panic

	ctx := context.Background()
	event := profiledomain.NewCreatedEvent("acme", "01J0TRANSPORTER0000000002")
	seq, err := store.Append(ctx, "acme", "01J0TRANSPORTER0000000002", event, 0)
	if err != nil {
		t.Fatalf("a broken observer changed append's result: %v", err)
	}
	if seq == 0 {
		t.Fatal("expected the domain PubAck sequence unchanged")
	}

	// A rejected append (BR-TP20's optimistic-concurrency guard) must observe
	// nothing at all: the event never reached the stream, so it did not
	// happen. A second store on the same stream, this one observing for real
	// — Phase 43e made observation a construction-time choice, so the switch
	// is a new store rather than a mutation of this one.
	store = NewJetStreamEventStore(js, WithObservation(nc))
	if _, err := store.Append(ctx, "acme", "01J0TRANSPORTER0000000002", event, 0); err != ErrSequenceConflict {
		t.Fatalf("expected ErrSequenceConflict, got %v", err)
	}
	select {
	case m := <-obs:
		t.Fatalf("a rejected append was observed anyway: %s", m.Subject)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestTransporterProfilePayloadsPassTheRedactionReview(t *testing.T) {
	// BR-046's review found exactly two fields here needing action, and put
	// them on the SHARED denylist rather than forking a second one. This spec
	// is the standing check that they never reach the cross-tenant channel.
	nc, js, cleanup := newObservabilityTestNATS(t)
	defer cleanup()
	obs := subscribeObservations(t, nc)

	store := NewJetStreamEventStore(js, WithObservation(nc))

	event := profiledomain.NewCreatedEvent("acme", "01J0TRANSPORTER0000000003")
	event.ActorName = "Dana Whitfield"
	event.ActorSourceIP = "203.0.113.42"
	if _, err := store.Append(context.Background(), "acme", "01J0TRANSPORTER0000000003", event, 0); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-obs:
		body := string(m.Data)
		if strings.Contains(body, "Dana Whitfield") || strings.Contains(body, "203.0.113.42") {
			t.Fatalf("actor PII reached the cross-tenant channel: %s", body)
		}
		// Stripped from the payload and named in "redacted", so an operator
		// sees that something was withheld rather than that nothing was there.
		if !strings.Contains(body, `"redacted":["actorName","actorSourceIP"]`) {
			t.Fatalf("expected both fields named in the redacted list: %s", body)
		}
		// The identifiers an operator needs are still there.
		if !strings.Contains(body, "01J0TRANSPORTER0000000003") {
			t.Fatalf("redaction removed the benign organization id too: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no observation to inspect")
	}
}

// --- BR-037 / BR-TP75: the traceparent (Phase 43e) -----------------------
//
// BR-037 has required a traceparent on every evt.* publish since Phase 28.
// This service's seam never attached one — the rule was in the book, nothing
// enforced it here, and no consumer of these events read the header, so the
// gap cost nothing and stayed invisible. These two specs are the enforcement.

func appendedMsg(t *testing.T, ctx context.Context) *nats.Msg {
	t.Helper()
	nc, js, cleanup := newObservabilityTestNATS(t)
	t.Cleanup(cleanup)

	received := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe(profiledomain.SubjectWildcard, func(m *nats.Msg) { received <- m })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	store := NewJetStreamEventStore(js, WithObservation(nc))
	event := profiledomain.NewCreatedEvent("acme", "01J0TRANSPORTER0000000003")
	if _, err := store.Append(ctx, "acme", "01J0TRANSPORTER0000000003", event, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-received:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the appended event")
		return nil
	}
}

func TestAppendCarriesTheCausingTraceparent(t *testing.T) {
	tracer := natstrace.New(nil)
	sp := tracer.StartOutbound(nil, "rpc.acme.organizations.transporter-profile.create.v1", nil,
		"acme", "organizations", "transporter-profile", "create")
	ctx := natstrace.ContextWithSpan(context.Background(), sp)

	got := appendedMsg(t, ctx).Header.Get(natstrace.TraceparentHeader)
	if got == "" {
		t.Fatal("BR-037: an evt.* append must carry the traceparent of the span that caused it")
	}
	if want := sp.Traceparent(); got != want {
		t.Fatalf("traceparent = %q, want the causing span's %q", got, want)
	}
}

func TestAppendOmitsTraceparentWhenNoSpanIsReachable(t *testing.T) {
	// Nil-safe, exactly as shared/jstream behaves: a hydration or a tool with
	// no span on its ctx publishes an unheadered event rather than a
	// malformed one.
	if got := appendedMsg(t, context.Background()).Header.Get(natstrace.TraceparentHeader); got != "" {
		t.Fatalf("expected no traceparent when ctx carries no span, got %q", got)
	}
}
