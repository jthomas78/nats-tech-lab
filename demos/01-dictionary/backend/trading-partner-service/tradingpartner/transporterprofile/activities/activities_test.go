package activities_test

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	profileactivities "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/activities"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/workflow"
)

type deduplicatingPublisher struct {
	mu         sync.Mutex
	messageIDs []string
	events     map[string]profiledomain.Event
}

func (p *deduplicatingPublisher) AppendWorkflowEvent(_ context.Context, event profiledomain.Event, messageID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messageIDs = append(p.messageIDs, messageID)
	if p.events == nil {
		p.events = make(map[string]profiledomain.Event)
	}
	p.events[messageID] = event
	return nil
}

var _ = Describe("TransporterProfile activities", func() {
	Context("BR-TP24 workflow event publication is retry-safe", func() {
		It("uses tradingPartnerID:event:attemptNumber:step and deduplicates an activity retry", func() {
			publisher := &deduplicatingPublisher{}
			activity := profileactivities.NewProfileActivities(publisher, nil)
			input := profileworkflow.ProfileEventInput{
				Event: profiledomain.Event{
					Type:              profiledomain.DocumentApprovedEvent,
					Context:           "acme",
					TradingPartnerID:  "partner-1",
					AttemptNumber:     2,
					DocumentReference: "operating-license",
					OccurredAt:        time.Now().UTC(),
				},
				Step: 3,
			}

			Expect(activity.AppendProfileEvent(context.Background(), input)).To(Succeed())
			Expect(activity.AppendProfileEvent(context.Background(), input)).To(Succeed())
			Expect(publisher.messageIDs).To(HaveExactElements(
				"partner-1:document-approved:2:3",
				"partner-1:document-approved:2:3",
			))
			Expect(publisher.events).To(HaveLen(1))
		})
	})
})
