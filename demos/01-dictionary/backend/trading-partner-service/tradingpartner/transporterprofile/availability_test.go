package transporterprofile_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
)

// BR-TP55: AvailableForAssignment is a computed read-layer value gated on
// BOTH the vetting gate and a configured tracking credential.
func TestAvailableForAssignmentRequiresGateAndCredential(t *testing.T) {
	creds := map[string]string{"CARTRACK": "API_KEY"}

	cases := []struct {
		name  string
		state domain.State
		want  bool
	}{
		{"gate off, no credential", domain.State{}, false},
		{"gate on, no credential", domain.State{FleetAvailabilityGate: true}, false},
		{"gate off, credential configured", domain.State{TrackingCredentials: creds}, false},
		{"gate on, credential configured", domain.State{FleetAvailabilityGate: true, TrackingCredentials: creds}, true},
	}
	for _, tc := range cases {
		if got := tc.state.AvailableForAssignment(); got != tc.want {
			t.Errorf("%s: AvailableForAssignment()=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// BR-TP54: re-configuring a provider overwrites it rather than accumulating
// versions — the deliberate opposite of BR-TP43's write-once documents.
func TestReconfiguringAProviderOverwrites(t *testing.T) {
	p := &domain.TransporterProfile{}
	p.Apply(domain.NewCreatedEvent("acme-default", "tp-1"))

	first, err := p.ConfigureTrackingCredential("CARTRACK", "API_KEY")
	if err != nil {
		t.Fatalf("first configure: %v", err)
	}
	p.Apply(first)

	second, err := p.ConfigureTrackingCredential("CARTRACK", "USERNAME_PASSWORD")
	if err != nil {
		t.Fatalf("second configure: %v", err)
	}
	p.Apply(second)

	creds := p.State().TrackingCredentials
	if len(creds) != 1 {
		t.Fatalf("expected one entry for a re-configured provider, got %d: %v", len(creds), creds)
	}
	if creds["CARTRACK"] != "USERNAME_PASSWORD" {
		t.Errorf("expected the later credential type to win, got %q", creds["CARTRACK"])
	}
}

// BR-TP52, asserted by what the event must NEVER emit. A rule about
// non-emission is worth testing that way round rather than only by what a
// happy path produces.
func TestConfiguredEventCannotCarryASecret(t *testing.T) {
	const secret = "sk-live-SUPERSECRET-9f3a"

	p := &domain.TransporterProfile{}
	p.Apply(domain.NewCreatedEvent("acme-default", "tp-1"))

	event, err := p.ConfigureTrackingCredential("WEBFLEET", "USERNAME_PASSWORD")
	if err != nil {
		t.Fatalf("configure: %v", err)
	}

	// The aggregate has no parameter or field through which the secret could
	// travel, so the only way it could appear is if someone added one. Encode
	// the whole event — the exact bytes that reach the log — and search them.
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("BR-TP52: the published event contains credential material: %s", raw)
	}

	// And the projected state, which reaches Postgres and the KV cache.
	p.Apply(event)
	rawState, err := json.Marshal(p.State())
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if strings.Contains(string(rawState), secret) {
		t.Fatalf("BR-TP52: projected state contains credential material: %s", rawState)
	}
}
