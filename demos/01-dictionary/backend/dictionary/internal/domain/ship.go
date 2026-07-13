// Package domain holds the shipping domain: Ship and Container aggregates,
// their projected read models (ShipState, ContainerState), events, and the
// domain errors. No framework dependencies.
//
// Cargo moved off the ship aggregate in Phase 8: a ship's manifest is now
// derived by joining containers whose OnShipID matches the ship — see
// container.go and the terminal queries.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// ─── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrNotFound      = errors.New("ship not found")
	ErrAlreadyDocked = errors.New("ship is already docked at this port")
	ErrMustDepart    = errors.New("ship must depart current port first")
	ErrNotDocked     = errors.New("ship is not docked at this port")
	ErrNotInPort     = errors.New("ship must be docked to load or unload containers") // BR-012
	ErrUnknownPort   = errors.New("port is not registered")                          // BR-017 (also reused by container.go for BR-018)
)

// ─── Value objects ────────────────────────────────────────────────────────────

// ShipStatus represents the AIS navigational status of a ship.
type ShipStatus string

const (
	StatusInTransit                 ShipStatus = "in-transit"                 // blue
	StatusDocked                    ShipStatus = "docked"                     // green
	StatusAtAnchor                  ShipStatus = "at-anchor"                  // amber
	StatusNotUnderCommand           ShipStatus = "not-under-command"          // red
	StatusRestrictedManoeuvrability ShipStatus = "restricted-manoeuvrability" // orange
)

// ─── Read model (projected into KV and Postgres) ─────────────────────────────

// ShipState is the materialised view stored in NATS KV (Shape A/B read model)
// and in Postgres (Shape B canonical projection). The container manifest is
// NOT part of ship state — it is a join over the container projection
// (OnShipID == ShipID).
type ShipState struct {
	Context     string     `json:"context"` // fleet / KV-bucket qualifier
	ShipID      string     `json:"shipID"`
	ShipName    string     `json:"shipName"`
	Status      ShipStatus `json:"status"`      // AIS navigational status
	CurrentPort string     `json:"currentPort"` // "" = at sea
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// KVKey returns the key within the context-scoped bucket: ship.{shipID}.
func (s ShipState) KVKey() string { return "ship." + s.ShipID }

// ─── Aggregate (command validation + Shape C reconstruction) ──────────────────

// ShipAggregate reconstructs ship state by replaying events. It is the single
// place where the ship rules are enforced (write side) and where Shape C reads
// derive their fleet view (read side). The Hydrate / ReconstructFleet helpers
// that feed events into the aggregate live in the application layer so the
// domain stays free of JetStream imports.
type ShipAggregate struct {
	ShipID      string
	ShipName    string
	CurrentPort string // "" = at sea
	UpdatedAt   time.Time
}

// Apply folds one event into the aggregate's state. Subject selects the
// transition; unknown subjects are silently ignored.
func (a *ShipAggregate) Apply(subject string, event ShipEvent) {
	aggregate, id, eventType, ok := SubjectDetails(subject)
	if !ok || aggregate != "ship" {
		return
	}
	a.ShipID = id
	a.UpdatedAt = event.OccurredAt
	switch eventType {
	case ShipArrivedEvent:
		if a.ShipName == "" && event.ShipName != "" {
			a.ShipName = event.ShipName
		}
		a.CurrentPort = event.Port
	case ShipDepartedEvent:
		a.CurrentPort = ""
	}
}

// State returns a snapshot of the aggregate as a ShipState projection.
func (a *ShipAggregate) State(context string) ShipState {
	status := StatusInTransit
	if a.CurrentPort != "" {
		status = StatusDocked
	}
	return ShipState{
		Context:     context,
		ShipID:      a.ShipID,
		ShipName:    a.ShipName,
		Status:      status,
		CurrentPort: a.CurrentPort,
		UpdatedAt:   a.UpdatedAt,
	}
}

// FromState restores aggregate fields from an existing ShipState so the
// event projector can apply a delta without replaying from JetStream.
func (a *ShipAggregate) FromState(s ShipState) {
	a.ShipID = s.ShipID
	a.ShipName = s.ShipName
	a.CurrentPort = s.CurrentPort
	a.UpdatedAt = s.UpdatedAt
}

// ─── Command methods (each returns the new event or a domain error) ───────────

// Arrive returns a ShipArrived event if the ship is currently at sea.
// shipName is only used when this is the ship's first recorded arrival.
// portKnown reports whether port exists in the ports registry (BR-017); the
// application layer resolves this via PortRepository before calling Arrive,
// the same pattern used for the cross-aggregate checks in container.go.
func (a *ShipAggregate) Arrive(port, shipName string, portKnown bool) (ShipEvent, error) {
	if !portKnown {
		return ShipEvent{}, ErrUnknownPort // BR-017
	}
	if a.CurrentPort == port {
		return ShipEvent{}, ErrAlreadyDocked
	}
	if a.CurrentPort != "" {
		return ShipEvent{}, fmt.Errorf("%w (%s)", ErrMustDepart, a.CurrentPort)
	}
	name := a.ShipName
	if name == "" {
		name = shipName
	}
	return ShipEvent{
		ShipID:     a.ShipID,
		ShipName:   name,
		Port:       port,
		OccurredAt: time.Now().UTC(),
	}, nil
}

// Depart returns a ShipDeparted event if the ship is currently at the named port.
func (a *ShipAggregate) Depart(port string) (ShipEvent, error) {
	if a.CurrentPort != port {
		cur := a.CurrentPort
		if cur == "" {
			cur = "sea"
		}
		return ShipEvent{}, fmt.Errorf("%w (currently: %s)", ErrNotDocked, cur)
	}
	return ShipEvent{
		ShipID:     a.ShipID,
		Port:       port,
		OccurredAt: time.Now().UTC(),
	}, nil
}
