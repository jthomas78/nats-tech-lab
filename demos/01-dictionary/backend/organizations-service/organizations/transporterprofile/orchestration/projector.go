package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

type ProjectionWriter interface {
	Upsert(ctx context.Context, state profiledomain.State) error
}

type CacheWriter interface {
	Put(ctx context.Context, state profiledomain.State) error
}

type Projector struct {
	js         jetstream.JetStream
	projection ProjectionWriter
	cache      CacheWriter
	mu         sync.Mutex
	consume    jetstream.ConsumeContext
}

func NewProjector(js jetstream.JetStream, projection ProjectionWriter, cache CacheWriter) *Projector {
	return &Projector{js: js, projection: projection, cache: cache}
}

func (p *Projector) Start(ctx context.Context) error {
	consumer, err := p.js.CreateOrUpdateConsumer(ctx, profiledomain.StreamName, jetstream.ConsumerConfig{
		Name:          "transporter-profile-projector",
		Durable:       "transporter-profile-projector",
		FilterSubject: profiledomain.SubjectWildcard,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("create transporter profile projector: %w", err)
	}
	consume, err := consumer.Consume(func(msg jetstream.Msg) {
		eventType, ok := profiledomain.EventType(msg.Subject())
		if !ok {
			_ = msg.Ack()
			return
		}
		// Only known state-transition events project. Audit-only branch events
		// and any unrecognised event type are acknowledged and skipped, so a
		// stray or future event can never overwrite the projection.
		if !profiledomain.ProjectsState(eventType) {
			_ = msg.Ack()
			return
		}
		var event profiledomain.Event
		if json.Unmarshal(msg.Data(), &event) != nil {
			_ = msg.Term()
			return
		}
		event.Type = eventType
		// Certificate events intentionally contain only changed fields. Rebuild
		// the aggregate for this one canonical projection write instead of
		// sneaking a full-state snapshot into the immutable event. This also
		// gives projection restart the same result as an event replay.
		agg, _, err := NewJetStreamEventStore(p.js).Hydrate(context.Background(), event.Context, event.OrganizationID)
		if err != nil {
			_ = msg.Nak()
			return
		}
		state := agg.State()
		if err := p.projection.Upsert(context.Background(), state); err != nil {
			_ = msg.Nak()
			return
		}
		if err := p.cache.Put(context.Background(), state); err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("consume transporter profile events: %w", err)
	}
	p.mu.Lock()
	p.consume = consume
	p.mu.Unlock()
	return nil
}

func (p *Projector) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consume != nil {
		p.consume.Stop()
		p.consume = nil
	}
}
