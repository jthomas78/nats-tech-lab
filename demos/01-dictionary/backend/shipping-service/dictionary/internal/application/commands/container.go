package commands

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
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
// (BR-008, BR-012, BR-014) — 	in Phase 8 both aggregates are co-located on the
// single SHIPPING stream, so one atomic replay hydrates both and the checks
// are strongly consistent. Phase 23 splits the streams and turns exactly this
// spot into the distributed-consistency problem.
type ContainerHandler struct {
	pub   Publisher
	js    jetstream.JetStream
	ports domain.PortRepository
}

func NewContainerHandler(pub Publisher, js jetstream.JetStream, ports domain.PortRepository) *ContainerHandler {
	return &ContainerHandler{pub: pub, js: js, ports: ports}
}

// RegisterContainer places a new container in the origin port's terminal.
func (h *ContainerHandler) RegisterContainer(ctx context.Context, in ContainerInput) (domain.ContainerState, error) {
	if err := domain.ValidateContext(in.Context); err != nil {
		return domain.ContainerState{}, err
	}
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

	// Resolve the natural key against the event stream (strongly consistent,
	// authoritative) to detect an existing registration for BR-015. A brand-new
	// container gets a freshly-minted surrogate key; an already-registered one
	// hydrates its existing id so Register can reject the duplicate.
	cont, err := h.hydrateByNaturalKey(ctx, in.Context, in.ContainerID)
	if err != nil {
		return domain.ContainerState{}, err
	}
	if !cont.IsRegistered() {
		cont.ID = newSurrogateID()
	}
	originKnown, err := h.ports.Exists(ctx, in.Context, in.OriginPort)
	if err != nil {
		return domain.ContainerState{}, err
	}
	destKnown, err := h.ports.Exists(ctx, in.Context, in.DestPort)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event, err := cont.Register(in.Cargo, in.OriginPort, in.DestPort, originKnown, destKnown)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event.Context = in.Context
	subject := domain.ContainerSubject(in.Context, event.ID, domain.ContainerRegisteredEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ContainerState{}, err
	}
	cont.Apply(subject, event)
	return cont.State(in.Context), nil
}

// LoadContainer crane-loads a container onto a docked ship.
func (h *ContainerHandler) LoadContainer(ctx context.Context, in ContainerInput) (domain.ContainerState, error) {
	if err := domain.ValidateContext(in.Context); err != nil {
		return domain.ContainerState{}, err
	}
	if err := requireIDs(in); err != nil {
		return domain.ContainerState{}, err
	}
	ship, cont, err := h.hydratePair(ctx, in.Context, in.ShipID, in.ContainerID)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event, err := cont.Load(in.ShipID, ship.CurrentPort)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event.Context = in.Context
	subject := domain.ContainerSubject(in.Context, event.ID, domain.ContainerLoadedEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ContainerState{}, err
	}
	cont.Apply(subject, event)
	return cont.State(in.Context), nil
}

// UnloadContainer crane-unloads a container into the terminal at the ship's
// current port.
func (h *ContainerHandler) UnloadContainer(ctx context.Context, in ContainerInput) (domain.ContainerState, error) {
	if err := domain.ValidateContext(in.Context); err != nil {
		return domain.ContainerState{}, err
	}
	if err := requireIDs(in); err != nil {
		return domain.ContainerState{}, err
	}
	ship, cont, err := h.hydratePair(ctx, in.Context, in.ShipID, in.ContainerID)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event, err := cont.Unload(in.ShipID, ship.CurrentPort)
	if err != nil {
		return domain.ContainerState{}, err
	}
	event.Context = in.Context
	subject := domain.ContainerSubject(in.Context, event.ID, domain.ContainerUnloadedEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ContainerState{}, err
	}
	cont.Apply(subject, event)
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

// hydratePair rebuilds BOTH aggregates from one replay of the SHIPPING stream,
// scoped to itemContext so two tenants sharing a shipID or container natural
// key never fold into the same aggregate. Ship events fold into a
// surrogate-id-keyed map (foldShipEvent) — like Container's natural key,
// Ship's ShipID is mutable (BR-022), so the ship is resolved by CURRENT name
// (resolveShipByNaturalKey) only after every ship's history is folded, not
// by matching the subject directly. Container events fold by the immutable
// surrogate id (Phase 8.3): the id is resolved from the .registered event
// whose natural key matches containerID within the same context, then every
// event carrying that id is folded — so identity is the UUID, not the
// mutable ISO 6346 natural key. Because both aggregates come from the same
// replay, cross-aggregate rule checks see a single consistent point in time.
func (h *ContainerHandler) hydratePair(ctx context.Context, itemContext, shipID, containerID string) (*domain.ShipAggregate, *domain.ContainerAggregate, error) {
	ships := make(map[string]*domain.ShipAggregate)
	cont := &domain.ContainerAggregate{}
	var targetID string // surrogate id the natural key resolves to

	replay := func(subject string, data []byte) {
		if isShipSubject(subject) {
			var event domain.ShipEvent
			if json.Unmarshal(data, &event) == nil && event.Context == itemContext {
				foldShipEvent(ships, subject, event)
			}
			return
		}
		var event domain.ContainerEvent
		if json.Unmarshal(data, &event) != nil {
			return
		}
		if event.Context != itemContext {
			return
		}
		aggregate, subjectID, eventType, ok := domain.SubjectDetails(subject)
		if ok && aggregate == "container" && eventType == domain.ContainerRegisteredEvent && targetID == "" && event.ContainerID == containerID {
			targetID = subjectID
		}
		if targetID != "" && subjectID == targetID {
			cont.Apply(subject, event)
		}
	}
	if err := replayStream(ctx, h.js, replay); err != nil {
		return nil, nil, err
	}
	return resolveShipByNaturalKey(ships, shipID), cont, nil
}

// hydrateByNaturalKey folds every container event with a matching natural key
// and itemContext into a fresh aggregate. Used only by Register, which has no
// surrogate id yet: it must resolve uniqueness (BR-015) by the natural key
// against the event stream, scoped to itemContext so two tenants may reuse
// the same ISO 6346 code without colliding. If a registration exists the
// aggregate carries its id and reports IsRegistered(); otherwise it is blank
// and the caller mints a new id.
func (h *ContainerHandler) hydrateByNaturalKey(ctx context.Context, itemContext, containerID string) (*domain.ContainerAggregate, error) {
	cont := &domain.ContainerAggregate{ContainerID: containerID}
	replay := func(subject string, data []byte) {
		if isShipSubject(subject) {
			return
		}
		var event domain.ContainerEvent
		if json.Unmarshal(data, &event) == nil && event.ContainerID == containerID && event.Context == itemContext {
			cont.Apply(subject, event)
		}
	}
	if err := replayStream(ctx, h.js, replay); err != nil {
		return nil, err
	}
	return cont, nil
}

// newSurrogateID returns a random RFC 4122 v4 UUID. Containers use it as their
// immutable aggregate identity (Phase 8.3), decoupling identity from the
// mutable ISO 6346 natural key. Dependency-free so the POC builds offline.
func newSurrogateID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (h *ContainerHandler) publish(ctx context.Context, subject string, event domain.ContainerEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return h.pub.Publish(ctx, subject, data)
}
