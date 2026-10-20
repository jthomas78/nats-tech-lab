package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

type ProjectionWriter interface {
	Upsert(ctx context.Context, state profiledomain.State) error
}

type CacheWriter interface {
	Put(ctx context.Context, state profiledomain.State) error
}

// CertificateWriter projects the GIT certificates carried on the aggregate
// into organizations.compliance_documents — the second projection this
// consumer feeds, and the one ADR-050 Option A introduced. It is optional:
// a projector without one still maintains the profile projection, which is
// what every spec predating 39a exercises.
type CertificateWriter interface {
	UpsertCertificate(ctx context.Context, organizationID string, cert organizationdomain.ProjectedCertificate) error
}

type Projector struct {
	js           jetstream.JetStream
	projection   ProjectionWriter
	cache        CacheWriter
	certificates CertificateWriter
	otel         *otelnats.Provider
	account      string
	mu           sync.Mutex
	consume      jetstream.ConsumeContext
}

func NewProjector(js jetstream.JetStream, projection ProjectionWriter, cache CacheWriter) *Projector {
	return &Projector{js: js, projection: projection, cache: cache}
}

// WithCertificateWriter supplies the compliance_documents projection.
func (p *Projector) WithCertificateWriter(w CertificateWriter) *Projector {
	p.certificates = w
	return p
}

// WithOTel consumes each event under a CONSUMER span that continues the
// event's traceparent (Phase 54b-3b, BR-037). account is the tenant whose
// stream this projector reads: the receiving account (BR-069). The
// projection writes run under the span. A nil Provider keeps the projector
// untraced, as before.
func (p *Projector) WithOTel(prov *otelnats.Provider, account string) *Projector {
	p.otel = prov
	p.account = account
	return p
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
		// Decision 6: one ctx per delivery, carrying the CONSUMER span when
		// traced, so every write below is part of the event's trace. The span
		// is this processing attempt: Error when project returns an error
		// (BR-TP76), even when a redelivery later succeeds under a span of
		// its own. otelnats.Consume ends it on a panic too. Without a
		// Provider the delivery is untraced.
		_ = p.otel.Consume(context.Background(), msg.Subject(), msg.Headers(),
			otelnats.Trust{ReceivingAccount: p.account},
			func(ctx context.Context) error { return p.project(ctx, msg) })
	})
	if err != nil {
		return fmt.Errorf("consume transporter profile events: %w", err)
	}
	p.mu.Lock()
	p.consume = consume
	p.mu.Unlock()
	return nil
}

// project settles one delivery — Ack, Nak or Term — and returns why
// processing failed, or nil when it succeeded or the event was skipped on
// purpose.
func (p *Projector) project(ctx context.Context, msg jetstream.Msg) error {
	eventType, ok := profiledomain.EventType(msg.Subject())
	if !ok {
		_ = msg.Ack()
		return nil
	}
	// Only known state-transition events project. Audit-only branch events
	// and any unrecognised event type are acknowledged and skipped, so a
	// stray or future event can never overwrite the projection.
	if !profiledomain.ProjectsState(eventType) {
		_ = msg.Ack()
		return nil
	}
	var event profiledomain.Event
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		_ = msg.Term()
		return fmt.Errorf("decode transporter profile event: %w", err)
	}
	event.Type = eventType
	// Certificate events intentionally contain only changed fields. Rebuild
	// the aggregate for this one canonical projection write instead of
	// sneaking a full-state snapshot into the immutable event. This also
	// gives projection restart the same result as an event replay.
	agg, _, err := NewJetStreamEventStore(p.js).Hydrate(ctx, event.Context, event.OrganizationID)
	if err != nil {
		_ = msg.Nak()
		return fmt.Errorf("hydrate transporter profile: %w", err)
	}
	state := agg.State()
	if err := p.projection.Upsert(ctx, state); err != nil {
		_ = msg.Nak()
		return fmt.Errorf("upsert transporter profile projection: %w", err)
	}
	if err := p.cache.Put(ctx, state); err != nil {
		_ = msg.Nak()
		return fmt.Errorf("cache transporter profile: %w", err)
	}
	// The certificate projection is written from the same hydrated state,
	// so a redelivery re-writes identical rows rather than applying a
	// delta twice. Every certificate is written, not just the one this
	// event names: a supersede-on-approval moves several at once
	// (BR-TP69), and writing only the named one would leave the others
	// showing cover they no longer carry.
	if p.certificates != nil {
		for _, certificate := range state.Certificates {
			if err := p.certificates.UpsertCertificate(ctx, state.ID, projectedCertificate(certificate)); err != nil {
				_ = msg.Nak()
				return fmt.Errorf("upsert certificate %s: %w", certificate.ID, err)
			}
		}
	}
	_ = msg.Ack()
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

// projectedCertificate narrows the aggregate's certificate to exactly what
// the projection may write. The conversion is deliberately lossy in one
// direction only — there are no contact fields on either side of it.
func projectedCertificate(c profiledomain.Certificate) organizationdomain.ProjectedCertificate {
	return organizationdomain.ProjectedCertificate{
		ID: c.ID, Status: c.Status, DocumentName: c.DocumentName,
		GoodsTypes:    append([]string(nil), c.GoodsTypes...),
		CoverageCents: c.CoverageCents, ExpiresAt: c.ExpiresAt,
		InsurerName: c.InsurerName, File: c.File,
	}
}
