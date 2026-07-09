package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

// ContainerInput carries the caller-supplied fields for a container command.
type ContainerInput struct {
	Context     string `json:"context"`              // fleet / KV-bucket qualifier
	ContainerID string `json:"containerID"`          // ISO 6346, e.g. TCKU1234567
	Cargo       string `json:"cargo,omitempty"`      // register
	OriginPort  string `json:"originPort,omitempty"` // register
	DestPort    string `json:"destPort,omitempty"`   // register
	ShipID      string `json:"shipID,omitempty"`     // load / unload
}

// ContainerHandler executes the container commands. Load and Unload need both
// the container's and the ship's state to enforce the cross-aggregate rules
// (BR-008, BR-012, BR-014) — in Phase 8 both aggregates are co-located on the
// single SHIPPING stream, so one atomic replay hydrates both and the checks
// are strongly consistent. Phase 9 splits the streams and turns exactly this
// spot into the distributed-consistency problem.
type ContainerHandler struct {
	pub Publisher
	js  jetstream.JetStream
}

func NewContainerHandler(pub Publisher, js jetstream.JetStream) *ContainerHandler {
	return &ContainerHandler{pub: pub, js: js}
}

// RegisterContainer places a new container in the origin port's terminal.
func (h *ContainerHandler) RegisterContainer(ctx context.Context, in ContainerInput) (domain.ContainerState, error) {
	// Input validation (application layer, like BR-007 — not a domain rule).
	switch {
	case in.ContainerID == "":
		return domain.ContainerState{}, fmt.Errorf("containerID is required")
	case in.Cargo == "":
		return domain.ContainerState{}, fmt.Errorf("cargo is required")
	case in.OriginPort == "":
		return domain.ContainerState{}, fmt.Errorf("originPort is required")
	case in.DestPort == "":
		return domain.ContainerState{}, fmt.Errorf("destPort is required")
	}

	_, cont, err := h.hydratePair(ctx, "", in.ContainerID)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event, err := cont.Register(in.Cargo, in.OriginPort, in.DestPort)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event.Context = in.Context
	if err := h.publish(ctx, domain.SubjectContainerRegistered, event); err != nil {
		return domain.ContainerState{}, err
	}
	cont.Apply(domain.SubjectContainerRegistered, event)
	return cont.State(in.Context), nil
}

// LoadContainer crane-loads a container onto a docked ship.
func (h *ContainerHandler) LoadContainer(ctx context.Context, in ContainerInput) (domain.ContainerState, error) {
	if err := requireIDs(in); err != nil {
		return domain.ContainerState{}, err
	}
	ship, cont, err := h.hydratePair(ctx, in.ShipID, in.ContainerID)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event, err := cont.Load(in.ShipID, ship.CurrentPort)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event.Context = in.Context
	if err := h.publish(ctx, domain.SubjectContainerLoaded, event); err != nil {
		return domain.ContainerState{}, err
	}
	cont.Apply(domain.SubjectContainerLoaded, event)
	return cont.State(in.Context), nil
}

// UnloadContainer crane-unloads a container into the terminal at the ship's
// current port.
func (h *ContainerHandler) UnloadContainer(ctx context.Context, in ContainerInput) (domain.ContainerState, error) {
	if err := requireIDs(in); err != nil {
		return domain.ContainerState{}, err
	}
	ship, cont, err := h.hydratePair(ctx, in.ShipID, in.ContainerID)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event, err := cont.Unload(in.ShipID, ship.CurrentPort)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event.Context = in.Context
	if err := h.publish(ctx, domain.SubjectContainerUnloaded, event); err != nil {
		return domain.ContainerState{}, err
	}
	cont.Apply(domain.SubjectContainerUnloaded, event)
	return cont.State(in.Context), nil
}

func requireIDs(in ContainerInput) error {
	if in.ContainerID == "" {
		return fmt.Errorf("containerID is required")
	}
	if in.ShipID == "" {
		return fmt.Errorf("shipID is required")
	}
	return nil
}

// hydratePair rebuilds BOTH aggregates from one replay of the SHIPPING
// stream: ship.* events with a matching shipID fold into the ShipAggregate,
// container.* events with a matching containerID fold into the
// ContainerAggregate. Because both come from the same replay, cross-aggregate
// rule checks see a single consistent point in time.
func (h *ContainerHandler) hydratePair(ctx context.Context, shipID, containerID string) (*domain.ShipAggregate, *domain.ContainerAggregate, error) {
	ship := &domain.ShipAggregate{ShipID: shipID}
	cont := &domain.ContainerAggregate{ContainerID: containerID}

	replay := func(subject string, data []byte) {
		if isShipSubject(subject) {
			var event domain.ShipEvent
			if json.Unmarshal(data, &event) == nil && shipID != "" && event.ShipID == shipID {
				ship.Apply(subject, event)
			}
			return
		}
		var event domain.ContainerEvent
		if json.Unmarshal(data, &event) == nil && event.ContainerID == containerID {
			cont.Apply(subject, event)
		}
	}
	if err := replayStream(ctx, h.js, replay); err != nil {
		return nil, nil, err
	}
	return ship, cont, nil
}

func (h *ContainerHandler) publish(ctx context.Context, subject string, event domain.ContainerEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return h.pub.Publish(ctx, subject, data)
}
