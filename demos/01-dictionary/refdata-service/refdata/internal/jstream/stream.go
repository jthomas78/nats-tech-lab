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

	"github.com/nats-io/nats.go/jetstream"
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
