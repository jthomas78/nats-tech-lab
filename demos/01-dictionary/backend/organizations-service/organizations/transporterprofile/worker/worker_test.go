package worker_test

import (
	"context"
	"errors"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	profileworker "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/worker"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/workflow"
)

type fakeResubmitter struct {
	order *[]string
	state profiledomain.State
}

func (r *fakeResubmitter) Resubmit(context.Context, string, string) (profiledomain.State, error) {
	*r.order = append(*r.order, "append")
	return r.state, nil
}

type fakeWorkflowExecutor struct {
	order   *[]string
	options client.StartWorkflowOptions
	input   profileworkflow.VettingInput
}

func (e *fakeWorkflowExecutor) ExecuteWorkflow(_ context.Context, options client.StartWorkflowOptions, _ interface{}, args ...interface{}) (client.WorkflowRun, error) {
	*e.order = append(*e.order, "start")
	e.options = options
	e.input = args[0].(profileworkflow.VettingInput)
	return nil, nil
}

var _ = Describe("TransporterProfile worker facade", func() {
	Context("BR-TP26 rejected profiles resubmit under the same workflow ID", func() {
		It("appends VettingResubmitted before start and passes the incremented attempt with AllowDuplicate", func() {
			order := []string{}
			commands := &fakeResubmitter{order: &order, state: profiledomain.State{Context: "acme", ID: "partner-1", Status: profiledomain.StatusInReview, AttemptNumber: 2}}
			executor := &fakeWorkflowExecutor{order: &order}
			service := profileworker.NewVettingService(executor, commands, profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second})

			Expect(service.Resubmit(context.Background(), "acme", "acme", "partner-1", []string{"doc-a"})).To(Succeed())
			Expect(order).To(HaveExactElements("append", "start"))
			Expect(executor.input.Tenant).To(Equal("acme"), "BR-TP58: a resubmitted attempt carries its tenant too")
			Expect(executor.options.ID).To(Equal("acme-transporter-vetting-partner-1"))
			Expect(executor.options.WorkflowIDReusePolicy).To(Equal(enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE))
			Expect(executor.input.AttemptNumber).To(Equal(2))
			Expect(executor.input.Resubmitted).To(BeTrue())
		})
	})

	Context("38h-ii D6: the retired polling Schedules are recognised for deletion", func() {
		It("matches a schedule ID in any context, and nothing else", func() {
			Expect(profileworker.IsGitMonitorScheduleID("acme-transporter-git-monitor-partner-1")).To(BeTrue())
			Expect(profileworker.IsGitMonitorScheduleID("acme-northdiv-transporter-git-monitor-partner-1")).To(BeTrue())

			// An action ID names a workflow execution, not a schedule.
			Expect(profileworker.IsGitMonitorScheduleID("acme-transporter-git-monitor-run-partner-1-2026-08-21T11:30:00Z")).To(BeFalse())
			// Deleting either of these would take out something this service
			// still relies on, or something it does not own at all.
			Expect(profileworker.IsGitMonitorScheduleID("acme-transporter-vetting-partner-1")).To(BeFalse())
			Expect(profileworker.IsGitMonitorScheduleID("some-other-service-schedule")).To(BeFalse())
		})
	})
})

type fakeOrganizations struct {
	organization organizationdomain.Organization
	err          error
}

func (f *fakeOrganizations) Get(context.Context, string) (organizationdomain.Organization, error) {
	return f.organization, f.err
}

type fakeProfileState struct{ state profiledomain.State }

func (f *fakeProfileState) Get(context.Context, string) (profiledomain.State, error) {
	return f.state, nil
}

type fakeDocuments struct {
	docs []organizationdomain.ComplianceDocument
}

func (f *fakeDocuments) ListDocuments(context.Context, string) ([]organizationdomain.ComplianceDocument, error) {
	return f.docs, nil
}

var _ = Describe("BR-TP56 submit for vetting", func() {
	var (
		order    []string
		executor *fakeWorkflowExecutor
		orgs     *fakeOrganizations
		profiles *fakeProfileState
		docs     *fakeDocuments
	)

	timeouts := profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second}

	build := func() *profileworker.VettingService {
		return profileworker.NewVettingService(executor, &fakeResubmitter{order: &order,
			state: profiledomain.State{AttemptNumber: 2}}, timeouts).
			WithSubmit(orgs, profiles, docs)
	}

	BeforeEach(func() {
		order = []string{}
		executor = &fakeWorkflowExecutor{order: &order}
		orgs = &fakeOrganizations{organization: organizationdomain.Organization{
			ID: "org-1", Type: organizationdomain.PartnerTypeTransporter}}
		profiles = &fakeProfileState{state: profiledomain.State{
			Status: profiledomain.StatusAwaiting, AttemptNumber: 0}}
		docs = &fakeDocuments{docs: []organizationdomain.ComplianceDocument{
			{ID: "doc-b", Status: organizationdomain.DocumentStatusForReview},
			{ID: "doc-a", Status: organizationdomain.DocumentStatusForReview},
		}}
	})

	It("starts the workflow with the pending document IDs as the required references", func() {
		Expect(build().Submit(context.Background(), "acme", "acme", "org-1")).To(Succeed())

		Expect(order).To(HaveExactElements("start"), "a first attempt appends nothing itself; the workflow emits VettingStarted")
		Expect(executor.input.RequiredDocumentReferences).To(HaveExactElements("doc-a", "doc-b"))
		Expect(executor.input.Tenant).To(Equal("acme"), "BR-TP58: the attempt must carry the tenant its events publish over")
		Expect(executor.input.AttemptNumber).To(Equal(1))
		Expect(executor.input.Resubmitted).To(BeFalse())
		Expect(executor.options.ID).To(Equal("acme-transporter-vetting-org-1"))
	})

	It("excludes documents that are no longer pending review", func() {
		docs.docs = append(docs.docs,
			organizationdomain.ComplianceDocument{ID: "doc-approved", Status: organizationdomain.DocumentStatusApproved},
			organizationdomain.ComplianceDocument{ID: "doc-rejected", Status: organizationdomain.DocumentStatusRejected},
			organizationdomain.ComplianceDocument{ID: "doc-superseded", Status: organizationdomain.DocumentStatusSuperseded},
		)

		Expect(build().Submit(context.Background(), "acme", "acme", "org-1")).To(Succeed())

		Expect(executor.input.RequiredDocumentReferences).To(HaveExactElements("doc-a", "doc-b"),
			"the workflow waits on exactly the set it is given, so a settled document would strand the attempt")
	})

	It("refuses a SHIPPER without starting a workflow", func() {
		orgs.organization.Type = organizationdomain.PartnerTypeShipper

		Expect(build().Submit(context.Background(), "acme", "acme", "org-1")).
			To(MatchError(profileworker.ErrNotTransporter))
		Expect(order).To(BeEmpty())
	})

	It("refuses a transporter with no document pending review", func() {
		docs.docs = []organizationdomain.ComplianceDocument{
			{ID: "doc-approved", Status: organizationdomain.DocumentStatusApproved},
		}

		Expect(build().Submit(context.Background(), "acme", "acme", "org-1")).
			To(MatchError(profileworker.ErrNoPendingDocuments))
		Expect(order).To(BeEmpty())
	})

	// Phase 40 retired document resubmission: a rejected document is replaced
	// by dropping another file, so until that replacement exists there is
	// nothing in review and BR-TP26's re-vet has nothing to collect.
	It("refuses a re-vet while every document is still rejected", func() {
		profiles.state.Status = profiledomain.StatusRejected
		docs.docs = []organizationdomain.ComplianceDocument{
			{ID: "doc-rejected", Status: organizationdomain.DocumentStatusRejected},
		}

		Expect(build().Submit(context.Background(), "acme", "acme", "org-1")).
			To(MatchError(profileworker.ErrNoPendingDocuments))
		Expect(order).To(BeEmpty())
	})

	DescribeTable("refuses a profile that is not awaiting documentation",
		func(status profiledomain.Status) {
			profiles.state.Status = status

			Expect(build().Submit(context.Background(), "acme", "acme", "org-1")).
				To(MatchError(profileworker.ErrProfileNotSubmittable))
			Expect(order).To(BeEmpty())
		},
		Entry("already in review", profiledomain.StatusInReview),
		Entry("already vetted", profiledomain.StatusVetted),
	)

	// Routing rather than rejecting keeps one entry point for callers while
	// leaving BR-TP26 the only path that appends VettingResubmitted — the two
	// must never both start a workflow for the same transporter.
	It("routes a Rejected profile through BR-TP26's resubmit path", func() {
		profiles.state.Status = profiledomain.StatusRejected

		Expect(build().Submit(context.Background(), "acme", "acme", "org-1")).To(Succeed())

		Expect(order).To(HaveExactElements("append", "start"))
		Expect(executor.input.Resubmitted).To(BeTrue())
		Expect(executor.input.AttemptNumber).To(Equal(2))
		Expect(executor.input.Tenant).To(Equal("acme"))
		Expect(executor.input.RequiredDocumentReferences).To(HaveExactElements("doc-a", "doc-b"))
	})
})

type recordedSignal struct {
	workflowID, runID, name string
	arg                     interface{}
}

type fakeSignaler struct {
	sent []recordedSignal
	err  error
}

func (f *fakeSignaler) SignalWorkflow(_ context.Context, workflowID, runID, name string, arg interface{}) error {
	f.sent = append(f.sent, recordedSignal{workflowID: workflowID, runID: runID, name: name, arg: arg})
	return f.err
}

var _ = Describe("BR-TP57 document review signals reach the workflow", func() {
	var (
		signaler *fakeSignaler
		service  *profileworker.VettingService
	)

	BeforeEach(func() {
		signaler = &fakeSignaler{}
		order := []string{}
		service = profileworker.NewVettingService(&fakeWorkflowExecutor{order: &order}, nil,
			profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second}).
			WithSignaler(signaler)
	})

	DescribeTable("sends exactly one signal carrying the document ID and verdict",
		func(approved bool) {
			Expect(service.SignalDocumentReview(context.Background(), "acme", "org-1", "doc-7", approved)).To(Succeed())

			Expect(signaler.sent).To(HaveLen(1))
			sent := signaler.sent[0]
			Expect(sent.workflowID).To(Equal("acme-transporter-vetting-org-1"))
			Expect(sent.runID).To(BeEmpty(), "an empty run ID targets the live attempt, which BR-TP26's reuse policy keeps under one workflow ID")
			Expect(sent.name).To(Equal(profileworkflow.DocumentReviewSignalName))
			Expect(sent.arg).To(Equal(profileworkflow.DocumentReviewSignal{Reference: "doc-7", Approved: approved}))
		},
		Entry("approved", true),
		Entry("rejected", false),
	)

	It("treats a transporter with no running workflow as success", func() {
		signaler.err = serviceerror.NewNotFound("workflow execution not found")

		Expect(service.SignalDocumentReview(context.Background(), "acme", "org-1", "doc-7", true)).To(Succeed(),
			"reviewing a document outside a vetting attempt stays legal")
	})

	It("returns a transport failure for the caller to log", func() {
		signaler.err = errors.New("temporal unavailable")

		Expect(service.SignalDocumentReview(context.Background(), "acme", "org-1", "doc-7", true)).
			To(MatchError("temporal unavailable"))
	})

	It("signals nothing when no signaler is configured", func() {
		bare := profileworker.NewVettingService(nil, nil,
			profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second})

		Expect(bare.SignalDocumentReview(context.Background(), "acme", "org-1", "doc-7", true)).To(Succeed())
		Expect(signaler.sent).To(BeEmpty())
	})
})
