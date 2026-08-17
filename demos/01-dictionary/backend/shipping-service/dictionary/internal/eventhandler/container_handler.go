package eventhandler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/natstrace"
)

// RegisterContainers starts the container projector: container events update
// the canonical Postgres projection first, then eagerly write through to the
// tenant-scoped container KV bucket under the event context's key prefix (the
// read model served by the terminal queries). One durable consumer, positioned
// independently of the ship projectors.
//
// nc is optional (nil-safe, Phase 15b) — see RegisterShips's doc comment;
// after every successful KV write this fire-and-forget publishes
// notify.{context}.shipping.container.changed carrying the full persisted
// ContainerState.
func RegisterContainers(
	ctx context.Context,
	js jetstream.JetStream,
	kv *kvstore.Store,
	nc *nats.Conn,
	repo domain.ContainerRepository,
	log *slog.Logger,
) (jetstream.ConsumeContext, error) {
	// See handler.go's register() for why: the Consume callback below closes
	// over this context for the projector's entire lifetime, so it must not
	// be tied to whatever short-lived context the caller used to register it
	// (e.g. an HTTP request context, Phase 13b's tenant switch).
	msgCtx := context.WithoutCancel(ctx)
	// See handler.go's register() for the same nil-safe-nc construction
	// rationale.
	tracer := natstrace.New(nc)
	cons, err := js.CreateOrUpdateConsumer(ctx, domain.StreamName, jetstream.ConsumerConfig{
		Durable:       "container-projector",
		FilterSubject: domain.SubjectContainerWildcard,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, err
	}
	return cons.Consume(func(msg jetstream.Msg) {
		aggregate, id, eventType, subjectOK := domain.SubjectDetails(msg.Subject())
		var event domain.ContainerEvent
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			log.Error("drop malformed container event", "subject", msg.Subject(), "err", err)
			_ = msg.Ack()
			return
		}
		if !subjectOK || aggregate != "container" || event.ContainerID == "" {
			log.Warn("skip container event without containerID", "subject", msg.Subject())
			_ = msg.Ack()
			return
		}
		event.ID = id

		// One span per message (BR-037, Phase 28d) — see handler.go's
		// register() for the full rationale on why context/entity/action
		// come from the already-parsed subject fields rather than being
		// re-derived by StartFromHeaders itself.
		sp := tracer.StartFromHeaders(msg.Headers(), msg.Subject(), msg.Data(), event.Context, "shipping", aggregate, eventType)
		sp.SetAttribute("entity_id", id)
		spanCtx := natstrace.ContextWithSpan(msgCtx, sp)

		agg := currentContainerAgg(spanCtx, kv, event)
		agg.Apply(msg.Subject(), event)
		state := agg.State(event.Context)

		persisted, err := repo.Upsert(spanCtx, state)
		if err != nil {
			log.Error("container projection failed, will redeliver", "subject", msg.Subject(), "err", err)
			sp.Fail(err, nil, nil)
			_ = msg.Nak()
			return
		}
		data, err := json.Marshal(persisted)
		if err != nil {
			log.Error("marshal container state", "err", err)
			sp.Fail(err, nil, nil)
			_ = msg.Nak()
			return
		}
		if _, err := kv.Put(spanCtx, event.Context, state.KVKey(), data); err != nil {
			log.Error("container kv write failed, will redeliver", "subject", msg.Subject(), "err", err)
			sp.Fail(err, nil, nil)
			_ = msg.Nak()
			return
		}
		publishNotify(nc, log, event.Context, "container", data, sp)
		if rawData, err := json.Marshal(event); err == nil {
			publishRawNotify(nc, log, event.Context, "container", eventType, rawData, sp)
		}
		sp.End(nil, nil)
		_ = msg.Ack()
	})
}

// currentContainerAgg reads the current KV state for the container and loads
// it into a ContainerAggregate so the projector can apply a single-event
// delta without replaying the full JetStream history.
func currentContainerAgg(ctx context.Context, kv *kvstore.Store, event domain.ContainerEvent) *domain.ContainerAggregate {
	agg := &domain.ContainerAggregate{ID: event.ID, ContainerID: event.ContainerID}
	raw, _, err := kv.Get(ctx, event.Context, "container."+event.ContainerID)
	if err == nil {
		var existing domain.ContainerState
		if json.Unmarshal(raw, &existing) == nil {
			agg.FromState(existing)
		}
	}
	return agg
}
