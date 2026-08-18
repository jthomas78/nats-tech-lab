// Package commands holds the write-side use cases for the shipping domain.
// Each command hydrates its aggregate(s) from JetStream before applying a
// domain rule, then publishes the resulting event. This is the Petrosyan
// CommandBus pattern adapted for NATS JetStream as the event store.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// Publisher is the outbound port to the event backbone. PublishWithTrace
// (Phase 28d, BR-037) attaches a traceparent header derived from sp when one
// is reachable via the ctx a browserrpc handler seeded with
// natstrace.ContextWithSpan — nil-safe, matching jstream.Publisher's own
// PublishWithTrace exactly (that concrete type already satisfies this
// interface unchanged).
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
	PublishWithTrace(ctx context.Context, sp *natstrace.Span, subject string, data []byte) error
}

// ShipInput carries the caller-supplied fields for a ship command.
type ShipInput struct {
	Context  string `json:"context"` // fleet / KV-bucket qualifier
	ShipID   string `json:"shipID"`
	ShipName string `json:"shipName,omitempty"` // used on Register / first Arrive
	Port     string `json:"port,omitempty"`
}

// ShipCorrectionInput carries the caller-supplied fields for a shipID
// correction (BR-022) — renaming a ship's natural key without affecting its
// surrogate identity.
type ShipCorrectionInput struct {
	Context   string `json:"context"`
	ShipID    string `json:"shipID"`
	NewShipID string `json:"newShipID"`
}

// ShipHandler executes the ship movement commands. It holds both the publish
// port and the JetStream handle used for aggregate hydration (read before write).
type ShipHandler struct {
	pub   Publisher
	js    jetstream.JetStream
	ports domain.PortRepository
}

func NewShipHandler(pub Publisher, js jetstream.JetStream, ports domain.PortRepository) *ShipHandler {
	return &ShipHandler{pub: pub, js: js, ports: ports}
}

func (h *ShipHandler) ArrivePort(ctx context.Context, in ShipInput) (domain.ShipState, error) {
	if err := domain.ValidateContext(in.Context); err != nil {
		return domain.ShipState{}, err
	}
	agg, err := h.hydrateByNaturalKey(ctx, in.Context, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	if !agg.IsRegistered() {
		if err := h.register(ctx, agg, in.Context, in.ShipName); err != nil {
			return domain.ShipState{}, err
		}
	}
	portKnown, err := h.ports.Exists(ctx, in.Context, in.Port)
	if err != nil {
		return domain.ShipState{}, err
	}
	event, err := agg.Arrive(in.Port, in.ShipName, portKnown)
	if err != nil {
		return domain.ShipState{}, err
	}
	event.Context = in.Context
	subject := domain.ShipSubject(in.Context, agg.ID, domain.ShipArrivedEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(subject, event)
	return agg.State(in.Context), nil
}

func (h *ShipHandler) DepartPort(ctx context.Context, in ShipInput) (domain.ShipState, error) {
	if err := domain.ValidateContext(in.Context); err != nil {
		return domain.ShipState{}, err
	}
	agg, err := h.hydrateByNaturalKey(ctx, in.Context, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	event, err := agg.Depart(in.Port)
	if err != nil {
		return domain.ShipState{}, err
	}
	event.Context = in.Context
	subject := domain.ShipSubject(in.Context, agg.ID, domain.ShipDepartedEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(subject, event)
	return agg.State(in.Context), nil
}

// RegisterShip mints a ship's surrogate identity explicitly (BR-021). Also
// invoked implicitly by ArrivePort on a ship's first arrival — pre-registering
// is optional, not a precondition.
func (h *ShipHandler) RegisterShip(ctx context.Context, in ShipInput) (domain.ShipState, error) {
	if err := domain.ValidateContext(in.Context); err != nil {
		return domain.ShipState{}, err
	}
	agg, err := h.hydrateByNaturalKey(ctx, in.Context, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	if err := h.register(ctx, agg, in.Context, in.ShipName); err != nil {
		return domain.ShipState{}, err
	}
	return agg.State(in.Context), nil
}

// register mints a fresh surrogate id for an unregistered aggregate,
// publishes the .registered event, and folds it back in. Shared by
// RegisterShip and ArrivePort's implicit-registration path.
func (h *ShipHandler) register(ctx context.Context, agg *domain.ShipAggregate, shipContext, shipName string) error {
	agg.ID = newSurrogateID()
	event, err := agg.Register(shipName)
	if err != nil {
		return err
	}
	event.Context = shipContext
	subject := domain.ShipSubject(shipContext, agg.ID, domain.ShipRegisteredEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return err
	}
	agg.Apply(subject, event)
	return nil
}

// CorrectShipID renames a registered ship's natural key (BR-022), preserving
// its surrogate identity.
func (h *ShipHandler) CorrectShipID(ctx context.Context, in ShipCorrectionInput) (domain.ShipState, error) {
	if err := domain.ValidateContext(in.Context); err != nil {
		return domain.ShipState{}, err
	}
	if err := domain.ValidateShipID(in.ShipID); err != nil {
		return domain.ShipState{}, err
	}
	if err := domain.ValidateShipID(in.NewShipID); err != nil {
		return domain.ShipState{}, err
	}
	if in.ShipID == in.NewShipID {
		return domain.ShipState{}, fmt.Errorf("newShipID must differ from the current shipID")
	}
	ships, err := h.foldAllShips(ctx, in.Context)
	if err != nil {
		return domain.ShipState{}, err
	}
	target := resolveShipByNaturalKey(ships, in.ShipID)
	if !target.IsRegistered() {
		return domain.ShipState{}, domain.ErrNotFound
	}
	if collision := resolveShipByNaturalKey(ships, in.NewShipID); collision.IsRegistered() {
		return domain.ShipState{}, domain.ErrShipIDInUse
	}
	event, err := target.CorrectShipID(in.NewShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	event.Context = in.Context
	subject := domain.ShipSubject(in.Context, target.ID, domain.ShipIDCorrectedEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ShipState{}, err
	}
	target.Apply(subject, event)
	return target.State(in.Context), nil
}

// hydrateByNaturalKey resolves the ship whose CURRENT shipID matches shipID
// by folding every ship's history in itemContext. A single-ship
// FilterSubject replay (as used for Container) isn't enough here: ShipID is
// mutable (BR-022), so the requested name might belong to a different
// surrogate than it did historically.
func (h *ShipHandler) hydrateByNaturalKey(ctx context.Context, itemContext, shipID string) (*domain.ShipAggregate, error) {
	if err := domain.ValidateShipID(shipID); err != nil {
		return nil, err
	}
	ships, err := h.foldAllShips(ctx, itemContext)
	if err != nil {
		return nil, err
	}
	return resolveShipByNaturalKey(ships, shipID), nil
}

// foldAllShips replays every ship event in itemContext into a map keyed by
// surrogate id. Shared by hydrateByNaturalKey and CorrectShipID, which needs
// to resolve both the source shipID and a possible target-name collision
// from the same replay.
func (h *ShipHandler) foldAllShips(ctx context.Context, itemContext string) (map[string]*domain.ShipAggregate, error) {
	ships := make(map[string]*domain.ShipAggregate)
	replay := func(subject string, data []byte) {
		if !isShipSubject(subject) {
			return
		}
		var event domain.ShipEvent
		if json.Unmarshal(data, &event) == nil && event.Context == itemContext {
			foldShipEvent(ships, subject, event)
		}
	}
	if err := replayFiltered(ctx, h.js, domain.ShipContextWildcard(itemContext), replay); err != nil {
		return nil, err
	}
	return ships, nil
}

// foldShipEvent applies one ship event into the aggregate keyed by its
// subject's surrogate id, creating the aggregate on first sight. Shared with
// ContainerHandler.hydratePair, which folds ship events for the same reason
// (a ship's natural key is no longer the subject-carried identity).
func foldShipEvent(ships map[string]*domain.ShipAggregate, subject string, event domain.ShipEvent) {
	_, subjectID, _, ok := domain.SubjectDetails(subject)
	if !ok {
		return
	}
	agg, exists := ships[subjectID]
	if !exists {
		agg = &domain.ShipAggregate{}
		ships[subjectID] = agg
	}
	agg.Apply(subject, event)
}

// resolveShipByNaturalKey returns the ship whose CURRENT (post-fold) ShipID
// equals shipID. Unlike Container's natural key, a ship's ShipID is mutable
// (CorrectShipID), so resolution can't lock onto the first matching
// .registered event — it must compare each candidate's final folded state.
// Returns a blank, unregistered aggregate if no match.
func resolveShipByNaturalKey(ships map[string]*domain.ShipAggregate, shipID string) *domain.ShipAggregate {
	for _, agg := range ships {
		if agg.IsRegistered() && agg.ShipID == shipID {
			return agg
		}
	}
	return &domain.ShipAggregate{ShipID: shipID}
}

func isShipSubject(subject string) bool {
	aggregate, event, ok := domain.SubjectTokens(subject)
	if !ok || aggregate != "ship" {
		return false
	}
	switch event {
	case domain.ShipRegisteredEvent, domain.ShipArrivedEvent, domain.ShipDepartedEvent, domain.ShipIDCorrectedEvent:
		return true
	default:
		return false
	}
}

// dropConsumer deletes a hydration consumer once its replay is done.
//
// An ordered consumer is ephemeral, but "ephemeral" only means the server
// reaps it after InactiveThreshold (5m by default) — it is NOT removed when
// the client stops pulling. Since every write command hydrates, leaving them
// to expire burns one consumer slot per command for five minutes, and the
// account's JetStream MaxConsumers limit (20 for a tenant account, four of
// which are the durable projectors) is exhausted after ~16 writes. Deleting
// explicitly keeps hydration slot-neutral.
//
// It runs on its own context: the caller's is typically already cancelled or
// about to be by the time this is deferred, which would skip the delete and
// reinstate the leak.
func dropConsumer(js jetstream.JetStream, consumer jetstream.Consumer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = js.DeleteConsumer(ctx, domain.StreamName, consumer.CachedInfo().Name)
}

// replayFiltered folds an aggregate instance's history. Unlike a full-stream
// replay, completion is detected from the filtered consumer's pending count.
func replayFiltered(ctx context.Context, js jetstream.JetStream, filter string, fn func(string, []byte)) error {
	consumer, err := js.OrderedConsumer(ctx, domain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{filter},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("hydrate: create filtered consumer: %w", err)
	}
	defer dropConsumer(js, consumer)
	if consumer.CachedInfo().NumPending == 0 {
		return nil
	}
	msgs, err := consumer.Messages()
	if err != nil {
		return fmt.Errorf("hydrate: filtered messages: %w", err)
	}
	defer msgs.Stop()
	for {
		msg, err := msgs.Next()
		if err != nil {
			break
		}
		fn(msg.Subject(), msg.Data())
		meta, _ := msg.Metadata()
		if meta != nil && meta.NumPending == 0 {
			break
		}
	}
	return nil
}

func (h *ShipHandler) publish(ctx context.Context, subject string, event domain.ShipEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	sp := natstrace.SpanFromContext(ctx)
	return h.pub.PublishWithTrace(ctx, sp, subject, data)
}

// replayStream folds every message in the SHIPPING stream through fn, in
// order, from seq=1 up to the stream's last sequence at call time. It always
// consumes the full subject set — the caller routes by subject — because a
// subject-filtered consumer could never observe the terminating sequence when
// the last message belongs to the other aggregate. Shared by the ship
// hydrator and the container pair-hydrator.
func replayStream(
	ctx context.Context,
	js jetstream.JetStream,
	fn func(subject string, data []byte),
) error {
	info, err := js.Stream(ctx, domain.StreamName)
	if err != nil {
		return fmt.Errorf("hydrate: stream info: %w", err)
	}
	lastSeq := info.CachedInfo().State.LastSeq
	if lastSeq == 0 {
		return nil
	}

	consumer, err := js.OrderedConsumer(ctx, domain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: domain.StreamSubjects(),
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("hydrate: create consumer: %w", err)
	}
	defer dropConsumer(js, consumer)
	msgs, err := consumer.Messages()
	if err != nil {
		return fmt.Errorf("hydrate: messages: %w", err)
	}
	defer msgs.Stop()

	for {
		msg, err := msgs.Next()
		if err != nil {
			break
		}
		fn(msg.Subject(), msg.Data())
		meta, _ := msg.Metadata()
		if meta != nil && meta.Sequence.Stream >= lastSeq {
			break
		}
	}
	return nil
}
