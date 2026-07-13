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

	consumer, err := q.js.OrderedConsumer(ctx, domain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: domain.StreamSubjects(),
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return empty, fmt.Errorf("ordered consumer: %w", err)
	}
	// Completion is measured against the filtered consumer's pending count, not
	// the stream's LastSeq. After a subject migration the stream can still hold
	// messages that no longer match StreamSubjects() (e.g. pre-Phase-9 events);
	// folding until LastSeq would block forever on a tail message the filtered
	// consumer never delivers.
	if consumer.CachedInfo().NumPending == 0 {
		return empty, nil
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
		aggregate, id, eventType, subjectOK := domain.SubjectDetails(msg.Subject())
		switch {
		case subjectOK && aggregate == "ship" && (eventType == domain.ShipArrivedEvent || eventType == domain.ShipDepartedEvent):
			var event domain.ShipEvent
			if json.Unmarshal(msg.Data(), &event) == nil {
				event.ShipID = id
				agg, ok := ships[id]
				if !ok {
					agg = &domain.ShipAggregate{}
					ships[id] = agg
				}
				agg.Apply(msg.Subject(), event)
				shipContexts[id] = event.Context
			}
		case subjectOK && aggregate == "container" && (eventType == domain.ContainerRegisteredEvent || eventType == domain.ContainerLoadedEvent || eventType == domain.ContainerUnloadedEvent):
			// Fold by the surrogate id (Phase 8.3) — the immutable aggregate
			// identity — so a container's events group under one key regardless
			// of its natural key.
			var event domain.ContainerEvent
			if json.Unmarshal(msg.Data(), &event) == nil {
				event.ID = id
				agg, ok := containers[id]
				if !ok {
					agg = &domain.ContainerAggregate{}
					containers[id] = agg
				}
				agg.Apply(msg.Subject(), event)
				containerContexts[id] = event.Context
			}
		}
		meta, _ := msg.Metadata()
		if meta != nil && meta.NumPending == 0 {
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
