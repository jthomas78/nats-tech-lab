// Package eventhandler contains the JetStream consumers that project
// dictionary events into the two read-side shapes. Each shape has its own
// durable consumer so the projections replay and advance independently.
package eventhandler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

// RegisterShapeA starts the Shape A projector: events are written straight
// into the context-scoped KV bucket, which IS the read model.
func RegisterShapeA(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, log *slog.Logger) (jetstream.ConsumeContext, error) {
	return register(ctx, js, "dict-shape-a", log, func(msgCtx context.Context, event domain.EntryEvent) error {
		entry := event.Entry
		key := entry.KVKey()

		// Preserve the original createdAt when an update overwrites the key.
		if prev, _, err := kv.Get(msgCtx, entry.Context, key); err == nil {
			var existing domain.DictionaryEntry
			if json.Unmarshal(prev, &existing) == nil && !existing.CreatedAt.IsZero() {
				entry.CreatedAt = existing.CreatedAt
			}
		}

		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		_, err = kv.Put(msgCtx, entry.Context, key, data)
		return err
	})
}

// RegisterShapeB starts the Shape B projector: events update the canonical
// Postgres projection first, then refresh the KV cache (watch-based
// invalidation by overwrite).
func RegisterShapeB(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, repo domain.Repository, log *slog.Logger) (jetstream.ConsumeContext, error) {
	return register(ctx, js, "dict-shape-b", log, func(msgCtx context.Context, event domain.EntryEvent) error {
		persisted, err := repo.Upsert(msgCtx, event.Entry)
		if err != nil {
			return err
		}
		data, err := json.Marshal(persisted)
		if err != nil {
			return err
		}
		_, err = kv.Put(msgCtx, persisted.Context, persisted.KVKey(), data)
		return err
	})
}

func register(ctx context.Context, js jetstream.JetStream, durable string, log *slog.Logger, project func(context.Context, domain.EntryEvent) error) (jetstream.ConsumeContext, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, domain.StreamName, jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: domain.SubjectWildcard,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, err
	}
	return cons.Consume(func(msg jetstream.Msg) {
		var event domain.EntryEvent
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			log.Error("drop malformed event", "consumer", durable, "subject", msg.Subject(), "err", err)
			_ = msg.Ack() // poison message: ack so it does not redeliver forever
			return
		}
		if err := project(ctx, event); err != nil {
			log.Error("projection failed, will redeliver", "consumer", durable, "subject", msg.Subject(), "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
}
