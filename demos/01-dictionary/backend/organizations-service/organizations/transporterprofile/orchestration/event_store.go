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

type JetStreamEventStore struct{ js jetstream.JetStream }

func NewJetStreamEventStore(js jetstream.JetStream) *JetStreamEventStore {
	return &JetStreamEventStore{js: js}
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
	ack, err := s.js.PublishMsg(ctx, msg)
	if err != nil {
		var jsErr jetstream.JetStreamError
		if errors.As(err, &jsErr) && jsErr.APIError() != nil && jsErr.APIError().ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence {
			return 0, ErrSequenceConflict
		}
		return 0, fmt.Errorf("publish transporter profile event: %w", err)
	}
	return ack.Sequence, nil
}
