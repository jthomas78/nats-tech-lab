package notify_test

// The subject shapes, asserted without a NATS server.
//
// Phase 43d: these replace publish_notify_test.go, which exercised
// eventhandler's publishNotify/publishRawNotify helpers over an embedded
// server to assert properties — traceparent propagation, nil-safety,
// observation ordering — that now belong to shared/natsnotify and are tested
// once there. What is left is the part that is genuinely this service's: the
// subjects it publishes on and the tokens each is attributed by.

import (
	"testing"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/notify"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
)

func check(t *testing.T, got natsnotify.Subject, wantName string, wantTok natsnotify.Tokens) {
	t.Helper()
	if got.Name != wantName {
		t.Errorf("subject = %q, want %q", got.Name, wantName)
	}
	if got.Tokens != wantTok {
		t.Errorf("tokens = %+v, want %+v", got.Tokens, wantTok)
	}
}

func TestChanged(t *testing.T) {
	check(t, notify.Changed("acme", "ship"),
		"notify.acme.shipping.ship.changed",
		natsnotify.Tokens{Context: "acme", Service: "shipping", Entity: "ship", Action: "changed"})
}

func TestRawNamesTheEntityAndVerbRatherThanTheirPositions(t *testing.T) {
	// The literal "raw" sits where a positional reader takes the entity, and
	// the action is the domain verb rather than "changed". Deriving would
	// file every raw event under an entity called "raw".
	check(t, notify.Raw("acme", "ship", "arrived"),
		"notify.acme.shipping.raw.ship.arrived",
		natsnotify.Tokens{Context: "acme", Service: "shipping", Entity: "ship", Action: "arrived"})
}

func TestPortChanged(t *testing.T) {
	check(t, notify.PortChanged("acme"),
		"notify.acme.shipping.port.changed",
		natsnotify.Tokens{Context: "acme", Service: "shipping", Entity: "port", Action: "changed"})
}

func TestKVChangedKeepsItsTokensStableAsTheKeyGrows(t *testing.T) {
	// KV keys are themselves dotted ({context}.{entityType}.{id}), so this
	// subject has no fixed arity. The tokens must not move with the key.
	short := notify.KVChanged("acme", "ships", "ship.S1")
	long := notify.KVChanged("acme", "ships", "acme.ship.S1")
	check(t, short, "notify.acme.kv.ships.ship.S1.changed",
		natsnotify.Tokens{Context: "acme", Service: "kv", Entity: "ships", Action: "changed"})
	if long.Tokens != short.Tokens {
		t.Errorf("a longer key changed the tokens: %+v vs %+v", long.Tokens, short.Tokens)
	}
}

func TestRefdataChangedIsAttributedToTheBusinessContextNotThePlumbing(t *testing.T) {
	// The subject's own {context} position holds "_platform" because this
	// bridge republishes into PLATFORM, but the change belongs to acme. This
	// is the discrepancy that makes deriving tokens from a subject wrong
	// rather than merely fragile.
	got := notify.RefdataChanged("acme", "ports")
	check(t, got, "notify._platform.refdata.acme.ports.changed",
		natsnotify.Tokens{Context: "acme", Service: "refdata", Entity: "ports", Action: "changed"})
	if got.Tokens.Context == "_platform" {
		t.Error("attributed to the bridge's plumbing rather than the business context")
	}
}
