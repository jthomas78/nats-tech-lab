// Package commands holds the write-side use cases for the shipping domain.
// Each command hydrates its aggregate(s) from JetStream before applying a
// domain rule, then publishes the resulting event. This is the Petrosyan
// CommandBus pattern adapted for NATS JetStream as the event store.
package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

// Publisher is the outbound port to the event backbone.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// ShipInput carries the caller-supplied fields for a ship command.
type ShipInput struct {
	Context  string `json:"context"` // fleet / KV-bucket qualifier
	ShipID   string `json:"shipID"`
	ShipName string `json:"shipName,omitempty"` // used on first Arrive
	Port     string `json:"port,omitempty"`
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
	agg, err := h.hydrate(ctx, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
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
	subject := domain.ShipSubject(domain.Region, domain.Tenant, in.ShipID, domain.ShipArrivedEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(subject, event)
	return agg.State(in.Context), nil
}

func (h *ShipHandler) DepartPort(ctx context.Context, in ShipInput) (domain.ShipState, error) {
	agg, err := h.hydrate(ctx, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	event, err := agg.Depart(in.Port)
	if err != nil {
		return domain.ShipState{}, err
	}
	event.Context = in.Context
	subject := domain.ShipSubject(domain.Region, domain.Tenant, in.ShipID, domain.ShipDepartedEvent)
	if err := h.publish(ctx, subject, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(subject, event)
	return agg.State(in.Context), nil
}

// hydrate replays the stream and folds the ship events for shipID into a
// fresh aggregate. If the stream is empty the aggregate is returned blank.
func (h *ShipHandler) hydrate(ctx context.Context, shipID string) (*domain.ShipAggregate, error) {
	agg := &domain.ShipAggregate{ShipID: shipID}

	replay := func(subject string, data []byte) {
		if !isShipSubject(subject) {
			return
		}
		var event domain.ShipEvent
		if json.Unmarshal(data, &event) == nil {
			agg.Apply(subject, event)
		}
	}
	if err := replayFiltered(ctx, h.js, domain.ShipInstanceSubject(domain.Region, domain.Tenant, shipID), replay); err != nil {
		return nil, err
	}
	return agg, nil
}

func isShipSubject(subject string) bool {
	aggregate, event, ok := domain.SubjectTokens(subject)
	return ok && aggregate == "ship" && (event == domain.ShipArrivedEvent || event == domain.ShipDepartedEvent)
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
	return h.pub.Publish(ctx, subject, data)
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
