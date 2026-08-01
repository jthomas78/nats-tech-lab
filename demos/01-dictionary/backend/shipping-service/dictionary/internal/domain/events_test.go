package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubjectTaxonomy(t *testing.T) {
	ship := ShipSubject("acme", "SH-001", ShipArrivedEvent)
	if ship != "evt.acme.shipping.ship.SH-001.arrived" {
		t.Fatalf("unexpected ship subject: %s", ship)
	}
	container := ContainerSubject("acme", "9f3c-uuid", ContainerLoadedEvent)
	if container != "evt.acme.shipping.container.9f3c-uuid.loaded" {
		t.Fatalf("unexpected container subject: %s", container)
	}
	aggregate, id, event, ok := SubjectDetails(container)
	if !ok || aggregate != "container" || id != "9f3c-uuid" || event != "loaded" {
		t.Fatalf("unexpected parsed subject: %q %q %q %v", aggregate, id, event, ok)
	}
	if SubjectShipWildcard != "evt.*.shipping.ship.>" || SubjectContainerWildcard != "evt.*.shipping.container.>" {
		t.Fatalf("unexpected stream wildcards: %v", StreamSubjects())
	}
	// Different contexts sharing a shipID/id must not collide on subject.
	if ShipSubject("acme-atlantic-fleet", "SH-001", ShipArrivedEvent) == ShipSubject("acme-pacific-fleet", "SH-001", ShipArrivedEvent) {
		t.Fatal("ship subjects for different contexts must not collide")
	}
}

// TestAggregateIdentityIsCarriedOnlyBySubject asserts the surrogate id (the
// aggregate's true, subject-carried identity) never leaks into the payload,
// while the mutable natural key (ShipID / ContainerID) — which is NOT the
// aggregate identity and is meant to be searchable/correctable — IS carried
// in the payload for both aggregates.
func TestAggregateIdentityIsCarriedOnlyBySubject(t *testing.T) {
	shipJSON, err := json.Marshal(ShipEvent{ID: "7a1c-uuid", ShipID: "SH-001"})
	if err != nil {
		t.Fatal(err)
	}
	containerJSON, err := json.Marshal(ContainerEvent{ID: "9f3c-uuid", ContainerID: "TCKU1234567"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(shipJSON), "7a1c-uuid") || strings.Contains(string(containerJSON), "9f3c-uuid") {
		t.Fatalf("surrogate id leaked into payload: ship=%s container=%s", shipJSON, containerJSON)
	}
	if !strings.Contains(string(shipJSON), "SH-001") {
		t.Fatalf("ship natural key must be present in payload: ship=%s", shipJSON)
	}
	if !strings.Contains(string(containerJSON), "TCKU1234567") {
		t.Fatalf("container natural key must be present in payload: container=%s", containerJSON)
	}
}
