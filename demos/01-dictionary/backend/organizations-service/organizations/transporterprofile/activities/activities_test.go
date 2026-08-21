package activities_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	profileactivities "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/activities"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/workflow"
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
		It("uses organizationID:event:attemptNumber:step and deduplicates an activity retry", func() {
			publisher := &deduplicatingPublisher{}
			activity := profileactivities.NewProfileActivities(
				&tenantPublishers{sinks: map[string]*deduplicatingPublisher{"acme": publisher}}, nil)
			input := profileworkflow.ProfileEventInput{
				Event: profiledomain.Event{
					Type:              profiledomain.DocumentApprovedEvent,
					Context:           "acme",
					OrganizationID:    "partner-1",
					AttemptNumber:     2,
					DocumentReference: "operating-license",
					OccurredAt:        time.Now().UTC(),
				},
				Tenant: "acme",
				Step:   3,
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

// tenantPublishers is the resolver BR-TP58 requires in place of a single
// bound publisher: it hands back a different sink per tenant, so a test can
// prove an append landed on the tenant its input named and on no other.
type tenantPublishers struct {
	mu    sync.Mutex
	sinks map[string]*deduplicatingPublisher
}

func (r *tenantPublishers) ProfileEvents(tenant string) (profileactivities.WorkflowEventPublisher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sink, ok := r.sinks[tenant]
	if !ok {
		return nil, errTenantNotConnected
	}
	return sink, nil
}

var errTenantNotConnected = errors.New("tenant not connected")

var _ = Describe("BR-TP58 activities are tenant-resolved, never tenant-bound", func() {
	var (
		resolver *tenantPublishers
		acme     *deduplicatingPublisher
		globex   *deduplicatingPublisher
		activity *profileactivities.ProfileActivities
	)

	BeforeEach(func() {
		acme, globex = &deduplicatingPublisher{}, &deduplicatingPublisher{}
		resolver = &tenantPublishers{sinks: map[string]*deduplicatingPublisher{"acme": acme, "globex": globex}}
		activity = profileactivities.NewProfileActivities(resolver, nil)
	})

	input := func(tenant, contextKey string) profileworkflow.ProfileEventInput {
		return profileworkflow.ProfileEventInput{
			Tenant: tenant,
			Event: profiledomain.Event{
				Type: profiledomain.VettedEvent, Context: contextKey,
				OrganizationID: "org-1", AttemptNumber: 1, OccurredAt: time.Now().UTC(),
			},
			Step: 1,
		}
	}

	It("publishes over the connection its own input names, not another tenant's", func() {
		Expect(activity.AppendProfileEvent(context.Background(), input("acme", "acme"))).To(Succeed())

		Expect(acme.messageIDs).To(HaveLen(1))
		Expect(globex.messageIDs).To(BeEmpty(), "an append must never reach a tenant its input did not name")
	})

	It("routes two tenants' appends to their own connections when both are connected", func() {
		Expect(activity.AppendProfileEvent(context.Background(), input("acme", "acme"))).To(Succeed())
		Expect(activity.AppendProfileEvent(context.Background(), input("globex", "globex"))).To(Succeed())

		Expect(acme.messageIDs).To(HaveLen(1))
		Expect(globex.messageIDs).To(HaveLen(1))
		Expect(acme.events).NotTo(Equal(globex.events))
	})

	It("appends nothing for a tenant this service holds no credential for", func() {
		err := activity.AppendProfileEvent(context.Background(), input("unknown", "unknown"))

		Expect(err).To(MatchError(errTenantNotConnected))
		Expect(acme.messageIDs).To(BeEmpty())
		Expect(globex.messageIDs).To(BeEmpty(), "an unresolvable tenant must not fall back to another connection")
	})

	// Structural, deliberately: the regression this rule exists to prevent is
	// someone reintroducing a construction-time publisher, which would compile
	// and pass every behavioural test above while quietly making the task
	// queue's single constant unsafe again.
	It("holds no single bound publisher field", func() {
		value := reflect.ValueOf(*activity)
		publisherType := reflect.TypeOf((*profileactivities.WorkflowEventPublisher)(nil)).Elem()
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			Expect(field.Type.Implements(publisherType)).To(BeFalse(),
				"field %q is a bound WorkflowEventPublisher; BR-TP58 requires a per-tenant resolver", field.Name)
		}
	})
})
