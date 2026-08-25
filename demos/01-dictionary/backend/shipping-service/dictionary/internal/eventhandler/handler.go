// Package eventhandler contains the JetStream consumers that project
// shipping events into their read-side projections: ships (Postgres +
// write-through KV cache), containers, and meta.
package eventhandler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/notify"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// RegisterShips starts the ship projector: events update the canonical
// Postgres projection first, then eagerly write the result through to the KV
// cache so subsequent reads are served from KV without hitting Postgres.
//
// nc is optional (nil-safe, Phase 15b — moved here Phase 31 from the retired
// Shape A projector): when set, a fire-and-forget
// notify.{context}.shipping.ship.changed event carrying the full new
// ShipState is published after every successful KV write, letting a browser
// connected directly to this tenant's NATS account (Main-POC-Plan.md
// Phase 15d) react without KV watch or SSE. This projector is the ship
// entity's sole notify.* publisher (BR-024's "exactly one publisher per
// event" invariant). See notify_test.go for the payload contract this
// relies on.
func RegisterShips(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, nc *nats.Conn, repo domain.ShipRepository, log *slog.Logger) (jetstream.ConsumeContext, error) {
	n := notifier(nc, log)
	return register(ctx, js, "ship-projector", nc, log, func(msgCtx context.Context, subject string, event domain.ShipEvent) error {
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
		if _, err := kv.Put(msgCtx, event.Context, newKey, data); err != nil {
			return err
		}
		n.Publish(msgCtx, notify.Changed(event.Context, "ship"), data)
		if _, _, eventType, ok := domain.SubjectDetails(subject); ok {
			if rawData, err := json.Marshal(event); err == nil {
				n.Publish(msgCtx, notify.Raw(event.Context, "ship", eventType), rawData)
			}
		}
		return nil
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
	nc *nats.Conn,
	log *slog.Logger,
	project func(context.Context, string, domain.ShipEvent) error,
) (jetstream.ConsumeContext, error) {
	// The Consume callback below closes over ctx for the projector's entire
	// lifetime — every message it ever processes calls project(ctx, ...).
	// ctx is only meant to bound the CreateOrUpdateConsumer call right below;
	// stripping cancellation (keeping any values) means a caller whose ctx is
	// canceled shortly after registering (e.g. an HTTP request context) can't
	// leave every future event failing with "context canceled" and stuck
	// redelivering forever.
	msgCtx := context.WithoutCancel(ctx)
	// tracer is constructed once per registration (nc may be nil, same
	// nil-safe convention as publishNotify — natstrace.New(nil) is safe to
	// build; only a Span's finish() ever dereferences nc, and that call is
	// itself panic-recovered) rather than per message, mirroring how
	// browserrpc.Adapter builds one Tracer per connection (Phase 28b).
	tracer := natstrace.New(nc)
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
		aggregate, id, eventType, subjectOK := domain.SubjectDetails(msg.Subject())
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

		// One span per message (BR-037, Phase 28d) — continuing the
		// traceparent header the evt.* publish carried (PublishWithTrace) if
		// present, else a fresh root span. context/entity/action come from
		// the subject fields already parsed above, never re-parsed
		// positionally by StartFromHeaders itself: evt.*'s six-token shape
		// (evt.{context}.{service}.{entity}.{entity-id}.{event}) isn't the
		// rpc.*/api.* shape splitSubject assumes (see StartFromHeaders' doc
		// comment).
		sp := tracer.StartFromHeaders(msg.Headers(), msg.Subject(), msg.Data(), event.Context, "shipping", aggregate, eventType)
		sp.SetAttribute("entity_id", id)
		spanCtx := natstrace.ContextWithSpan(msgCtx, sp)

		if err := project(spanCtx, msg.Subject(), event); err != nil {
			log.Error("projection failed, will redeliver", "consumer", durable, "subject", msg.Subject(), "err", err)
			sp.Fail(err, nil, nil)
			_ = msg.Nak()
			return
		}
		sp.End(nil, nil)
		_ = msg.Ack()
	})
}

// notifier returns this package's notify.* seam.
//
// Phase 43d: publishNotify and publishRawNotify used to live here — two
// helpers that concatenated a subject, published it, log-and-swallowed the
// error, and then asked natstrace to parse the subject back into
// observability tokens. shared/natsnotify owns that sequence now, and
// internal/notify owns the subjects; what is left is the choice this service
// makes, which is that its projectors observe.
//
// Observation is enabled on the same connection the notify goes out on, which
// is what BR-D45 requires: the envelope has to be inside the publishing
// tenant's account for BR-AC34's import remap to attribute it correctly.
func notifier(nc *nats.Conn, log *slog.Logger) *natsnotify.Notifier {
	return natsnotify.New(nc, log, natsnotify.WithObservation(nc))
}
