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

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/workflow"
)

type eventRecorder struct {
	mu          sync.Mutex
	events      []profileworkflow.ProfileEventInput
	gitErr      error
	scheduled   []profileworkflow.GitMonitorInput
	scheduleErr error

	// coverExpiries is served to CoverExpiry one per call, so a spec can move
	// the expiry between re-arms (BR-TP61). The last value repeats once the
	// list is exhausted.
	coverExpiries []*int64
	coverCalls    int
	drops         []profileworkflow.GitMonitorInput
}

func (r *eventRecorder) coverExpiry(_ context.Context, in profileworkflow.CoverExpiryInput) (*int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.coverCalls++
	if len(r.coverExpiries) == 0 {
		return nil, nil
	}
	if r.coverCalls > len(r.coverExpiries) {
		return r.coverExpiries[len(r.coverExpiries)-1], nil
	}
	return r.coverExpiries[r.coverCalls-1], nil
}

func (r *eventRecorder) handleDrop(_ context.Context, in profileworkflow.GitMonitorInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drops = append(r.drops, in)
	return nil
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

// scheduleMonitor records BR-TP28's schedule creation. Recorded rather than
// ignored so a spec can prove the monitor is armed only on the Vetted path —
// a rejected or compensated attempt must not leave a schedule behind.
func (r *eventRecorder) scheduleMonitor(_ context.Context, in profileworkflow.GitMonitorInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scheduled = append(r.scheduled, in)
	return r.scheduleErr
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

// lapseAt is an expiry that many hours from the test environment's start.
// The env's clock begins at wall-clock now, so a duration from time.Now() is
// what the workflow's own workflow.Now() will be measured against.
func lapseAt(in time.Duration) int64 {
	return time.Now().Add(in).Unix()
}

func vettingInput(required []string) profileworkflow.VettingInput {
	return profileworkflow.VettingInput{
		Tenant: "acme", Context: "acme", OrganizationID: "partner-1", AttemptNumber: 1,
		RequiredDocumentReferences: required,
		ActivityTimeouts: profileworkflow.ActivityTimeouts{
			StartToClose: 2 * time.Second, ScheduleToClose: 3 * time.Second,
		},
	}
}

func registerVettingActivities(env *testsuite.TestWorkflowEnvironment, recorder *eventRecorder) {
	env.RegisterActivityWithOptions(recorder.append, activity.RegisterOptions{Name: profileworkflow.AppendProfileEventActivity})
	env.RegisterActivityWithOptions(recorder.verifyGIT, activity.RegisterOptions{Name: profileworkflow.RequestGitVerificationActivity})
	env.RegisterActivityWithOptions(recorder.coverExpiry, activity.RegisterOptions{Name: profileworkflow.CoverExpiryActivity})
	env.RegisterActivityWithOptions(recorder.handleDrop, activity.RegisterOptions{Name: profileworkflow.HandleGitStatusDropActivity})
}

func executeVetting(recorder *eventRecorder, required []string, signals ...profileworkflow.DocumentReviewSignal) (profileworkflow.VettingResult, error) {
	GinkgoHelper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(recorder.append, activity.RegisterOptions{Name: profileworkflow.AppendProfileEventActivity})
	env.RegisterActivityWithOptions(recorder.verifyGIT, activity.RegisterOptions{Name: profileworkflow.RequestGitVerificationActivity})
	env.RegisterActivityWithOptions(recorder.coverExpiry, activity.RegisterOptions{Name: profileworkflow.CoverExpiryActivity})
	env.RegisterActivityWithOptions(recorder.handleDrop, activity.RegisterOptions{Name: profileworkflow.HandleGitStatusDropActivity})
	for i, signal := range signals {
		signal := signal
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(profileworkflow.DocumentReviewSignalName, signal)
		}, time.Duration(i+1)*time.Second)
	}
	// BR-TP62 keeps a vetted run *running* — it is holding the cover timer —
	// so unlike every other outcome there is no Result to wait for. The state
	// is read by query while the run is still open, which is exactly what an
	// operator reads in the Temporal UI, and the run is then cancelled so the
	// test environment has an end to reach. Without the cancel the env sits on
	// a run that never finishes and reports it as a ScheduleToClose deadline,
	// which says nothing about what the workflow actually did.
	var (
		parked   profileworkflow.VettingResult
		didPark  bool
		queryErr error
	)
	env.RegisterDelayedCallback(func() {
		if env.IsWorkflowCompleted() {
			return
		}
		value, err := env.QueryWorkflow(profileworkflow.VettingStateQuery)
		if err != nil {
			queryErr = err
			env.CancelWorkflow()
			return
		}
		queryErr = value.Get(&parked)
		didPark = true
		env.CancelWorkflow()
	}, time.Duration(len(signals)+1)*time.Second)

	env.ExecuteWorkflow(profileworkflow.TransporterVettingWorkflow, profileworkflow.VettingInput{
		Tenant:                     "acme",
		Context:                    "acme",
		OrganizationID:             "partner-1",
		AttemptNumber:              1,
		RequiredDocumentReferences: required,
		ActivityTimeouts: profileworkflow.ActivityTimeouts{
			StartToClose:    2 * time.Second,
			ScheduleToClose: 3 * time.Second,
		},
	})
	if queryErr != nil {
		return profileworkflow.VettingResult{}, queryErr
	}
	if didPark {
		return parked, nil
	}
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

	Context("BR-TP60/BR-TP62 the cover timer is armed at Vetted, and only then", func() {
		// The run does not complete on this path — it is holding the timer —
		// so "reached Vetted" is read by query, the same way the UI reads it.
		It("parks in Vetted holding the timer rather than completing", func() {
			expiry := lapseAt(30 * 24 * time.Hour)
			recorder := &eventRecorder{coverExpiries: []*int64{&expiry}}

			state, err := executeVetting(recorder, []string{"doc-a"},
				profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(state.Status).To(Equal(profiledomain.StatusVetted))
			Expect(state.FleetAvailabilityGate).To(BeTrue())
			Expect(recorder.coverCalls).To(Equal(1), "the expiry is read once per arming, not polled")
			Expect(recorder.drops).To(BeEmpty(), "cover has not lapsed, so nothing may be dropped")
		})

		// The failure this guards is the one 38h-ii's polling predecessor
		// could produce: a transporter that never vetted, or whose approvals
		// were compensated away, must not end up with something armed that can
		// later suspend it for losing cover it never had.
		It("arms nothing when the attempt ends Rejected", func() {
			rejected := &eventRecorder{}
			result, err := executeVetting(rejected, []string{"doc-a"},
				profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: false})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(profiledomain.StatusRejected))
			Expect(rejected.coverCalls).To(BeZero(), "a rejected attempt never reaches the watch")
			Expect(rejected.drops).To(BeEmpty())

			compensated := &eventRecorder{gitErr: errors.New("GIT verification rejected")}
			_, err = executeVetting(compensated, []string{"doc-a"},
				profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(compensated.coverCalls).To(BeZero(),
				"BR-TP22 reverted the approvals, so there is no cover to watch")
		})

		// A document with no expiry cannot lapse by time. The run must still
		// park — a completed run has nothing left to receive BR-TP61's signal
		// — but it must not arm a timer or invent a lapse.
		It("parks without arming when the cover has no expiry", func() {
			recorder := &eventRecorder{coverExpiries: []*int64{nil}}

			state, err := executeVetting(recorder, []string{"doc-a"},
				profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(state.Status).To(Equal(profiledomain.StatusVetted))
			Expect(recorder.drops).To(BeEmpty())
		})
	})

	Context("BR-TP61 a changed expiry re-arms the timer", func() {
		It("re-reads the expiry on CoverChanged rather than trusting the signal payload", func() {
			GinkgoHelper()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()

			near := lapseAt(time.Hour)
			far := lapseAt(365 * 24 * time.Hour)
			recorder := &eventRecorder{coverExpiries: []*int64{&near, &far}}
			registerVettingActivities(env, recorder)

			env.RegisterDelayedCallback(func() {
				env.SignalWorkflow(profileworkflow.DocumentReviewSignalName,
					profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true})
			}, time.Second)
			// Sent well before the near expiry, so the only way the run can
			// survive past it is by having re-armed against the far one.
			env.RegisterDelayedCallback(func() {
				env.SignalWorkflow(profileworkflow.CoverChangedSignalName, nil)
			}, 2*time.Second)
			env.RegisterDelayedCallback(func() {
				env.CancelWorkflow()
			}, 48*time.Hour)

			env.ExecuteWorkflow(profileworkflow.TransporterVettingWorkflow, vettingInput([]string{"doc-a"}))

			Expect(recorder.coverCalls).To(Equal(2), "the expiry is re-read, not taken from the signal")
			Expect(recorder.drops).To(BeEmpty(),
				"the original expiry passed while the run was armed against the new one; firing on it would be firing on a stale expiry")
		})
	})

	Context("BR-TP62 the timer's lifetime is exactly the Vetted state", func() {
		It("drops cover when the timer fires and leaves Vetted for CoverLapsed", func() {
			expiry := lapseAt(2 * time.Hour)
			recorder := &eventRecorder{coverExpiries: []*int64{&expiry}}

			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			registerVettingActivities(env, recorder)
			env.RegisterDelayedCallback(func() {
				env.SignalWorkflow(profileworkflow.DocumentReviewSignalName,
					profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true})
			}, time.Second)

			env.ExecuteWorkflow(profileworkflow.TransporterVettingWorkflow, vettingInput([]string{"doc-a"}))

			Expect(env.IsWorkflowCompleted()).To(BeTrue(), "leaving Vetted ends the watch, and with it the run")
			Expect(env.GetWorkflowError()).NotTo(HaveOccurred())

			var result profileworkflow.VettingResult
			Expect(env.GetWorkflowResult(&result)).To(Succeed())
			Expect(result.Status).To(Equal(profiledomain.StatusCoverLapsed),
				"BR-TP63: the lapse is a state transition, not just a gate flipped underneath Vetted")
			Expect(result.FleetAvailabilityGate).To(BeFalse())

			Expect(recorder.drops).To(HaveLen(1), "exactly one drop per lapse")
			Expect(recorder.drops[0].OrganizationID).To(Equal("partner-1"))
			Expect(recorder.drops[0].Tenant).To(Equal("acme"),
				"BR-TP58: the drop publishes over the tenant's own connection")
			Expect(recorder.drops[0].Context).To(Equal("acme"))
		})
	})

})
