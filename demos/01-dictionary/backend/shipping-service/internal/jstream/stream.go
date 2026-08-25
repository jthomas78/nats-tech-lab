// Package jstream wraps JetStream stream provisioning with the lab's
// conventions: LimitsPolicy retention (never InterestPolicy) so events are
// kept and can be replayed by any number of consumers.
package jstream

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// CreateStream creates or updates a stream with the supplied production-form
// subject filters, LimitsPolicy retention, and file storage. LimitsPolicy is
// required for replay: messages survive acknowledgement until limits evict them.
func CreateStream(ctx context.Context, js jetstream.JetStream, name string, subjects []string) (jetstream.Stream, error) {
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream %s: %w", name, err)
	}
	return stream, nil
}

// Publisher publishes events to JetStream subjects.
type Publisher struct {
	js jetstream.JetStream

	// observer, when set by EnableObservation, emits an obs.pubsub.* copy of
	// every evt.* publish that goes through PublishWithTrace (Phase 43a,
	// BR-045). Nil by default: a Publisher built for a test or a one-off tool
	// stays silent, and only the tenant wiring in rest/tenant.go opts in.
	observer *natstrace.Tracer
}

func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// EnableObservation turns on BR-045's publish-side observation for this
// Publisher, emitting one obs.pubsub.{context}.{service}.{entity}.{action}
// envelope per evt.* publish on nc. Mirrors kvstore.Store.EnableNotify's
// opt-in shape, and is wired at the same point in rest/tenant.go.
//
// The hook lives in PublishWithTrace rather than in Publish/PublishMsg
// (ADR-047 amendment A3): those two are generic primitives that also carry
// non-domain traffic, whereas PublishWithTrace is the seam every evt.* publish
// in this service already goes through. That makes coverage structural — a
// new evt.* publisher is observed without anyone remembering to wire it —
// without observing plumbing that is not a domain event.
func (p *Publisher) EnableObservation(nc *nats.Conn) {
	p.observer = natstrace.New(nc)
}

func (p *Publisher) Publish(ctx context.Context, subject string, data []byte) error {
	if _, err := p.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// PublishMsg publishes data to subject carrying headers — jetstream.JetStream's
// header-carrying publish, unlike the plain-payload Publish above (JetStream's
// own Publish convenience method takes no headers). Phase 28d: this is what
// lets an evt.* publish carry a traceparent.
func (p *Publisher) PublishMsg(ctx context.Context, subject string, headers nats.Header, data []byte) error {
	msg := &nats.Msg{Subject: subject, Data: data, Header: headers}
	if _, err := p.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// PublishWithTrace publishes data to subject the same way Publish does, plus
// a traceparent header derived from sp (BR-037) — nil-safe: a nil sp (no
// span reachable at the call site, e.g. ctx carried none) publishes with no
// traceparent header at all, identical to a plain Publish.
// Phase 43a: it is also the evt.* observation seam — see EnableObservation.
// The observation is emitted only after the domain publish succeeds (an event
// that never reached the stream did not happen, and must not appear on the
// operator's wire tap), and is fire-and-forget: its own failure is invisible
// here by design (BR-045).
func (p *Publisher) PublishWithTrace(ctx context.Context, sp *natstrace.Span, subject string, data []byte) error {
	var err error
	if sp == nil {
		err = p.Publish(ctx, subject, data)
	} else {
		err = p.PublishMsg(ctx, subject, nats.Header{natstrace.TraceparentHeader: []string{sp.Traceparent()}}, data)
	}
	if err != nil {
		return err
	}
	p.observer.ObservePublish(sp, subject, data)
	return nil
}
