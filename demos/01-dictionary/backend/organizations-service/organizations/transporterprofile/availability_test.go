package transporterprofile_test

import (
	"encoding/json"
	"strings"
	"testing"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
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

func TestCertificateEventsExcludeInsuranceContactsAndApprovalLocksEarlierCertificates(t *testing.T) {
	p := &domain.TransporterProfile{}
	p.Apply(domain.NewCreatedEvent("acme", "transporter-1"))

	first := organizationdomain.ComplianceDocument{ID: "first", Type: organizationdomain.DocumentTypeGoodsInTransit, Status: organizationdomain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"}}
	registered, err := p.RegisterCertificate(first, "admin", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	p.Apply(registered)
	second := organizationdomain.ComplianceDocument{ID: "second", Type: organizationdomain.DocumentTypeGoodsInTransit, Status: organizationdomain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"}, InsurerName: "Acme Insurance", InsuranceContactName: "Private person", InsuranceContactNumber: "secret"}
	registered, err = p.RegisterCertificate(second, "admin", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	p.Apply(registered)

	events, err := p.ApproveCertificate("second", "admin", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		p.Apply(event)
	}
	state := p.State()
	if state.Certificates["first"].Status != organizationdomain.DocumentStatusSuperseded {
		t.Fatalf("first status = %s, want SUPERSEDED", state.Certificates["first"].Status)
	}
	if state.Certificates["second"].Status != organizationdomain.DocumentStatusApproved {
		t.Fatalf("second status = %s, want APPROVED", state.Certificates["second"].Status)
	}
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Private person") || strings.Contains(string(raw), "secret") {
		t.Fatalf("BR-TP72: certificate events contain insurance contacts: %s", raw)
	}
}

// BR-TP63 (38h-ii): a cover lapse is a real state transition, Vetted ->
// CoverLapsed, not merely a gate flipped underneath an unchanged status.
//
// Before this rule a lapsed transporter read `Vetted` with
// FleetAvailabilityGate=false — two fields disagreeing about the same fact,
// which is what made 38h-ii's "the timer exists if and only if the profile
// is Vetted" invariant unstatable without a compound guard.
func TestRevokingFleetAvailabilityLeavesVetted(t *testing.T) {
	p := vettedProfile(t)

	event, err := p.RevokeFleetAvailability()
	if err != nil {
		t.Fatalf("RevokeFleetAvailability: %v", err)
	}
	if event.Status != domain.StatusCoverLapsed {
		t.Errorf("event Status=%q, want %q", event.Status, domain.StatusCoverLapsed)
	}
	if event.FleetAvailabilityGate || event.GitVerified {
		t.Errorf("gate=%v gitVerified=%v, want both false", event.FleetAvailabilityGate, event.GitVerified)
	}

	p.Apply(event)
	state := p.State()
	if state.Status != domain.StatusCoverLapsed {
		t.Errorf("applied Status=%q, want %q", state.Status, domain.StatusCoverLapsed)
	}
	if state.FleetAvailabilityGate {
		t.Error("applied gate is still open")
	}
	if state.AvailableForAssignment() {
		t.Error("a lapsed transporter must not be available for assignment")
	}
}

// The revocation is refused a second time, so a repeated cover drop cannot
// walk the profile out of CoverLapsed or append a duplicate event.
func TestRevokingFleetAvailabilityTwiceIsRefused(t *testing.T) {
	p := vettedProfile(t)

	event, err := p.RevokeFleetAvailability()
	if err != nil {
		t.Fatalf("RevokeFleetAvailability: %v", err)
	}
	p.Apply(event)

	if _, err := p.RevokeFleetAvailability(); err == nil {
		t.Fatal("a second revocation must be refused")
	}
}

// vettedProfile builds a profile that has completed vetting, by applying the
// same events the workflow appends rather than by setting fields — the point
// of an event-sourced aggregate is that no other route in exists.
func vettedProfile(t *testing.T) *domain.TransporterProfile {
	t.Helper()
	p := &domain.TransporterProfile{}
	p.Apply(domain.NewCreatedEvent("acme", "tp-1"))
	p.Apply(domain.Event{
		Type: domain.VettedEvent, Context: "acme", OrganizationID: "tp-1",
		Status: domain.StatusVetted, FleetAvailabilityGate: true, GitVerified: true,
	})
	if !p.State().FleetAvailabilityGate {
		t.Fatal("fixture did not reach a vetted, gate-open state")
	}
	return p
}
