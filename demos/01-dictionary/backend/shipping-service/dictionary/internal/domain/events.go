package domain

import (
	"strings"
	"time"
)

// Both aggregates (Ship and Container) are co-located on the single SHIPPING
// stream, partitioned by subject. This keeps every cross-aggregate rule
// checkable from one atomic replay (Phase 8 baseline). Phase 16 extracts the
// container subjects into a dedicated TERMINAL stream to expose the
// distributed-consistency problem.
const (
	StreamName = "SHIPPING"
	Region     = "emea"
	Tenant     = "acme"

	ShipArrivedEvent         = "arrived"
	ShipDepartedEvent        = "departed"
	ContainerRegisteredEvent = "registered"
	ContainerLoadedEvent     = "loaded"
	ContainerUnloadedEvent   = "unloaded"

	// SubjectWildcard matches every event in the SHIPPING stream.
	SubjectWildcard = Region + ".events." + Tenant + ".>"
	// SubjectShipWildcard matches only ship movement events.
	SubjectShipWildcard = Region + ".events." + Tenant + ".ship.>"
	// SubjectContainerWildcard matches only container lifecycle events.
	SubjectContainerWildcard = Region + ".events." + Tenant + ".container.>"
)

func ShipSubject(region, tenant, shipID, event string) string {
	return strings.Join([]string{region, "events", tenant, "ship", shipID, event}, ".")
}

func ContainerSubject(region, tenant, id, event string) string {
	return strings.Join([]string{region, "events", tenant, "container", id, event}, ".")
}

func ShipInstanceSubject(region, tenant, shipID string) string {
	return strings.Join([]string{region, "events", tenant, "ship", shipID, ">"}, ".")
}

// SubjectDetails returns the aggregate identity and event tokens from a
// production-form subject: {region}.events.{tenant}.{aggregate}.{id}.{event}.
func SubjectDetails(subject string) (aggregate, id, event string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[1] != "events" || parts[4] == "" {
		return "", "", "", false
	}
	return parts[3], parts[4], parts[5], true
}

func SubjectTokens(subject string) (aggregate, event string, ok bool) {
	aggregate, _, event, ok = SubjectDetails(subject)
	return
}

// StreamSubjects lists the subjects bound to the SHIPPING stream.
func StreamSubjects() []string {
	return []string{
		SubjectShipWildcard,
		SubjectContainerWildcard,
	}
}

// ShipEvent is the envelope published on every ship subject. Only the fields
// relevant to the subject are populated; the rest are zero values.
type ShipEvent struct {
	Context    string    `json:"context"`            // fleet / KV-bucket qualifier
	ShipID     string    `json:"-"`                  // aggregate identity comes from the subject
	ShipName   string    `json:"shipName,omitempty"` // carried on .arrived for replay
	Port       string    `json:"port,omitempty"`     // .arrived / .departed
	OccurredAt time.Time `json:"occurredAt"`
}

// ContainerEvent is the envelope published on every container subject.
//
// ID is the container's immutable surrogate key (UUID), minted once at
// registration and carried in every event subject (Phase 8.3). It — not the ISO
// 6346 ContainerID — is the aggregate identity that hydration folds by.
type ContainerEvent struct {
	Context     string    `json:"context"`              // fleet / KV-bucket qualifier
	ID          string    `json:"-"`                    // aggregate identity comes from the subject
	ContainerID string    `json:"containerID"`          // ISO 6346 natural key
	Cargo       string    `json:"cargo,omitempty"`      // .registered
	OriginPort  string    `json:"originPort,omitempty"` // .registered
	DestPort    string    `json:"destPort,omitempty"`   // .registered
	Port        string    `json:"port,omitempty"`       // terminal port (.registered/.unloaded) or ship's port (.loaded)
	ShipID      string    `json:"shipID,omitempty"`     // .loaded / .unloaded
	OccurredAt  time.Time `json:"occurredAt"`
}
