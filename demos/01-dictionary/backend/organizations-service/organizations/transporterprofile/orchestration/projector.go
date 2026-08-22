package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
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
		// The certificate projection is written from the same hydrated state,
		// so a redelivery re-writes identical rows rather than applying a
		// delta twice. Every certificate is written, not just the one this
		// event names: a supersede-on-approval moves several at once
		// (BR-TP69), and writing only the named one would leave the others
		// showing cover they no longer carry.
		if p.certificates != nil {
			failed := false
			for _, certificate := range state.Certificates {
				if err := p.certificates.UpsertCertificate(context.Background(), state.ID, projectedCertificate(certificate)); err != nil {
					failed = true
					break
				}
			}
			if failed {
				_ = msg.Nak()
				return
			}
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

// projectedCertificate narrows the aggregate's certificate to exactly what
// the projection may write. The conversion is deliberately lossy in one
// direction only — there are no contact fields on either side of it.
func projectedCertificate(c profiledomain.Certificate) organizationdomain.ProjectedCertificate {
	return organizationdomain.ProjectedCertificate{
		ID: c.ID, Status: c.Status, Reference: c.Reference,
		GoodsTypes:    append([]string(nil), c.GoodsTypes...),
		CoverageCents: c.CoverageCents, ExpiresAt: c.ExpiresAt,
		InsurerName: c.InsurerName, File: c.File,
	}
}
