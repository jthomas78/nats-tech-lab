// Package eventhandler contains the JetStream consumers that project shipping
// events into the two read-side shapes. Each shape has its own durable consumer
// so the projections replay and advance independently.
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

// RegisterShapeA starts the Shape A projector: ship events are projected
// directly into the tenant-scoped KV bucket under its context key prefix,
// which IS the read model.
// On each event the projector reads the current KV state, applies the event
// delta via ShipAggregate, and writes the new ShipState back.
//
// nc is optional (nil-safe, Phase 15b): when set, a fire-and-forget
// notify.{context}.shipping.ship.changed event carrying the full new
// ShipState is published after every successful KV write, letting a browser
// connected directly to this tenant's NATS account (Main-POC-Plan.md
// Phase 15d) react without KV watch or SSE. Only Shape A publishes this —
// Shape B projects the same events into its own cache but the browser
// doesn't distinguish shapes, so a second notify per event would be a
// duplicate, not new information. See notify_test.go for the payload
// contract this relies on.
func RegisterShapeA(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, nc *nats.Conn, log *slog.Logger) (jetstream.ConsumeContext, error) {
	return register(ctx, js, "ship-shape-a", nc, log, func(msgCtx context.Context, subject string, event domain.ShipEvent) error {
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
		if _, err := kv.Put(msgCtx, event.Context, newKey, data); err != nil {
			return err
		}
		sp := natstrace.SpanFromContext(msgCtx)
		publishNotify(nc, log, event.Context, "ship", data, sp)
		if _, _, eventType, ok := domain.SubjectDetails(subject); ok {
			if rawData, err := json.Marshal(event); err == nil {
				publishRawNotify(nc, log, event.Context, "ship", eventType, rawData, sp)
			}
		}
		return nil
	})
}

// RegisterShapeB starts the Shape B projector: events update the canonical
// Postgres projection first, then eagerly write the result through to the KV
// cache so subsequent reads are served from KV without hitting Postgres.
//
// nc is nil-safe (Phase 28d) — threaded through only so register()'s shared
// Consume callback can construct a natstrace.Tracer for this projector's
// per-message spans too; Shape B has no notify.* publish of its own (only
// Shape A does, see RegisterShapeA's doc comment), so nc is otherwise unused
// here.
func RegisterShapeB(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, nc *nats.Conn, repo domain.ShipRepository, log *slog.Logger) (jetstream.ConsumeContext, error) {
	return register(ctx, js, "ship-shape-b", nc, log, func(msgCtx context.Context, subject string, event domain.ShipEvent) error {
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

// publishNotify fire-and-forget publishes a notify.{kvContext}.shipping.
// {entity}.changed event (Phase 15b) carrying payload (the full projected
// entity, already-marshaled JSON) — plain core NATS pub/sub, no JetStream
// retention: a missed notification during a brief browser disconnect is
// covered by the bootstrap api.*.shipping.{entity}.list.v1 call on
// reconnect (Main-POC-Plan.md Phase 15d), so no replay mechanism is needed
// here, unlike refdata-service's obs.rpc.*/RPCTRACE (BR-D29).
//
// nc is nil-safe: every Register* caller in this package already passes nil
// in contexts where no tenant connection is relevant (e.g. some tests), and
// this must not fail or panic in that case — same nil-safe-Deps convention
// used throughout this repo. A publish error is logged, never returned:
// notify.* is a best-effort convenience for reactive UIs, not a correctness
// requirement the projector's own success depends on.
//
// sp is the per-message span (Phase 28d, BR-037) that caused this notify —
// nil-safe both ways: a nil sp (or sp.Traceparent() == "", which is the same
// nil-safe no-op) publishes with no traceparent header at all, identical to
// the pre-28d behavior. When present, it lets the trace waterfall show the
// async tail continuing past the KV write the caller already made.
func publishNotify(nc *nats.Conn, log *slog.Logger, kvContext, entity string, payload []byte, sp *natstrace.Span) {
	if nc == nil {
		return
	}
	subject := "notify." + kvContext + ".shipping." + entity + ".changed"
	msg := &nats.Msg{Subject: subject, Data: payload}
	if tp := sp.Traceparent(); tp != "" {
		msg.Header = nats.Header{natstrace.TraceparentHeader: []string{tp}}
	}
	if err := nc.PublishMsg(msg); err != nil && log != nil {
		log.Warn("notify publish failed", "subject", subject, "err", err)
	}
}

// publishRawNotify fire-and-forget publishes
// notify.{kvContext}.shipping.raw.{entity}.{event} (Phase 23) carrying the
// raw domain event as received off the SHIPPING stream — distinct from
// publishNotify's "current projected state" payload: the Admin UI's
// JetStream watch panel wants the actual verb (arrived/departed/loaded/...),
// not a projected snapshot, replacing the per-SSE-connection OrderedConsumer
// dictionary/internal/rest/sse.go's watchJetStream used to create. Same
// nil-safe, best-effort convention as publishNotify. sp is the same
// per-message span publishNotify accepts, and behaves identically
// (nil-safe, Phase 28d).
func publishRawNotify(nc *nats.Conn, log *slog.Logger, kvContext, entity, event string, payload []byte, sp *natstrace.Span) {
	if nc == nil {
		return
	}
	subject := "notify." + kvContext + ".shipping.raw." + entity + "." + event
	msg := &nats.Msg{Subject: subject, Data: payload}
	if tp := sp.Traceparent(); tp != "" {
		msg.Header = nats.Header{natstrace.TraceparentHeader: []string{tp}}
	}
	if err := nc.PublishMsg(msg); err != nil && log != nil {
		log.Warn("raw notify publish failed", "subject", subject, "err", err)
	}
}
