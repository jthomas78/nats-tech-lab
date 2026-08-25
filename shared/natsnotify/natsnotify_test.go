package natsnotify_test

// The interface's test surface. Every property a caller must know about
// Publish is asserted here rather than at each of the nine call sites, which
// is the point of having a seam at all: BR-049 used to be enforced by an AST
// scan over publish sites, and is now a property of this one function.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

func recv(t *testing.T, ch chan *nats.Msg, what string) *nats.Msg {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}

func silence(t *testing.T, ch chan *nats.Msg, what string) {
	t.Helper()
	select {
	case m := <-ch:
		t.Fatalf("expected no %s, got %q", what, m.Subject)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestPublishDeliversTheDomainMessage(t *testing.T) {
	nc := natstest.Start(t, "natsnotify-test")
	got := natstest.Subscribe(t, nc, "notify.>")

	n := natsnotify.New(nc, nil)
	n.Publish(context.Background(),
		natsnotify.Subject{Name: "notify.acme.shipping.ship.changed", Tokens: natsnotify.Tokens{Context: "acme", Service: "shipping", Entity: "ship", Action: "changed"}},
		[]byte(`{"id":"S1"}`))

	m := recv(t, got, "the notify message")
	if m.Subject != "notify.acme.shipping.ship.changed" {
		t.Fatalf("subject = %q", m.Subject)
	}
	if string(m.Data) != `{"id":"S1"}` {
		t.Fatalf("payload = %q", m.Data)
	}
}

func TestPublishObservesUnderTheTokensItWasGivenNotTheOnesInTheSubject(t *testing.T) {
	// The defect this module exists to remove. refdata publishes under
	// _platform but must be attributed to the tenant context, and the
	// positional deriver reads token 1 — so a seam that derived would file
	// every refdata notification under "_platform".
	nc := natstest.Start(t, "natsnotify-test")
	obs := natstest.Observations(t, nc)

	n := natsnotify.New(nc, nil, natsnotify.WithObservation(nc))
	n.Publish(context.Background(),
		natsnotify.Subject{Name: "notify._platform.refdata.acme.ports.changed", Tokens: natsnotify.Tokens{Context: "acme", Service: "refdata", Entity: "ports", Action: "changed"}},
		[]byte(`{}`))

	m := recv(t, obs, "the observation")
	if want := "obs.pubsub.acme.refdata.ports.changed"; m.Subject != want {
		t.Fatalf("observation subject = %q, want %q — the tokens must come from the caller, not the subject", m.Subject, want)
	}
	var env struct {
		Subject   string `json:"subject"`
		Direction string `json:"direction"`
	}
	if err := json.Unmarshal(m.Data, &env); err != nil {
		t.Fatal(err)
	}
	if env.Subject != "notify._platform.refdata.acme.ports.changed" {
		t.Fatalf("envelope records subject %q — it must carry the real published subject", env.Subject)
	}
	if env.Direction != "publish" {
		t.Fatalf("direction = %q, want publish", env.Direction)
	}
	if m.Header.Get(nats.MsgIdHdr) == "" {
		t.Fatal("expected Nats-Msg-Id — BR-047's dedup window depends on it")
	}
}

func TestPublishObservesAFourTokenSubjectThePositionalDeriverWouldSkip(t *testing.T) {
	// notify.accounts.account.created carries no {context} and is one token
	// below natstrace.ObservePublish's floor, so deriving would drop it
	// silently. Explicit tokens make the family's irregular member ordinary.
	nc := natstest.Start(t, "natsnotify-test")
	obs := natstest.Observations(t, nc)

	n := natsnotify.New(nc, nil, natsnotify.WithObservation(nc))
	n.Publish(context.Background(),
		natsnotify.Subject{Name: "notify.accounts.account.created", Tokens: natsnotify.Tokens{Context: "_platform", Service: "accounts", Entity: "account", Action: "created"}},
		[]byte(`{}`))

	m := recv(t, obs, "the observation")
	if want := "obs.pubsub._platform.accounts.account.created"; m.Subject != want {
		t.Fatalf("observation subject = %q, want %q", m.Subject, want)
	}
}

func TestNotifierWithoutObservationStaysSilent(t *testing.T) {
	// The gate. observability-service's own publishers depend on this: their
	// exclusion from BR-045 is expressed by never being given a WithObservation,
	// rather than by an entry in a hand-maintained list.
	nc := natstest.Start(t, "natsnotify-test")
	obs := natstest.Observations(t, nc)
	got := natstest.Subscribe(t, nc, "notify.>")

	n := natsnotify.New(nc, nil)
	n.Publish(context.Background(),
		natsnotify.Subject{Name: "notify.acme.kv.pubsub-messages.k1.changed", Tokens: natsnotify.Tokens{Context: "acme", Service: "kv", Entity: "pubsub-messages", Action: "changed"}},
		[]byte(`{}`))

	recv(t, got, "the notify message") // the domain publish still happens
	silence(t, obs, "observation")
}

func TestObservationContinuesTheSpanOnTheContext(t *testing.T) {
	nc := natstest.Start(t, "natsnotify-test")
	obs := natstest.Observations(t, nc)

	parent := natstrace.New(nc).StartOutbound(nil, "rpc.acme.shipping.ship.get", nil, "acme", "shipping", "ship", "get")
	ctx := natstrace.ContextWithSpan(context.Background(), parent)

	n := natsnotify.New(nc, nil, natsnotify.WithObservation(nc))
	n.Publish(ctx, natsnotify.Subject{Name: "notify.acme.shipping.ship.changed", Tokens: natsnotify.Tokens{Context: "acme", Service: "shipping", Entity: "ship", Action: "changed"}}, []byte(`{}`))

	m := recv(t, obs, "the observation")
	var env struct {
		TraceID      string `json:"traceId"`
		ParentSpanID string `json:"parentSpanId"`
	}
	if err := json.Unmarshal(m.Data, &env); err != nil {
		t.Fatal(err)
	}
	if env.TraceID == "" {
		t.Fatal("observation has no trace id")
	}
	if env.ParentSpanID == "" {
		t.Fatal("observation did not continue the span on the context — it would appear as an orphan, not in the caller's waterfall")
	}
}

func TestPublishToleratesNilConnLoggerAndContext(t *testing.T) {
	// Every call site this replaces already tolerated an unconfigured notify
	// connection as a supported deployment, and several are reached from a KV
	// watch callback with no request context behind them.
	natsnotify.New(nil, nil).Publish(context.Background(), natsnotify.Subject{Name: "notify.acme.shipping.ship.changed"}, []byte(`{}`))

	nc := natstest.Start(t, "natsnotify-test")
	got := natstest.Subscribe(t, nc, "notify.>")
	natsnotify.New(nc, nil, natsnotify.WithObservation(nil)).
		Publish(nil, natsnotify.Subject{Name: "notify.acme.shipping.ship.changed"}, []byte(`{}`)) //nolint:staticcheck // nil ctx is the property under test
	recv(t, got, "the notify message")
}

func TestPublishAttachesTraceparentWhenASpanIsOnTheContext(t *testing.T) {
	// BR-037: the notify carries the trace forward the same way the evt.*
	// seam does, so the waterfall shows the async tail past the KV write the
	// caller already made. A NATS KV entry can never carry a traceparent of
	// its own — jetstream.KeyValue.Put takes no headers — so this notify is
	// the only place the trace can continue.
	nc := natstest.Start(t, "natsnotify-test")
	got := natstest.Subscribe(t, nc, "notify.>")

	parent := natstrace.New(nc).StartOutbound(nil, "rpc.acme.shipping.ship.get", nil, "acme", "shipping", "ship", "get")
	ctx := natstrace.ContextWithSpan(context.Background(), parent)

	natsnotify.New(nc, nil).Publish(ctx,
		natsnotify.Subject{Name: "notify.acme.shipping.ship.changed"}, []byte(`{}`))

	m := recv(t, got, "the notify message")
	if m.Header.Get(natstrace.TraceparentHeader) == "" {
		t.Fatal("no traceparent — the trace stops at the KV write")
	}
}

func TestPublishOmitsTraceparentWhenThereIsNoSpan(t *testing.T) {
	nc := natstest.Start(t, "natsnotify-test")
	got := natstest.Subscribe(t, nc, "notify.>")

	natsnotify.New(nc, nil).Publish(context.Background(),
		natsnotify.Subject{Name: "notify.acme.shipping.ship.changed"}, []byte(`{}`))

	m := recv(t, got, "the notify message")
	if m.Header.Get(natstrace.TraceparentHeader) != "" {
		t.Fatal("traceparent on a message with no span behind it")
	}
}

func TestObservationIsSkippedWhenThePublishNeverReachedTheWire(t *testing.T) {
	// A notify that failed must not appear in the Messages panel as though it
	// had happened. Observation runs on a separate, live connection here, so
	// the absence of an envelope is the publish failure being respected
	// rather than the observing conn being down too.
	obsNC := natstest.Start(t, "natsnotify-obs")
	obs := natstest.Observations(t, obsNC)

	deadNC := natstest.Start(t, "natsnotify-dead")
	deadNC.Close() // a closed conn fails every publish

	natsnotify.New(deadNC, nil, natsnotify.WithObservation(obsNC)).Publish(context.Background(),
		natsnotify.Subject{
			Name:   "notify.acme.shipping.ship.changed",
			Tokens: natsnotify.Tokens{Context: "acme", Service: "shipping", Entity: "ship", Action: "changed"},
		}, []byte(`{}`))

	silence(t, obs, "observation of a publish that failed")
}
