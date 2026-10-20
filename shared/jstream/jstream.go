// Package jstream is the evt.* publish seam: the one place a domain event
// reaches JetStream, carrying its traceparent (BR-037) and, when the seam is
// opted in, its obs.pubsub.* observation (BR-045).
//
// Phase 43e merged two hand-maintained copies of this — shipping-service's
// internal/jstream and refdata-service's — that had drifted only in their
// stream-creation helper. It is the evt.* sibling of shared/natsnotify, which
// owns notify.*, and the two now share one construction idiom: the observer
// is nil unless WithObservation is given, so a Publisher built for a test or
// a one-off tool stays silent.
//
// Unlike natsnotify, this seam derives its observability tokens positionally
// from the subject (otelnats.Observer.ObservePublish). That is sound here and
// is not for evt.* what it was for notify.*: every evt.* subject in the tree
// is evt.{context}.{service}.{entity}.{id}.{action}, which the deriver reads
// correctly. notify.* had two shapes it could not.
package jstream

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// StreamOption configures the stream CreateStream provisions.
type StreamOption func(*jetstream.StreamConfig)

// WithMaxAge bounds retention. Used by a stream that is a replayable
// notification feed rather than a source of truth — refdata's REFDATA — where
// leaving retention unbounded would hoard pointers nobody can still act on.
// A stream that IS the source of truth (shipping's SHIPPING) passes nothing
// and stays unbounded on purpose: its history is the aggregate.
func WithMaxAge(d time.Duration) StreamOption {
	return func(c *jetstream.StreamConfig) { c.MaxAge = d }
}

// CreateStream creates or updates a stream with the supplied production-form
// subject filters, LimitsPolicy retention, and file storage. LimitsPolicy is
// required for replay: messages survive acknowledgement until limits evict
// them, which is what lets an aggregate rehydrate from its own log.
func CreateStream(ctx context.Context, js jetstream.JetStream, name string, subjects []string, opts ...StreamOption) (jetstream.Stream, error) {
	cfg := jetstream.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	stream, err := js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create stream %s: %w", name, err)
	}
	return stream, nil
}

// Publisher publishes domain events to JetStream subjects.
type Publisher struct {
	js jetstream.JetStream

	// otelObserver, when set by WithObservation, emits an obs.pubsub.* copy
	// of every publish (Phase 43a, BR-045 / BR-D45). Nil by default.
	otelObserver *otelnats.Observer

	// otel, when set by WithOTel, puts Publish on the OTel path (Phase 54,
	// decision 16). Nil publishes with no span.
	otel *otelnats.Provider
}

// WithOTel puts Publish on the OTel path: a PRODUCER span per publish, its
// traceparent on the message, and the obs.pubsub.* observation taking its
// IDs from that span (decision 5, decision 13). A nil provider is the same
// as not passing the option.
func WithOTel(prov *otelnats.Provider) Option {
	return func(p *Publisher) { p.otel = prov }
}

// Option configures a Publisher at construction.
type Option func(*Publisher)

// WithObservation turns on BR-045's publish-side observation, emitting one
// obs.pubsub.{context}.{service}.{entity}.{action} envelope per evt.* publish
// on nc.
//
// nc must be the connection JetStream was built from — the tenant's own, not
// the platform's. That is what places the envelope inside the tenant's
// account, which is the only reason PLATFORM's --local-subject import remap
// can attribute it to the right tenant (BR-D45, BR-AC34).
func WithObservation(nc *nats.Conn) Option {
	return func(p *Publisher) {
		p.otelObserver = otelnats.NewObserver(nc)
	}
}

// NewPublisher builds a Publisher on js. Observation is opt-in: pass
// WithObservation(nc) at the wiring point (rest/tenant.go, composition.go)
// and nowhere else.
func NewPublisher(js jetstream.JetStream, opts ...Option) *Publisher {
	p := &Publisher{js: js}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Publish is the only exported publish on the seam, and deliberately so: the
// plain and header-carrying primitives beneath it are reachable only from
// inside this package, which is what makes "every evt.* publish is traced and
// observed" a property of the type rather than a convention callers keep.
//
// With WithOTel it starts a PRODUCER span from ctx, injects its traceparent
// (BR-037), and observes with that span's IDs. Without it, the publish
// carries no traceparent (Phase 54d retired the legacy tracer fallback).
//
// The observation is emitted only after the domain publish succeeds — an
// event that never reached the stream did not happen, and must not appear on
// an operator's wire tap — and is fire-and-forget: its own failure is
// invisible here by design (BR-045, ADR-047 A7).
func (p *Publisher) Publish(ctx context.Context, subject string, data []byte) error {
	if p.otel == nil {
		if err := p.publish(ctx, subject, data); err != nil {
			return err
		}
		p.otelObserver.ObservePublish(ctx, subject, data)
		return nil
	}

	// otelnats.Produce owns the PRODUCER span and its traceparent (Phase 57,
	// BR-078); the nil check above stays, because it chooses the header-less
	// publish (decision 8).
	msg := &nats.Msg{Subject: subject, Data: data}
	return p.otel.Produce(ctx, msg, func(ctx context.Context) error {
		if err := p.publishMsg(ctx, subject, msg.Header, data); err != nil {
			return err
		}
		// Observed while the span is still open, so its IDs are the producer's.
		p.otelObserver.ObservePublish(ctx, subject, data)
		return nil
	})
}

func (p *Publisher) publish(ctx context.Context, subject string, data []byte) error {
	if _, err := p.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// publishMsg is jetstream.JetStream's header-carrying publish, unlike the
// plain-payload publish above (JetStream's own Publish convenience method
// takes no headers). Phase 28d: this is what lets an evt.* publish carry a
// traceparent.
func (p *Publisher) publishMsg(ctx context.Context, subject string, headers nats.Header, data []byte) error {
	msg := &nats.Msg{Subject: subject, Data: data, Header: headers}
	if _, err := p.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}
