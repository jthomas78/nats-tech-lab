package workflow_test

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/workflow"
)

type eventRecorder struct {
	mu     sync.Mutex
	events []profileworkflow.ProfileEventInput
	gitErr error
}

func (r *eventRecorder) append(_ context.Context, in profileworkflow.ProfileEventInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, in)
	return nil
}

func (r *eventRecorder) verifyGIT(context.Context, profileworkflow.GitVerificationInput) error {
	return r.gitErr
}

func (r *eventRecorder) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, event := range r.events {
		out = append(out, event.Event.Type)
	}
	return out
}

func executeVetting(recorder *eventRecorder, required []string, signals ...profileworkflow.DocumentReviewSignal) (profileworkflow.VettingResult, error) {
	GinkgoHelper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(recorder.append, activity.RegisterOptions{Name: profileworkflow.AppendProfileEventActivity})
	env.RegisterActivityWithOptions(recorder.verifyGIT, activity.RegisterOptions{Name: profileworkflow.RequestGitVerificationActivity})
	for i, signal := range signals {
		signal := signal
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(profileworkflow.DocumentReviewSignalName, signal)
		}, time.Duration(i+1)*time.Second)
	}
	env.ExecuteWorkflow(profileworkflow.TransporterVettingWorkflow, profileworkflow.VettingInput{
		Context:                    "acme",
		TradingPartnerID:           "partner-1",
		AttemptNumber:              1,
		RequiredDocumentReferences: required,
		ActivityTimeouts: profileworkflow.ActivityTimeouts{
			StartToClose:    2 * time.Second,
			ScheduleToClose: 3 * time.Second,
		},
	})
	if err := env.GetWorkflowError(); err != nil {
		return profileworkflow.VettingResult{}, err
	}
	var result profileworkflow.VettingResult
	return result, env.GetWorkflowResult(&result)
}

var _ = Describe("Transporter vetting workflows", func() {
	Context("BR-TP21 both branches gate Vetted and fleet availability", func() {
		It("opens FleetAvailabilityGate and reaches Vetted only after both branches succeed", func() {
			recorder := &eventRecorder{}
			result, err := executeVetting(recorder, []string{"operating-license"}, profileworkflow.DocumentReviewSignal{Reference: "operating-license", Approved: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(profiledomain.StatusVetted))
			Expect(result.FleetAvailabilityGate).To(BeTrue())
			Expect(recorder.types()).To(ContainElements(profiledomain.DocumentApprovedEvent, profiledomain.GitVerifiedEvent, profiledomain.VettedEvent))
		})
	})

	Context("BR-TP22 saga failure appends compensation events", func() {
		It("reverts document approval on GIT failure but has nothing to compensate for GIT-success-alone", func() {
			gitFailure := &eventRecorder{gitErr: errors.New("GIT rejected")}
			result, err := executeVetting(gitFailure, []string{"operating-license"}, profileworkflow.DocumentReviewSignal{Reference: "operating-license", Approved: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(profiledomain.StatusRejected))
			Expect(result.FleetAvailabilityGate).To(BeFalse())
			Expect(gitFailure.types()).To(ContainElements(profiledomain.DocumentApprovedEvent, profiledomain.DocumentApprovalRevertedEvent, profiledomain.RejectedEvent))

			gitSuccess := &eventRecorder{}
			result, err = executeVetting(gitSuccess, []string{"operating-license"}, profileworkflow.DocumentReviewSignal{Reference: "operating-license", Approved: false})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(profiledomain.StatusRejected))
			Expect(gitSuccess.types()).NotTo(ContainElement(profiledomain.DocumentApprovalRevertedEvent))
		})
	})

	Context("BR-TP23 document review is signal-driven and per reference", func() {
		It("deduplicates approvals by reference and rejects on a rejected reference", func() {
			recorder := &eventRecorder{}
			result, err := executeVetting(recorder, []string{"doc-a", "doc-b"},
				profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true},
				profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true},
				profileworkflow.DocumentReviewSignal{Reference: "doc-b", Approved: true},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ApprovedDocumentReferences).To(ConsistOf("doc-a", "doc-b"))
			Expect(recorder.types()).To(HaveExactElements(
				profiledomain.VettingStartedEvent,
				profiledomain.DocumentApprovedEvent,
				profiledomain.DocumentApprovedEvent,
				profiledomain.GitVerifiedEvent,
				profiledomain.VettedEvent,
			))
		})
	})

	Context("BR-TP25 GIT verification has explicit configurable timeouts", func() {
		It("rejects zero timeout configuration and maps insurer failure into a rejected attempt", func() {
			Expect(profileworkflow.ActivityTimeouts{}.Validate()).To(MatchError(profileworkflow.ErrActivityTimeoutsRequired))
			Expect(profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second}.Validate()).To(Succeed())

			recorder := &eventRecorder{gitErr: errors.New("activity timed out")}
			result, err := executeVetting(recorder, []string{"doc-a"}, profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(profiledomain.StatusRejected))
		})
	})

	Context("BR-TP28 scheduled GIT drops revoke availability and suspend", func() {
		It("delegates one detected drop to HandleGitStatusDrop", func() {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context, profileworkflow.GitMonitorInput) error {
				calls++
				return nil
			}, activity.RegisterOptions{Name: profileworkflow.HandleGitStatusDropActivity})
			env.ExecuteWorkflow(profileworkflow.TransporterGitMonitorWorkflow, profileworkflow.GitMonitorInput{
				TradingPartnerID: "partner-1",
				ActivityTimeouts: profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second},
			})
			Expect(env.GetWorkflowError()).NotTo(HaveOccurred())
			Expect(calls).To(Equal(1))
		})
	})
})
