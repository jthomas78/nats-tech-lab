// Package domain holds the shipping domain: ShipState (projected read model),
// ShipAggregate (pure command-side logic and Shape C reconstruction), and
// the four domain errors. No framework dependencies.
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
	ErrNotInPort     = errors.New("ship must be docked to load or unload cargo")
	ErrCargoNotFound = errors.New("cargo item not found in manifest")
)

// ─── Value objects ────────────────────────────────────────────────────────────

// Cargo is a single item in a ship's manifest.
type Cargo struct {
	Description string `json:"description"`
	Units       int    `json:"units"`
}

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
// and in Postgres (Shape B canonical projection).
type ShipState struct {
	Context     string     `json:"context"` // fleet / KV-bucket qualifier
	ShipID      string     `json:"shipID"`
	ShipName    string     `json:"shipName"`
	Status      ShipStatus `json:"status"`      // AIS navigational status
	CurrentPort string     `json:"currentPort"` // "" = at sea
	Cargo       []Cargo    `json:"cargo"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// KVKey returns the key within the context-scoped bucket: ship.{shipID}.
func (s ShipState) KVKey() string { return "ship." + s.ShipID }

// ─── Aggregate (command validation + Shape C reconstruction) ──────────────────

// ShipAggregate reconstructs ship state by replaying events. It is the single
// place where domain rules are enforced (write side) and where Shape C reads
// derive their fleet view (read side). The Hydrate / ReconstructFleet helpers
// that feed events into the aggregate live in the application layer so the
// domain stays free of JetStream imports.
type ShipAggregate struct {
	ShipID      string
	ShipName    string
	CurrentPort string // "" = at sea
	Cargo       []Cargo
	UpdatedAt   time.Time
}

// Apply folds one event into the aggregate's state. Subject selects the
// transition; unknown subjects are silently ignored.
func (a *ShipAggregate) Apply(subject string, event ShipEvent) {
	a.ShipID = event.ShipID
	a.UpdatedAt = event.OccurredAt
	switch subject {
	case SubjectShipArrived:
		if a.ShipName == "" && event.ShipName != "" {
			a.ShipName = event.ShipName
		}
		a.CurrentPort = event.Port
	case SubjectShipDeparted:
		a.CurrentPort = ""
	case SubjectCargoLoaded:
		if event.Cargo != nil {
			a.Cargo = append(a.Cargo, *event.Cargo)
		}
	case SubjectCargoUnloaded:
		if event.Cargo != nil {
			a.removeCargo(event.Cargo.Description)
		}
	}
}

func (a *ShipAggregate) removeCargo(description string) {
	out := a.Cargo[:0]
	for _, c := range a.Cargo {
		if c.Description != description {
			out = append(out, c)
		}
	}
	a.Cargo = out
}

// State returns a snapshot of the aggregate as a ShipState projection.
func (a *ShipAggregate) State(context string) ShipState {
	cargo := make([]Cargo, len(a.Cargo))
	copy(cargo, a.Cargo)
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
		Cargo:       cargo,
		UpdatedAt:   a.UpdatedAt,
	}
}

// FromState restores aggregate fields from an existing ShipState so the
// event projector can apply a delta without replaying from JetStream.
func (a *ShipAggregate) FromState(s ShipState) {
	a.ShipID = s.ShipID
	a.ShipName = s.ShipName
	a.CurrentPort = s.CurrentPort
	a.Cargo = s.Cargo
	a.UpdatedAt = s.UpdatedAt
}

// ─── Command methods (each returns the new event or a domain error) ───────────

// Arrive returns a ShipArrived event if the ship is currently at sea.
// shipName is only used when this is the ship's first recorded arrival.
func (a *ShipAggregate) Arrive(port, shipName string) (ShipEvent, error) {
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

// LoadCargo returns a CargoLoaded event if the ship is docked.
func (a *ShipAggregate) LoadCargo(cargo Cargo) (ShipEvent, error) {
	if a.CurrentPort == "" {
		return ShipEvent{}, ErrNotInPort
	}
	return ShipEvent{
		ShipID:     a.ShipID,
		Cargo:      &cargo,
		OccurredAt: time.Now().UTC(),
	}, nil
}

// UnloadCargo returns a CargoUnloaded event if the ship is docked and the
// cargo item exists in the manifest.
func (a *ShipAggregate) UnloadCargo(cargo Cargo) (ShipEvent, error) {
	if a.CurrentPort == "" {
		return ShipEvent{}, ErrNotInPort
	}
	found := false
	for _, c := range a.Cargo {
		if c.Description == cargo.Description {
			found = true
			break
		}
	}
	if !found {
		return ShipEvent{}, fmt.Errorf("%w: %q", ErrCargoNotFound, cargo.Description)
	}
	return ShipEvent{
		ShipID:     a.ShipID,
		Cargo:      &cargo,
		OccurredAt: time.Now().UTC(),
	}, nil
}
