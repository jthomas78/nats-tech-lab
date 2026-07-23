// Package eventhandler contains the JetStream consumers that project shipping
// events into the two read-side shapes. Each shape has its own durable consumer
// so the projections replay and advance independently.
package eventhandler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// RegisterShapeA starts the Shape A projector: ship events are projected
// directly into the context-scoped KV bucket, which IS the read model.
// On each event the projector reads the current KV state, applies the event
// delta via ShipAggregate, and writes the new ShipState back.
func RegisterShapeA(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, log *slog.Logger) (jetstream.ConsumeContext, error) {
	return register(ctx, js, "ship-shape-a", log, func(msgCtx context.Context, subject string, event domain.ShipEvent) error {
		oldKey := shipKVKey(subject, event)
		agg := currentAgg(msgCtx, kv, event.Context, oldKey)
		agg.Apply(subject, event)
		newKey := "ship." + agg.ShipID
		data, err := json.Marshal(agg.State(event.Context))
		if err != nil {
			return err
		}
		if oldKey != newKey {
			if err := kv.Delete(msgCtx, event.Context, oldKey); err != nil {
				return err
			}
		}
		_, err = kv.Put(msgCtx, event.Context, newKey, data)
		return err
	})
}

// RegisterShapeB starts the Shape B projector: events update the canonical
// Postgres projection first, then eagerly write the result through to the KV
// cache so subsequent reads are served from KV without hitting Postgres.
func RegisterShapeB(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, repo domain.ShipRepository, log *slog.Logger) (jetstream.ConsumeContext, error) {
	return register(ctx, js, "ship-shape-b", log, func(msgCtx context.Context, subject string, event domain.ShipEvent) error {
		oldKey := shipKVKey(subject, event)
		agg := currentAgg(msgCtx, kv, event.Context, oldKey)
		agg.Apply(subject, event)
		state := agg.State(event.Context)

		persisted, err := repo.Upsert(msgCtx, state)
		if err != nil {
			return err
		}
		newKey := "ship." + persisted.ShipID
		data, err := json.Marshal(persisted)
		if err != nil {
			return err
		}
		if oldKey != newKey {
			if err := kv.Delete(msgCtx, event.Context, oldKey); err != nil {
				return err
			}
		}
		_, err = kv.Put(msgCtx, event.Context, newKey, data)
		return err
	})
}

// shipKVKey returns the KV key holding this ship's *pre-event* state. For a
// .corrected event that's the key under the previous name (event.ShipID is
// already the NEW name in the payload); for every other event type the
// ship's name hasn't changed, so it's just today's key.
func shipKVKey(subject string, event domain.ShipEvent) string {
	_, _, eventType, _ := domain.SubjectDetails(subject)
	if eventType == domain.ShipIDCorrectedEvent && event.PreviousShipID != "" {
		return "ship." + event.PreviousShipID
	}
	return "ship." + event.ShipID
}

// currentAgg reads the ship's current KV state at key and loads it into a
// ShipAggregate so the projector can apply a single-event delta efficiently
// without replaying the full JetStream history.
func currentAgg(ctx context.Context, kv *kvstore.Store, shipContext, key string) *domain.ShipAggregate {
	agg := &domain.ShipAggregate{}
	raw, _, err := kv.Get(ctx, shipContext, key)
	if err == nil {
		var existing domain.ShipState
		if json.Unmarshal(raw, &existing) == nil {
			agg.FromState(existing)
		}
	}
	return agg
}

func register(
	ctx context.Context,
	js jetstream.JetStream,
	durable string,
	log *slog.Logger,
	project func(context.Context, string, domain.ShipEvent) error,
) (jetstream.ConsumeContext, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, domain.StreamName, jetstream.ConsumerConfig{
		Durable: durable,
		// Ship projectors only consume ship movement events; container.*
		// events are handled by the container projector (container_handler.go).
		FilterSubject: domain.SubjectShipWildcard,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, err
	}
	return cons.Consume(func(msg jetstream.Msg) {
		aggregate, id, _, subjectOK := domain.SubjectDetails(msg.Subject())
		var event domain.ShipEvent
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			log.Error("drop malformed event", "consumer", durable, "subject", msg.Subject(), "err", err)
			_ = msg.Ack()
			return
		}
		// Skip messages from a previous domain version (e.g. legacy entry.* events)
		// that unmarshal without error but produce an empty shipID. Ack them as
		// poison messages so they are not redelivered indefinitely.
		if !subjectOK || aggregate != "ship" || event.ShipID == "" {
			log.Warn("skip legacy event (no shipID)", "consumer", durable, "subject", msg.Subject())
			_ = msg.Ack()
			return
		}
		event.ID = id
		if err := project(ctx, msg.Subject(), event); err != nil {
			log.Error("projection failed, will redeliver", "consumer", durable, "subject", msg.Subject(), "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
}
