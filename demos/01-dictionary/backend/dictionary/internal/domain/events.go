package domain

import "time"

// Both aggregates (Ship and Container) are co-located on the single SHIPPING
// stream, partitioned by subject. This keeps every cross-aggregate rule
// checkable from one atomic replay (Phase 8 baseline). Phase 9 extracts the
// container subjects into a dedicated TERMINAL stream to expose the
// distributed-consistency problem.
const (
	StreamName = "SHIPPING"

	SubjectShipArrived  = "SHIPPING.ship.arrived"
	SubjectShipDeparted = "SHIPPING.ship.departed"

	SubjectContainerRegistered = "SHIPPING.container.registered"
	SubjectContainerLoaded     = "SHIPPING.container.loaded"
	SubjectContainerUnloaded   = "SHIPPING.container.unloaded"

	// SubjectWildcard matches every event in the SHIPPING stream.
	SubjectWildcard = "SHIPPING.>"
	// SubjectShipWildcard matches only ship movement events.
	SubjectShipWildcard = "SHIPPING.ship.>"
	// SubjectContainerWildcard matches only container lifecycle events.
	SubjectContainerWildcard = "SHIPPING.container.>"
)

// StreamSubjects lists the subjects bound to the SHIPPING stream.
func StreamSubjects() []string {
	return []string{
		SubjectShipArrived,
		SubjectShipDeparted,
		SubjectContainerRegistered,
		SubjectContainerLoaded,
		SubjectContainerUnloaded,
	}
}

// ShipEvent is the envelope published on every ship subject. Only the fields
// relevant to the subject are populated; the rest are zero values.
type ShipEvent struct {
	Context    string    `json:"context"`            // fleet / KV-bucket qualifier
	ShipID     string    `json:"shipID"`             // stable machine identifier
	ShipName   string    `json:"shipName,omitempty"` // carried on .arrived for replay
	Port       string    `json:"port,omitempty"`     // .arrived / .departed
	OccurredAt time.Time `json:"occurredAt"`
}

// ContainerEvent is the envelope published on every container subject.
type ContainerEvent struct {
	Context     string    `json:"context"`              // fleet / KV-bucket qualifier
	ContainerID string    `json:"containerID"`          // ISO 6346 identifier
	Cargo       string    `json:"cargo,omitempty"`      // .registered
	OriginPort  string    `json:"originPort,omitempty"` // .registered
	DestPort    string    `json:"destPort,omitempty"`   // .registered
	Port        string    `json:"port,omitempty"`       // terminal port (.registered/.unloaded) or ship's port (.loaded)
	ShipID      string    `json:"shipID,omitempty"`     // .loaded / .unloaded
	OccurredAt  time.Time `json:"occurredAt"`
}
