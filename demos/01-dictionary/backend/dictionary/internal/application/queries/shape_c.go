package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

// ShipWithManifest is a reconstructed ship plus the containers currently on
// board — the manifest is the OnShipID join, computed after the replay.
type ShipWithManifest struct {
	domain.ShipState
	Manifest []domain.ContainerState `json:"manifest"`
}

// FleetReconstruction is the full Shape C result: every ship (with manifest)
// and every container seen in the event history.
type FleetReconstruction struct {
	Fleet      []ShipWithManifest      `json:"fleet"`
	Containers []domain.ContainerState `json:"containers"`
}

// ShapeC reconstructs fleet and container state by replaying the full
// JetStream event log. No KV, no Postgres — pure Fowler-style event sourcing:
// current state is derived entirely from history, with no separate persistent
// read model.
type ShapeC struct {
	js jetstream.JetStream
}

func NewShapeC(js jetstream.JetStream) *ShapeC { return &ShapeC{js: js} }

// ReconstructFleet replays all messages from seq=1, folds ship.* events into
// ShipAggregates and container.* events into ContainerAggregates, then joins
// each ship's manifest (containers with OnShipID == shipID). Callers should
// treat the result as a point-in-time snapshot; new events are not streamed.
func (q *ShapeC) ReconstructFleet(ctx context.Context) (FleetReconstruction, error) {
	empty := FleetReconstruction{Fleet: []ShipWithManifest{}, Containers: []domain.ContainerState{}}

	info, err := q.js.Stream(ctx, domain.StreamName)
	if err != nil {
		return empty, fmt.Errorf("stream info: %w", err)
	}
	lastSeq := info.CachedInfo().State.LastSeq
	if lastSeq == 0 {
		return empty, nil
	}

	consumer, err := q.js.OrderedConsumer(ctx, domain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: domain.StreamSubjects(),
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return empty, fmt.Errorf("ordered consumer: %w", err)
	}
	msgs, err := consumer.Messages()
	if err != nil {
		return empty, fmt.Errorf("messages: %w", err)
	}
	defer msgs.Stop()

	// One mutable aggregate per entity, keyed by ID, built by Apply.
	ships := make(map[string]*domain.ShipAggregate)
	containers := make(map[string]*domain.ContainerAggregate)
	// Most recent fleet context per entity for State().
	shipContexts := make(map[string]string)
	containerContexts := make(map[string]string)

	for {
		msg, err := msgs.Next()
		if err != nil {
			break
		}
		switch msg.Subject() {
		case domain.SubjectShipArrived, domain.SubjectShipDeparted:
			var event domain.ShipEvent
			if json.Unmarshal(msg.Data(), &event) == nil && event.ShipID != "" {
				agg, ok := ships[event.ShipID]
				if !ok {
					agg = &domain.ShipAggregate{}
					ships[event.ShipID] = agg
				}
				agg.Apply(msg.Subject(), event)
				shipContexts[event.ShipID] = event.Context
			}
		case domain.SubjectContainerRegistered, domain.SubjectContainerLoaded, domain.SubjectContainerUnloaded:
			// Fold by the surrogate id (Phase 8.3) — the immutable aggregate
			// identity — so a container's events group under one key regardless
			// of its natural key.
			var event domain.ContainerEvent
			if json.Unmarshal(msg.Data(), &event) == nil && event.ID != "" {
				agg, ok := containers[event.ID]
				if !ok {
					agg = &domain.ContainerAggregate{}
					containers[event.ID] = agg
				}
				agg.Apply(msg.Subject(), event)
				containerContexts[event.ID] = event.Context
			}
		}
		meta, _ := msg.Metadata()
		if meta != nil && meta.Sequence.Stream >= lastSeq {
			break
		}
	}

	result := FleetReconstruction{
		Fleet:      make([]ShipWithManifest, 0, len(ships)),
		Containers: make([]domain.ContainerState, 0, len(containers)),
	}
	for id, agg := range containers {
		result.Containers = append(result.Containers, agg.State(containerContexts[id]))
	}
	sort.Slice(result.Containers, func(i, j int) bool {
		return result.Containers[i].ContainerID < result.Containers[j].ContainerID
	})

	for id, agg := range ships {
		ship := ShipWithManifest{
			ShipState: agg.State(shipContexts[id]),
			Manifest:  []domain.ContainerState{},
		}
		for _, c := range result.Containers {
			if c.OnShipID != nil && *c.OnShipID == id {
				ship.Manifest = append(ship.Manifest, c)
			}
		}
		result.Fleet = append(result.Fleet, ship)
	}
	sort.Slice(result.Fleet, func(i, j int) bool {
		return result.Fleet[i].ShipID < result.Fleet[j].ShipID
	})
	return result, nil
}
