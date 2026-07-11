// Package jstream wraps JetStream stream provisioning with the lab's
// conventions: LimitsPolicy retention (never InterestPolicy) so events are
// kept and can be replayed by any number of consumers.
package jstream

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
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
