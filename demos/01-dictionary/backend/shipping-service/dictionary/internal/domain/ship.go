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
	ErrUnknownPort   = errors.New("port is not registered")                           // BR-017 (also reused by container.go for BR-018)
	ErrShipExists    = errors.New("ship is already registered")                       // BR-021
	ErrShipIDInUse   = errors.New("shipID is already in use by another ship")         // BR-022
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
	Context     string     `json:"context"` // fleet / KV-key-prefix qualifier
	ID          string     `json:"id"`      // surrogate key (UUID) — aggregate identity
	ShipID      string     `json:"shipID"`  // mutable natural key (call-sign / fleet code)
	ShipName    string     `json:"shipName"`
	Status      ShipStatus `json:"status"`      // AIS navigational status
	CurrentPort string     `json:"currentPort"` // "" = at sea
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// KVKey returns the bare key appended after the context prefix in the
// tenant-scoped bucket: ship.{shipID}. The read model is keyed by the
// human-facing natural key for query convenience (and doubles as the
// natural-key lookup); the surrogate ID is carried as a field. Aggregate
// identity on the write side is still the ID.
func (s ShipState) KVKey() string { return "ship." + s.ShipID }

// ─── Aggregate (command validation + Shape C reconstruction) ──────────────────

// ShipAggregate reconstructs ship state by replaying events. It is the single
// place where the ship rules are enforced (write side) and where Shape C reads
// derive their fleet view (read side). The Hydrate / ReconstructFleet helpers
// that feed events into the aggregate live in the application layer so the
// domain stays free of JetStream imports.
type ShipAggregate struct {
	ID          string // surrogate key (UUID) — the immutable aggregate identity
	ShipID      string // mutable natural key (call-sign / fleet code)
	ShipName    string
	CurrentPort string // "" = at sea
	UpdatedAt   time.Time

	registered bool // set once a .registered event has been applied
}

// Apply folds one event into the aggregate's state. Subject selects the
// transition; unknown subjects are silently ignored. Every event carries the
// surrogate ID (subject) and the natural ShipID (payload); both are
// refreshed on each fold, mirroring ContainerAggregate.Apply.
func (a *ShipAggregate) Apply(subject string, event ShipEvent) {
	aggregate, id, eventType, ok := SubjectDetails(subject)
	if !ok || aggregate != "ship" {
		return
	}
	a.ID = id
	a.ShipID = event.ShipID
	a.UpdatedAt = event.OccurredAt
	switch eventType {
	case ShipRegisteredEvent:
		a.registered = true
		if event.ShipName != "" {
			a.ShipName = event.ShipName
		}
	case ShipArrivedEvent:
		if a.ShipName == "" && event.ShipName != "" {
			a.ShipName = event.ShipName
		}
		a.CurrentPort = event.Port
	case ShipDepartedEvent:
		a.CurrentPort = ""
	case ShipIDCorrectedEvent:
		// a.ShipID is already updated above from event.ShipID (the new value).
	}
}

// IsRegistered reports whether a .registered (or first-arrival implicit
// registration) event has been folded into this aggregate.
func (a *ShipAggregate) IsRegistered() bool { return a.registered }

// Register returns a ShipRegistered event if this shipID has not already
// been registered. a.ID must already hold the freshly-minted surrogate key
// (the application layer resolves registration status by natural key first
// — mirrors ContainerAggregate's Register/RegisterContainer pattern).
// BR-021: a shipID can only be registered once.
func (a *ShipAggregate) Register(shipName string) (ShipEvent, error) {
	if a.registered {
		return ShipEvent{}, ErrShipExists
	}
	return ShipEvent{
		ID:         a.ID,
		ShipID:     a.ShipID,
		ShipName:   shipName,
		OccurredAt: time.Now().UTC(),
	}, nil
}

// CorrectShipID renames the ship's natural key. BR-022: the application
// layer has already validated newShipID against BR-020 and checked it is
// not currently in use by another ship in this context before calling this.
func (a *ShipAggregate) CorrectShipID(newShipID string) (ShipEvent, error) {
	if !a.registered {
		return ShipEvent{}, ErrNotFound
	}
	return ShipEvent{
		ID:             a.ID,
		ShipID:         newShipID,
		PreviousShipID: a.ShipID,
		OccurredAt:     time.Now().UTC(),
	}, nil
}

// State returns a snapshot of the aggregate as a ShipState projection.
func (a *ShipAggregate) State(context string) ShipState {
	status := StatusInTransit
	if a.CurrentPort != "" {
		status = StatusDocked
	}
	return ShipState{
		Context:     context,
		ID:          a.ID,
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
	a.ID = s.ID
	a.ShipID = s.ShipID
	a.ShipName = s.ShipName
	a.CurrentPort = s.CurrentPort
	a.UpdatedAt = s.UpdatedAt
	a.registered = s.ID != ""
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
