package domain

import (
	"errors"
	"time"
)

// ─── Errors (BR-008 … BR-015; BR-012 reuses ErrNotInPort from ship.go) ───────

var (
	ErrContainerNotFound      = errors.New("container not found")
	ErrContainerAtDestination = errors.New("container destination matches the ship's current port")    // BR-008
	ErrWrongDestination       = errors.New("container can only be unloaded at its destination port")   // BR-009
	ErrContainerNotInTerminal = errors.New("container must be in a terminal to be loaded")             // BR-010
	ErrContainerNotOnShip     = errors.New("container must be on a ship to be unloaded")               // BR-011
	ErrWrongShip              = errors.New("container is not on this ship")                            // BR-013
	ErrContainerNotAtPort     = errors.New("container is not in a terminal at the ship's current port") // BR-014
	ErrContainerExists        = errors.New("container is already registered")                          // BR-015
)

// ─── Value objects ────────────────────────────────────────────────────────────

// ContainerStatus is where the container currently is in its lifecycle.
type ContainerStatus string

const (
	ContainerInTerminal ContainerStatus = "in-terminal" // in a port terminal yard
	ContainerOnShip     ContainerStatus = "on-ship"     // crane-loaded onto a ship
)

// ─── Read model (projected into KV and Postgres) ─────────────────────────────

// ContainerState is the materialised view stored in the container-{context}
// KV bucket and the Postgres containers table. Location is modelled as two
// explicit nullable fields — exactly one is non-nil at any time — so queries
// never branch on Status to interpret an overloaded string.
type ContainerState struct {
	Context      string          `json:"context"`     // fleet / KV-bucket qualifier
	ContainerID  string          `json:"containerID"` // ISO 6346, e.g. TCKU1234567
	Cargo        string          `json:"cargo"`       // description of contents
	OriginPort   string          `json:"originPort"`
	DestPort     string          `json:"destPort"`
	Status       ContainerStatus `json:"status"`
	TerminalPort *string         `json:"terminalPort,omitempty"` // set iff Status == in-terminal
	OnShipID     *string         `json:"onShipID,omitempty"`     // set iff Status == on-ship
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// KVKey returns the key within the context-scoped bucket: container.{id}.
func (c ContainerState) KVKey() string { return "container." + c.ContainerID }

// ─── Aggregate (command validation + Shape C reconstruction) ──────────────────

// ContainerAggregate reconstructs container state by replaying events. It is
// the single place where the container rules (BR-008 … BR-015) are enforced.
// Cross-aggregate rules (BR-008, BR-012, BR-014) take the ship's identity and
// current port as parameters — in Phase 8 both aggregates hydrate from one
// atomic replay of the SHIPPING stream, so these checks are strongly
// consistent.
type ContainerAggregate struct {
	ContainerID  string
	Cargo        string
	OriginPort   string
	DestPort     string
	Status       ContainerStatus
	TerminalPort *string
	OnShipID     *string
	UpdatedAt    time.Time

	registered bool // set once a .registered event has been applied
}

// Apply folds one event into the aggregate's state. Subject selects the
// transition; unknown subjects are silently ignored.
func (c *ContainerAggregate) Apply(subject string, event ContainerEvent) {
	c.ContainerID = event.ContainerID
	c.UpdatedAt = event.OccurredAt
	switch subject {
	case SubjectContainerRegistered:
		c.registered = true
		c.Cargo = event.Cargo
		c.OriginPort = event.OriginPort
		c.DestPort = event.DestPort
		c.Status = ContainerInTerminal
		port := event.OriginPort
		c.TerminalPort = &port
		c.OnShipID = nil
	case SubjectContainerLoaded:
		c.Status = ContainerOnShip
		ship := event.ShipID
		c.OnShipID = &ship
		c.TerminalPort = nil
	case SubjectContainerUnloaded:
		c.Status = ContainerInTerminal
		port := event.Port
		c.TerminalPort = &port
		c.OnShipID = nil
	}
}

// State returns a snapshot of the aggregate as a ContainerState projection.
func (c *ContainerAggregate) State(context string) ContainerState {
	state := ContainerState{
		Context:     context,
		ContainerID: c.ContainerID,
		Cargo:       c.Cargo,
		OriginPort:  c.OriginPort,
		DestPort:    c.DestPort,
		Status:      c.Status,
		UpdatedAt:   c.UpdatedAt,
	}
	if c.TerminalPort != nil {
		port := *c.TerminalPort
		state.TerminalPort = &port
	}
	if c.OnShipID != nil {
		ship := *c.OnShipID
		state.OnShipID = &ship
	}
	return state
}

// FromState restores aggregate fields from an existing ContainerState so the
// event projector can apply a delta without replaying from JetStream.
func (c *ContainerAggregate) FromState(s ContainerState) {
	c.ContainerID = s.ContainerID
	c.Cargo = s.Cargo
	c.OriginPort = s.OriginPort
	c.DestPort = s.DestPort
	c.Status = s.Status
	c.TerminalPort = s.TerminalPort
	c.OnShipID = s.OnShipID
	c.UpdatedAt = s.UpdatedAt
	c.registered = s.ContainerID != ""
}

// ─── Command methods (each returns the new event or a domain error) ───────────

// Register places a new container in the origin port's terminal.
// BR-015: a container ID can only be registered once.
func (c *ContainerAggregate) Register(cargo, originPort, destPort string) (ContainerEvent, error) {
	if c.registered {
		return ContainerEvent{}, ErrContainerExists // BR-015
	}
	return ContainerEvent{
		ContainerID: c.ContainerID,
		Cargo:       cargo,
		OriginPort:  originPort,
		DestPort:    destPort,
		Port:        originPort,
		OccurredAt:  time.Now().UTC(),
	}, nil
}

// Load crane-loads the container onto the named ship. shipPort is the ship's
// current port ("" = at sea). Rules, in evaluation order:
// BR-012 (ship docked), BR-010 (container in-terminal),
// BR-014 (ship at the container's terminal), BR-008 (not already at destination).
func (c *ContainerAggregate) Load(shipID, shipPort string) (ContainerEvent, error) {
	if !c.registered {
		return ContainerEvent{}, ErrContainerNotFound
	}
	if shipPort == "" {
		return ContainerEvent{}, ErrNotInPort // BR-012
	}
	if c.Status != ContainerInTerminal {
		return ContainerEvent{}, ErrContainerNotInTerminal // BR-010
	}
	if c.TerminalPort == nil || *c.TerminalPort != shipPort {
		return ContainerEvent{}, ErrContainerNotAtPort // BR-014
	}
	if c.DestPort == shipPort {
		return ContainerEvent{}, ErrContainerAtDestination // BR-008
	}
	return ContainerEvent{
		ContainerID: c.ContainerID,
		ShipID:      shipID,
		Port:        shipPort,
		OccurredAt:  time.Now().UTC(),
	}, nil
}

// Unload crane-unloads the container into the terminal at the ship's current
// port. Rules, in evaluation order: BR-012 (ship docked), BR-011 (container
// on-ship), BR-013 (on this ship), BR-009 (only at the destination port).
func (c *ContainerAggregate) Unload(shipID, shipPort string) (ContainerEvent, error) {
	if !c.registered {
		return ContainerEvent{}, ErrContainerNotFound
	}
	if shipPort == "" {
		return ContainerEvent{}, ErrNotInPort // BR-012
	}
	if c.Status != ContainerOnShip {
		return ContainerEvent{}, ErrContainerNotOnShip // BR-011
	}
	if c.OnShipID == nil || *c.OnShipID != shipID {
		return ContainerEvent{}, ErrWrongShip // BR-013
	}
	if shipPort != c.DestPort {
		return ContainerEvent{}, ErrWrongDestination // BR-009
	}
	return ContainerEvent{
		ContainerID: c.ContainerID,
		ShipID:      shipID,
		Port:        shipPort,
		OccurredAt:  time.Now().UTC(),
	}, nil
}
