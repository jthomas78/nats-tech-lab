package queries

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

// ShapeC reconstructs fleet state by replaying the full JetStream event log.
// No KV, no Postgres — pure Fowler-style event sourcing: current state is
// derived entirely from history, with no separate persistent read model.
type ShapeC struct {
	js jetstream.JetStream
}

func NewShapeC(js jetstream.JetStream) *ShapeC { return &ShapeC{js: js} }

// ReconstructFleet replays all messages from seq=1 and returns the current
// state of every ship seen in the stream. Callers should treat the result as
// a point-in-time snapshot; new events are not streamed.
func (q *ShapeC) ReconstructFleet(ctx context.Context) ([]domain.ShipState, error) {
	info, err := q.js.Stream(ctx, domain.StreamName)
	if err != nil {
		return nil, fmt.Errorf("stream info: %w", err)
	}
	lastSeq := info.CachedInfo().State.LastSeq
	if lastSeq == 0 {
		return []domain.ShipState{}, nil
	}

	consumer, err := q.js.OrderedConsumer(ctx, domain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: domain.StreamSubjects(),
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("ordered consumer: %w", err)
	}
	msgs, err := consumer.Messages()
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	defer msgs.Stop()

	// aggregates maps shipID → mutable aggregate built by Apply.
	aggregates := make(map[string]*domain.ShipAggregate)
	// contexts tracks the most recent fleet context per ship for State().
	contexts := make(map[string]string)

	for {
		msg, err := msgs.Next()
		if err != nil {
			break
		}
		var event domain.ShipEvent
		if json.Unmarshal(msg.Data(), &event) == nil && event.ShipID != "" {
			agg, ok := aggregates[event.ShipID]
			if !ok {
				agg = &domain.ShipAggregate{}
				aggregates[event.ShipID] = agg
			}
			agg.Apply(msg.Subject(), event)
			contexts[event.ShipID] = event.Context
		}
		meta, _ := msg.Metadata()
		if meta != nil && meta.Sequence.Stream >= lastSeq {
			break
		}
	}

	fleet := make([]domain.ShipState, 0, len(aggregates))
	for id, agg := range aggregates {
		fleet = append(fleet, agg.State(contexts[id]))
	}
	return fleet, nil
}
