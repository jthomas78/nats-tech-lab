package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
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
		state := profiledomain.State{
			Context: event.Context, ID: event.TradingPartnerID, Status: event.Status,
			AttemptNumber: event.AttemptNumber, FleetAvailabilityGate: event.FleetAvailabilityGate,
			GitVerified:     event.GitVerified,
			DocumentReviews: event.DocumentReviews,
			UpdatedAt:       event.OccurredAt,
		}
		if eventType == profiledomain.CreatedEvent {
			state.Status = profiledomain.StatusAwaitingDocumentation
		}
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
