// Package worker wires transporter workflows and activities to Temporal and
// exposes the command facade used to start/resubmit them.
package worker

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/workflow"
)

var ProductionActivityTimeouts = profileworkflow.ActivityTimeouts{StartToClose: 30 * time.Second, ScheduleToClose: 2 * time.Minute}

type WorkflowExecutor interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, interface{}, ...interface{}) (client.WorkflowRun, error)
}
type ResubmitCommands interface {
	Resubmit(context.Context, string, string) (profiledomain.State, error)
}

type VettingService struct {
	executor WorkflowExecutor
	commands ResubmitCommands
	timeouts profileworkflow.ActivityTimeouts

	// BR-TP56's guard readers, supplied by WithSubmit.
	organizations OrganizationReader
	profiles      ProfileStateReader
	documents     DocumentLister

	// BR-TP57's signal transport, supplied by WithSignaler.
	signaler WorkflowSignaler
}

func NewVettingService(executor WorkflowExecutor, commands ResubmitCommands, timeouts profileworkflow.ActivityTimeouts) *VettingService {
	return &VettingService{executor: executor, commands: commands, timeouts: timeouts}
}

func (s *VettingService) Resubmit(ctx context.Context, tenant, contextKey, organizationID string, required []string) error {
	if err := s.timeouts.Validate(); err != nil {
		return err
	}
	state, err := s.commands.Resubmit(ctx, contextKey, organizationID)
	if err != nil {
		return err
	}
	_, err = s.executor.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: workflowID(contextKey, organizationID), TaskQueue: profileworkflow.TaskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, profileworkflow.TransporterVettingWorkflow, profileworkflow.VettingInput{
		Tenant: tenant, Context: contextKey, OrganizationID: organizationID, AttemptNumber: state.AttemptNumber,
		RequiredDocumentReferences: required, ActivityTimeouts: s.timeouts, Resubmitted: true,
	})
	return err
}

func workflowID(contextKey, organizationID string) string {
	return contextKey + "-transporter-vetting-" + organizationID
}

// DeleteGitMonitorSchedules removes the per-transporter polling Schedules that
// BR-TP28 created before 38h-ii replaced them with the durable cover timer
// (D6). Without this they keep firing into a TransporterGitMonitorWorkflow
// that is no longer registered — and one of them on the live stack had been
// failing every interval with "tenant is not connected" since before the
// tenant fix, which is its own argument for clearing them out rather than
// leaving them to rot.
func DeleteGitMonitorSchedules(ctx context.Context, temporalClient client.Client) (int, error) {
	iter, err := temporalClient.ScheduleClient().List(ctx, client.ScheduleListOptions{})
	if err != nil {
		return 0, err
	}
	deleted := 0
	for iter.HasNext() {
		entry, err := iter.Next()
		if err != nil {
			return deleted, err
		}
		if !IsGitMonitorScheduleID(entry.ID) {
			continue
		}
		if err := temporalClient.ScheduleClient().GetHandle(ctx, entry.ID).Delete(ctx); err != nil {
			// A schedule deleted by another replica racing this one is the
			// outcome we wanted, not a failure to report.
			var notFound *serviceerror.NotFound
			if errors.As(err, &notFound) {
				continue
			}
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// IsGitMonitorScheduleID recognises the IDs the retired scheduler minted,
// {context}-transporter-git-monitor-{organizationID}. Exported so the rule
// about *what gets deleted* is testable without faking Temporal's schedule
// client: deleting too eagerly here would take out schedules this service
// does not own.
//
// Note it must not match the retired *action* IDs
// ({context}-transporter-git-monitor-run-...), which name workflow
// executions rather than schedules and are not deletable this way.
func IsGitMonitorScheduleID(id string) bool {
	marker := "-transporter-git-monitor-"
	index := strings.Index(id, marker)
	if index < 0 {
		return false
	}
	return !strings.HasPrefix(id[index+len(marker):], "run-")
}

type Activities interface {
	AppendProfileEvent(context.Context, profileworkflow.ProfileEventInput) error
	RequestGitVerification(context.Context, profileworkflow.GitVerificationInput) error
	HandleGitStatusDrop(context.Context, profileworkflow.GitMonitorInput) error
	CoverExpiry(context.Context, profileworkflow.CoverExpiryInput) (*int64, error)
	DocumentReviewState(context.Context, profileworkflow.DocumentReviewStateInput) (map[string]string, error)
}

func New(temporalClient client.Client, activities Activities) temporalworker.Worker {
	w := temporalworker.New(temporalClient, profileworkflow.TaskQueue, temporalworker.Options{})
	w.RegisterWorkflow(profileworkflow.TransporterVettingWorkflow)
	w.RegisterActivityWithOptions(activities.AppendProfileEvent, activity.RegisterOptions{Name: profileworkflow.AppendProfileEventActivity})
	w.RegisterActivityWithOptions(activities.RequestGitVerification, activity.RegisterOptions{Name: profileworkflow.RequestGitVerificationActivity})
	w.RegisterActivityWithOptions(activities.HandleGitStatusDrop, activity.RegisterOptions{Name: profileworkflow.HandleGitStatusDropActivity})
	w.RegisterActivityWithOptions(activities.CoverExpiry, activity.RegisterOptions{Name: profileworkflow.CoverExpiryActivity})
	w.RegisterActivityWithOptions(activities.DocumentReviewState, activity.RegisterOptions{Name: profileworkflow.DocumentReviewStateActivity})
	return w
}

// --- BR-TP56: submit for vetting ---------------------------------------

// ErrNotTransporter, ErrProfileNotSubmittable and ErrNoPendingDocuments are
// BR-TP56's three refusals. Each maps to 409 Conflict at the api.* boundary
// via isConflictErr, the same way BR-TP19's gate does.
var (
	ErrNotTransporter        = errors.New("only a TRANSPORTER can be submitted for vetting")
	ErrProfileNotSubmittable = errors.New("transporter profile is not awaiting documentation")
	ErrNoPendingDocuments    = errors.New("transporter has no compliance document pending review")
)

type OrganizationReader interface {
	Get(context.Context, string) (organizationdomain.Organization, error)
}

type ProfileStateReader interface {
	Get(context.Context, string) (profiledomain.State, error)
}

type DocumentLister interface {
	ListDocuments(context.Context, string) ([]organizationdomain.ComplianceDocument, error)
}

// WithSubmit supplies the three readers BR-TP56's guard needs. Kept separate
// from NewVettingService so the BR-TP26 resubmit path, which predates this
// rule and does not consult them, stays constructible on its own.
func (s *VettingService) WithSubmit(organizations OrganizationReader, profiles ProfileStateReader, documents DocumentLister) *VettingService {
	s.organizations, s.profiles, s.documents = organizations, profiles, documents
	return s
}

// Submit is BR-TP56. It is the only entry point a caller needs: a Rejected
// profile is routed to BR-TP26's Resubmit rather than started fresh, so the
// two paths cannot both start a workflow for the same transporter and
// VettingResubmitted still gets appended where that rule requires it.
//
// The required references are the document IDs pending review *at submit
// time* (BR-TP36: the document ID is the vetting reference). Already-approved
// and rejected documents are excluded — the workflow waits on exactly the set
// it is given, so including a settled document would hang the attempt.
func (s *VettingService) Submit(ctx context.Context, tenant, contextKey, organizationID string) error {
	if err := s.timeouts.Validate(); err != nil {
		return err
	}
	if s.organizations == nil || s.profiles == nil || s.documents == nil {
		return errors.New("submit-for-vetting readers are not configured")
	}

	organization, err := s.organizations.Get(ctx, organizationID)
	if err != nil {
		return err
	}
	if organization.Type != organizationdomain.PartnerTypeTransporter {
		return ErrNotTransporter
	}

	state, err := s.profiles.Get(ctx, organizationID)
	if err != nil {
		return err
	}
	switch state.Status {
	case profiledomain.StatusAwaitingDocumentation, profiledomain.StatusRejected:
	default:
		return ErrProfileNotSubmittable
	}

	required, err := s.pendingReferences(ctx, organizationID)
	if err != nil {
		return err
	}
	if len(required) == 0 {
		return ErrNoPendingDocuments
	}

	if state.Status == profiledomain.StatusRejected {
		return s.Resubmit(ctx, tenant, contextKey, organizationID, required)
	}

	_, err = s.executor.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: workflowID(contextKey, organizationID), TaskQueue: profileworkflow.TaskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, profileworkflow.TransporterVettingWorkflow, profileworkflow.VettingInput{
		Tenant: tenant, Context: contextKey, OrganizationID: organizationID,
		AttemptNumber: state.AttemptNumber + 1, RequiredDocumentReferences: required,
		ActivityTimeouts: s.timeouts,
	})
	return err
}

// pendingReferences returns the IDs of documents awaiting review, sorted so a
// workflow's required set is deterministic — Temporal replays it, and an
// input that reordered between runs would be a needless source of history
// churn.
func (s *VettingService) pendingReferences(ctx context.Context, organizationID string) ([]string, error) {
	docs, err := s.documents.ListDocuments(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	var required []string
	for _, doc := range docs {
		if doc.Status == organizationdomain.DocumentStatusPending {
			required = append(required, doc.ID)
		}
	}
	sort.Strings(required)
	return required, nil
}

// --- BR-TP57: document review signals ----------------------------------

// WorkflowSignaler is Temporal's SignalWorkflow, narrowed. An empty runID
// means "the currently-running execution of this workflow ID", which is what
// BR-TP26's reuse policy makes the right target: resubmission runs under the
// same workflow ID, so the signal follows the live attempt without the caller
// tracking run IDs.
type WorkflowSignaler interface {
	SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error
}

// WithSignaler supplies BR-TP57's signal transport.
func (s *VettingService) WithSignaler(signaler WorkflowSignaler) *VettingService {
	s.signaler = signaler
	return s
}

// SignalCoverChanged is BR-TP61: it tells a parked vetting run that a
// goods-in-transit document's expiry may have moved, so it re-reads and
// re-arms its cover timer.
//
// It carries no payload deliberately. The document is the authority on when
// cover lapses, and a date travelling in a signal can be stale by the time it
// is delivered — the workflow re-reads through CoverExpiry instead.
//
// Like SignalDocumentReview, "no running workflow" is success: a transporter
// that is not vetted has no timer to re-arm, which is exactly BR-TP62's
// invariant rather than a failure.
func (s *VettingService) SignalCoverChanged(ctx context.Context, contextKey, organizationID string) error {
	if s.signaler == nil {
		return nil
	}
	err := s.signaler.SignalWorkflow(ctx, workflowID(contextKey, organizationID), "",
		profileworkflow.CoverChangedSignalName, nil)
	if isWorkflowNotFound(err) {
		return nil
	}
	return err
}

// SignalDocumentReview is BR-TP57: exactly one DocumentReview signal per
// review, carrying the document ID as the reference (BR-TP36) and the verdict.
//
// A transporter with no running workflow is not an error — reviewing a
// document outside a vetting attempt stays legal — so a "workflow not found"
// answer is reported as success. Any other failure is returned for the caller
// to log; the caller never rolls the review back over it.
func (s *VettingService) SignalDocumentReview(ctx context.Context, contextKey, organizationID, reference string, approved bool) error {
	if s.signaler == nil {
		return nil
	}
	err := s.signaler.SignalWorkflow(ctx, workflowID(contextKey, organizationID), "",
		profileworkflow.DocumentReviewSignalName,
		profileworkflow.DocumentReviewSignal{Reference: reference, Approved: approved})
	if isWorkflowNotFound(err) {
		return nil
	}
	return err
}

// isWorkflowNotFound distinguishes "there is no attempt to tell" from a real
// transport failure. Both a NotFound service error and Temporal's
// already-completed error mean the same thing here: nothing is waiting.
func isWorkflowNotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var completed *serviceerror.WorkflowNotReady
	return errors.As(err, &completed)
}
