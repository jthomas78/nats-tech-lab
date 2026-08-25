package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

func EnsureStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       profiledomain.StreamName,
		Subjects:   []string{profiledomain.SubjectWildcard},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		Duplicates: 10 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("create %s stream: %w", profiledomain.StreamName, err)
	}
	return nil
}

// AppendWorkflowEvent is the Activity-facing append boundary. The stable
// message ID makes a Temporal retry return the original PubAck rather than
// create another domain transition.
func (s *JetStreamEventStore) AppendWorkflowEvent(ctx context.Context, event profiledomain.Event, messageID string) error {
	_, expected, err := s.Hydrate(ctx, event.Context, event.OrganizationID)
	if err != nil {
		return err
	}
	_, err = s.append(ctx, event.Context, event.OrganizationID, event, expected, messageID)
	return err
}

type JetStreamEventStore struct {
	js jetstream.JetStream

	// observer, when set by WithObservation, emits an obs.pubsub.* copy of
	// every event appended through this store (Phase 43a, BR-TP75). Nil by
	// default, so a store built for a test or for the projector's own
	// hydration path stays silent.
	observer *natstrace.Tracer
}

// Option configures a JetStreamEventStore at construction.
type Option func(*JetStreamEventStore)

// WithObservation turns on BR-045's publish-side observation for this store,
// emitting one
// obs.pubsub.{context}.organizations.transporter-profile.{eventType} envelope
// per appended event. Wired once per tenant runtime (transporterprofile.Start),
// on the same connection JetStream was built from, so the observation lands
// inside that tenant's account and PLATFORM's import remap can name it.
//
// The hook sits on append, not on the workflow activities that call it: every
// transporter-profile event in this service reaches JetStream through that one
// function, so coverage is structural — a new event type is observed by
// construction rather than by someone remembering to wire it.
//
// This store keeps its own append rather than delegating to shared/jstream
// (Phase 43e): the optimistic-concurrency headers and the ErrSequenceConflict
// mapping below are the whole point of it, and folding them into the shared
// seam would produce a generic that served neither caller. What it does share
// is the construction idiom — observer nil unless asked for.
func WithObservation(nc *nats.Conn) Option {
	return func(s *JetStreamEventStore) { s.observer = natstrace.New(nc) }
}

func NewJetStreamEventStore(js jetstream.JetStream, opts ...Option) *JetStreamEventStore {
	s := &JetStreamEventStore{js: js}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *JetStreamEventStore) Hydrate(ctx context.Context, contextKey, organizationID string) (*profiledomain.TransporterProfile, uint64, error) {
	consumer, err := s.js.OrderedConsumer(ctx, profiledomain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{profiledomain.InstanceSubject(contextKey, organizationID)},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("hydrate transporter profile: %w", err)
	}
	defer dropHydrationConsumer(s.js, consumer)

	agg := &profiledomain.TransporterProfile{}
	var observed uint64
	if consumer.CachedInfo().NumPending == 0 {
		return agg, observed, nil
	}
	msgs, err := consumer.Messages()
	if err != nil {
		return nil, 0, fmt.Errorf("hydrate transporter profile messages: %w", err)
	}
	defer msgs.Stop()
	for {
		msg, nextErr := msgs.Next()
		if nextErr != nil {
			return nil, 0, fmt.Errorf("hydrate transporter profile next: %w", nextErr)
		}
		metadata, metadataErr := msg.Metadata()
		if metadataErr != nil {
			return nil, 0, fmt.Errorf("hydrate transporter profile metadata: %w", metadataErr)
		}
		observed = metadata.Sequence.Stream
		if eventType, ok := profiledomain.EventType(msg.Subject()); ok {
			var event profiledomain.Event
			if json.Unmarshal(msg.Data(), &event) == nil {
				event.Type = eventType
				agg.Apply(event)
			}
		}
		if metadata.NumPending == 0 {
			break
		}
	}
	return agg, observed, nil
}

func dropHydrationConsumer(js jetstream.JetStream, consumer jetstream.Consumer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = js.DeleteConsumer(ctx, profiledomain.StreamName, consumer.CachedInfo().Name)
}

func (s *JetStreamEventStore) Append(ctx context.Context, contextKey, organizationID string, event profiledomain.Event, expectedSequence uint64) (uint64, error) {
	return s.append(ctx, contextKey, organizationID, event, expectedSequence, "")
}

func (s *JetStreamEventStore) append(ctx context.Context, contextKey, organizationID string, event profiledomain.Event, expectedSequence uint64, messageID string) (uint64, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return 0, fmt.Errorf("marshal transporter profile event: %w", err)
	}
	msg := &nats.Msg{
		Subject: profiledomain.Subject(contextKey, organizationID, event.Type),
		Data:    data,
		Header: nats.Header{
			jetstream.ExpectedLastSubjSeqHeader:     []string{strconv.FormatUint(expectedSequence, 10)},
			jetstream.ExpectedLastSubjSeqSubjHeader: []string{profiledomain.InstanceSubject(contextKey, organizationID)},
		},
	}
	if messageID != "" {
		msg.Header.Set(nats.MsgIdHdr, messageID)
	}
	// BR-037: every evt.* publish carries a traceparent derived from the span
	// that caused it. This seam went without one from Phase 28 until Phase
	// 43e — the rule existed, nothing enforced it here, and nothing on the
	// consume side missed it loudly enough to notice. Nil-safe: no span
	// reachable on ctx means no header, exactly as shared/jstream behaves.
	sp := natstrace.SpanFromContext(ctx)
	if tp := sp.Traceparent(); tp != "" {
		msg.Header.Set(natstrace.TraceparentHeader, tp)
	}
	ack, err := s.js.PublishMsg(ctx, msg)
	if err != nil {
		var jsErr jetstream.JetStreamError
		if errors.As(err, &jsErr) && jsErr.APIError() != nil && jsErr.APIError().ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence {
			return 0, ErrSequenceConflict
		}
		return 0, fmt.Errorf("publish transporter profile event: %w", err)
	}
	// Only after the PubAck: a rejected append (BR-TP20's optimistic-
	// concurrency guard, above) never reached the stream, so it must not
	// appear on an operator's wire tap. The emit itself is fire-and-forget —
	// it drops its own error and cannot change this function's result or
	// meaningfully delay it (ADR-047 A7).
	s.observer.ObservePublish(sp, msg.Subject, data)
	return ack.Sequence, nil
}
