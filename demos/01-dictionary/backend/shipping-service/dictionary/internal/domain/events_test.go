package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubjectTaxonomy(t *testing.T) {
	ship := ShipSubject(Region, Tenant, "SH-001", ShipArrivedEvent)
	if ship != "emea.events.acme.ship.SH-001.arrived" {
		t.Fatalf("unexpected ship subject: %s", ship)
	}
	container := ContainerSubject(Region, Tenant, "9f3c-uuid", ContainerLoadedEvent)
	if container != "emea.events.acme.container.9f3c-uuid.loaded" {
		t.Fatalf("unexpected container subject: %s", container)
	}
	aggregate, id, event, ok := SubjectDetails(container)
	if !ok || aggregate != "container" || id != "9f3c-uuid" || event != "loaded" {
		t.Fatalf("unexpected parsed subject: %q %q %q %v", aggregate, id, event, ok)
	}
	if SubjectShipWildcard != "emea.events.acme.ship.>" || SubjectContainerWildcard != "emea.events.acme.container.>" {
		t.Fatalf("unexpected stream wildcards: %v", StreamSubjects())
	}
}

func TestAggregateIdentityIsCarriedOnlyBySubject(t *testing.T) {
	shipJSON, err := json.Marshal(ShipEvent{ShipID: "SH-001"})
	if err != nil {
		t.Fatal(err)
	}
	containerJSON, err := json.Marshal(ContainerEvent{ID: "9f3c-uuid", ContainerID: "TCKU1234567"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(shipJSON), "SH-001") || strings.Contains(string(containerJSON), "9f3c-uuid") {
		t.Fatalf("aggregate identity leaked into payload: ship=%s container=%s", shipJSON, containerJSON)
	}
}
