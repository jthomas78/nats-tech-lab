// Package commands holds the write-side use cases for the shipping domain.
// Each command hydrates the ShipAggregate from JetStream before applying a
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
	Context  string       `json:"context"`            // fleet / KV-bucket qualifier
	ShipID   string       `json:"shipID"`
	ShipName string       `json:"shipName,omitempty"` // used on first Arrive
	Port     string       `json:"port,omitempty"`
	Cargo    *domain.Cargo `json:"cargo,omitempty"`
}

// ShipHandler executes the four ship commands. It holds both the publish
// port and the JetStream handle used for aggregate hydration (read before write).
type ShipHandler struct {
	pub Publisher
	js  jetstream.JetStream
}

func NewShipHandler(pub Publisher, js jetstream.JetStream) *ShipHandler {
	return &ShipHandler{pub: pub, js: js}
}

func (h *ShipHandler) ArrivePort(ctx context.Context, in ShipInput) (domain.ShipState, error) {
	agg, err := h.hydrate(ctx, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	event, err := agg.Arrive(in.Port, in.ShipName)
	if err != nil {
		return domain.ShipState{}, err
	}
	event.Context = in.Context
	if err := h.publish(ctx, domain.SubjectShipArrived, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(domain.SubjectShipArrived, event)
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
	if err := h.publish(ctx, domain.SubjectShipDeparted, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(domain.SubjectShipDeparted, event)
	return agg.State(in.Context), nil
}

func (h *ShipHandler) LoadCargo(ctx context.Context, in ShipInput) (domain.ShipState, error) {
	if in.Cargo == nil {
		return domain.ShipState{}, fmt.Errorf("cargo is required")
	}
	agg, err := h.hydrate(ctx, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	event, err := agg.LoadCargo(*in.Cargo)
	if err != nil {
		return domain.ShipState{}, err
	}
	event.Context = in.Context
	if err := h.publish(ctx, domain.SubjectCargoLoaded, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(domain.SubjectCargoLoaded, event)
	return agg.State(in.Context), nil
}

func (h *ShipHandler) UnloadCargo(ctx context.Context, in ShipInput) (domain.ShipState, error) {
	if in.Cargo == nil {
		return domain.ShipState{}, fmt.Errorf("cargo is required")
	}
	agg, err := h.hydrate(ctx, in.ShipID)
	if err != nil {
		return domain.ShipState{}, err
	}
	event, err := agg.UnloadCargo(*in.Cargo)
	if err != nil {
		return domain.ShipState{}, err
	}
	event.Context = in.Context
	if err := h.publish(ctx, domain.SubjectCargoUnloaded, event); err != nil {
		return domain.ShipState{}, err
	}
	agg.Apply(domain.SubjectCargoUnloaded, event)
	return agg.State(in.Context), nil
}

// hydrate replays all events for shipID from JetStream and returns the current
// aggregate state. If the stream is empty the aggregate is returned blank.
func (h *ShipHandler) hydrate(ctx context.Context, shipID string) (*domain.ShipAggregate, error) {
	agg := &domain.ShipAggregate{ShipID: shipID}

	info, err := h.js.Stream(ctx, domain.StreamName)
	if err != nil {
		return nil, fmt.Errorf("hydrate: stream info: %w", err)
	}
	lastSeq := info.CachedInfo().State.LastSeq
	if lastSeq == 0 {
		return agg, nil
	}

	consumer, err := h.js.OrderedConsumer(ctx, domain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: domain.StreamSubjects(),
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("hydrate: create consumer: %w", err)
	}
	msgs, err := consumer.Messages()
	if err != nil {
		return nil, fmt.Errorf("hydrate: messages: %w", err)
	}
	defer msgs.Stop()

	for {
		msg, err := msgs.Next()
		if err != nil {
			break
		}
		var event domain.ShipEvent
		if json.Unmarshal(msg.Data(), &event) == nil && event.ShipID == shipID {
			agg.Apply(msg.Subject(), event)
		}
		meta, _ := msg.Metadata()
		if meta != nil && meta.Sequence.Stream >= lastSeq {
			break
		}
	}
	return agg, nil
}

func (h *ShipHandler) publish(ctx context.Context, subject string, event domain.ShipEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return h.pub.Publish(ctx, subject, data)
}
