// Package jstream wraps JetStream stream provisioning for the refdata
// service's change-event feed. Unlike the shipping backend's event store,
// this stream is a bounded notification channel, not a source of truth
// (Q6, Dictionary-Service-Plan.md) — hence the explicit MaxAge, unlike the
// shipping backend's deliberately unbounded SHIPPING stream.
package jstream

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// CreateChangeStream creates or updates a bounded LimitsPolicy stream: events
// are a replayable pointer/notification feed, not an event store, so maxAge
// bounds retention explicitly rather than leaving it unbounded.
func CreateChangeStream(ctx context.Context, js jetstream.JetStream, name string, subjects []string, maxAge time.Duration) (jetstream.Stream, error) {
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		MaxAge:    maxAge,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream %s: %w", name, err)
	}
	return stream, nil
}

// Publisher publishes change-event pointers to JetStream subjects.
type Publisher struct {
	js jetstream.JetStream
}

func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
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
func (p *Publisher) PublishWithTrace(ctx context.Context, sp *natstrace.Span, subject string, data []byte) error {
	if sp == nil {
		return p.Publish(ctx, subject, data)
	}
	return p.PublishMsg(ctx, subject, nats.Header{natstrace.TraceparentHeader: []string{sp.Traceparent()}}, data)
}
