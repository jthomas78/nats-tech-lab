// Package workflow contains the deterministic Temporal orchestration for
// transporter vetting and periodic GIT status monitoring.
package workflow

import (
	"errors"
	"sort"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

const (
	TaskQueue                      = "organizations-vetting"
	AppendProfileEventActivity     = "AppendProfileEvent"
	RequestGitVerificationActivity = "RequestGitVerification"
	HandleGitStatusDropActivity    = "HandleGitStatusDrop"
	// CoverExpiryActivity reads the earliest expiry across the transporter's
	// current goods-in-transit documents (BR-TP59/BR-TP60). A nil result means
	// no document can lapse by time.
	CoverExpiryActivity      = "CoverExpiry"
	DocumentReviewSignalName = "DocumentReview"
	// DocumentReviewStateActivity reads the *persisted* status of the
	// documents this attempt is waiting on (decision 14). The workflow no
	// longer trusts DocumentReviewSignal's payload: the signal is best-effort,
	// so a review that wrote its row and then failed to signal used to leave
	// this run waiting on an approval that had already happened, and a signal
	// that arrived without its row used to be believed. The signal is now a
	// wake-up and storage is the answer.
	DocumentReviewStateActivity = "DocumentReviewState"
	// CoverChangedSignalName re-arms the cover timer (BR-TP61). Sent by the
	// document write path whenever a goods-in-transit document's expiry could
	// have moved, so the workflow re-reads rather than trusting a payload.
	CoverChangedSignalName = "CoverChanged"
	// VettingStateQuery exposes the run's current derived state to
	// `temporal workflow query` and the Temporal UI. It exists because
	// BR-TP62 keeps the workflow *running* while the profile is Vetted, so
	// unlike a completed run there is no Result field to read the outcome
	// from — without it, the most common state is the one you cannot see.
	VettingStateQuery         = "vettingState"
	SequenceConflictErrorType = "TransporterProfileSequenceConflict"
)

// The two persisted statuses this workflow reacts to. Kept as local literals
// rather than an import of the organizations domain package: the workflow
// module deliberately depends on profiledomain only, and these cross an
// activity boundary as plain strings in either case.
const (
	documentStatusApproved = "APPROVED"
	documentStatusRejected = "REJECTED"
)

var ErrActivityTimeoutsRequired = errors.New("activity StartToCloseTimeout and ScheduleToCloseTimeout are required")

type ActivityTimeouts struct{ StartToClose, ScheduleToClose time.Duration }

func (t ActivityTimeouts) Validate() error {
	if t.StartToClose <= 0 || t.ScheduleToClose <= 0 {
		return ErrActivityTimeoutsRequired
	}
	if t.ScheduleToClose < t.StartToClose {
		return errors.New("activity ScheduleToCloseTimeout must be at least StartToCloseTimeout")
	}
	return nil
}

type VettingInput struct {
	// Tenant is the NATS account whose connection this attempt's events are
	// published over; Context is the company/business unit the event subject
	// is built from. They are separate axes and the mapping is many-to-one
	// (`acme` and `acme-northdiv` share one account), so an activity handed
	// only a context cannot know where to publish (BR-TP58).
	Tenant                     string
	Context                    string
	OrganizationID             string
	AttemptNumber              int
	RequiredDocumentReferences []string
	ActivityTimeouts           ActivityTimeouts
	Resubmitted                bool
	// AlreadyVetted is set only by watchCover's continue-as-new (BR-TP62).
	// The new run resumes the cover watch without re-running vetting, which
	// has already happened and must not append its events a second time.
	AlreadyVetted bool
}

type VettingResult struct {
	Status                     profiledomain.Status
	FleetAvailabilityGate      bool
	ApprovedDocumentReferences []string
}

// maxCoverCycles bounds how many re-arms one run absorbs before continuing as
// new (BR-TP62). The workflow lives as long as the transporter stays vetted —
// potentially years — and every signal and timer adds to a history that is
// replayed in full on each worker restart. Continue-as-new resets that
// history while keeping the workflow ID, so "this transporter's vetting" is
// still one addressable thing.
const maxCoverCycles = 100

// CoverExpiryInput asks for the earliest expiry across a transporter's
// current GIT documents. Tenant travels with it for the same reason it
// travels on every other activity input (BR-TP58).
type CoverExpiryInput struct {
	Tenant                  string
	Context, OrganizationID string
}

// DocumentReviewSignal is a wake-up, not a verdict. Approved is retained
// because the signal is part of the saga's existing wire contract and
// removing it would break senders mid-phase, but the workflow deliberately
// does not read it — see DocumentReviewStateActivity.
type DocumentReviewSignal struct {
	Reference string
	Approved  bool
}

// DocumentReviewStateInput asks for the persisted status of exactly the
// references this attempt requires. Scoped to those rather than "all this
// organization's documents" so an unrelated document cannot influence an
// attempt that never required it.
type DocumentReviewStateInput struct {
	Tenant                  string
	Context, OrganizationID string
	References              []string
}
type ProfileEventInput struct {
	// Tenant routes the append (BR-TP58). It is deliberately here and not on
	// profiledomain.Event: the tenant is the connection an event travels
	// over, never part of the event body, so it stays off the JetStream log
	// exactly as it stays out of every subject token.
	Tenant string
	Event  profiledomain.Event
	Step   int
}
type GitVerificationInput struct {
	Tenant                  string
	Context, OrganizationID string
	AttemptNumber           int
}
type GitMonitorInput struct {
	Tenant                  string
	Context, OrganizationID string
	ActivityTimeouts        ActivityTimeouts
}

func TransporterVettingWorkflow(ctx workflow.Context, input VettingInput) (VettingResult, error) {
	if err := input.ActivityTimeouts.Validate(); err != nil {
		return VettingResult{}, err
	}
	// current is what VettingStateQuery reports. Kept as a plain captured
	// variable rather than derived on demand: a query handler must not block
	// or run activities, so everything it answers with has to already be in
	// workflow state.
	current := VettingResult{Status: profiledomain.StatusInReview}
	if err := workflow.SetQueryHandler(ctx, VettingStateQuery, func() (VettingResult, error) {
		return current, nil
	}); err != nil {
		return VettingResult{}, err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    input.ActivityTimeouts.StartToClose,
		ScheduleToCloseTimeout: input.ActivityTimeouts.ScheduleToClose,
		RetryPolicy:            &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	if input.AlreadyVetted {
		approved := make(map[string]struct{}, len(input.RequiredDocumentReferences))
		for _, reference := range input.RequiredDocumentReferences {
			approved[reference] = struct{}{}
		}
		current = result(profiledomain.StatusVetted, true, approved)
		return watchCover(ctx, input, approved)
	}

	required := make(map[string]struct{}, len(input.RequiredDocumentReferences))
	reviews := make(map[string]profiledomain.DocumentReviewStatus, len(input.RequiredDocumentReferences))
	for _, reference := range input.RequiredDocumentReferences {
		required[reference] = struct{}{}
		reviews[reference] = profiledomain.DocumentPendingReview
	}
	approved := make(map[string]struct{}, len(required))
	step := 0
	appendEvent := func(eventType string, reference string) error {
		step++
		event := profiledomain.Event{
			Type: eventType, Context: input.Context, OrganizationID: input.OrganizationID,
			AttemptNumber: input.AttemptNumber, Step: step, DocumentReference: reference,
			DocumentReviews: cloneReviews(reviews),
			OccurredAt:      workflow.Now(ctx).UTC(),
		}
		switch eventType {
		case profiledomain.VettingStartedEvent, profiledomain.VettingResubmittedEvent:
			event.Status = profiledomain.StatusInReview
		case profiledomain.VettedEvent:
			event.Status, event.FleetAvailabilityGate = profiledomain.StatusVetted, true
			event.GitVerified = true
		case profiledomain.GitVerifiedEvent:
			event.GitVerified = true
		case profiledomain.RejectedEvent:
			event.Status = profiledomain.StatusRejected
		}
		err := workflow.ExecuteActivity(ctx, AppendProfileEventActivity, ProfileEventInput{Tenant: input.Tenant, Event: event, Step: step}).Get(ctx, nil)
		if isSequenceConflict(err) {
			return temporal.NewNonRetryableApplicationError("transporter profile changed; try again", SequenceConflictErrorType, err)
		}
		return err
	}

	// Resubmit already appended VettingResubmitted before this run was
	// started (BR-TP26), so only a first attempt emits VettingStarted here.
	if !input.Resubmitted {
		if err := appendEvent(profiledomain.VettingStartedEvent, ""); err != nil {
			return VettingResult{}, err
		}
	}

	// Scheduling the Activity before waiting for signals makes GIT verification
	// and document review genuine parallel branches in Temporal history.
	gitFuture := workflow.ExecuteActivity(ctx, RequestGitVerificationActivity, GitVerificationInput{
		Tenant: input.Tenant, Context: input.Context, OrganizationID: input.OrganizationID, AttemptNumber: input.AttemptNumber,
	})

	signals := workflow.GetSignalChannel(ctx, DocumentReviewSignalName)
	rejected := false
	references := append([]string(nil), input.RequiredDocumentReferences...)
	for len(approved) < len(required) && !rejected {
		var signal DocumentReviewSignal
		signals.Receive(ctx, &signal)

		// Decision 14: the signal's only job is to wake this run. What
		// actually happened is read back from storage, and the command that
		// wrote that row is the sole producer of document-approved /
		// document-rejected — this workflow appends neither. Re-reading every
		// reference (not just the signalled one) also recovers any review whose
		// signal was lost entirely, which the old per-signal handling could
		// never notice.
		var states map[string]string
		if err := workflow.ExecuteActivity(ctx, DocumentReviewStateActivity, DocumentReviewStateInput{
			Tenant: input.Tenant, Context: input.Context, OrganizationID: input.OrganizationID,
			References: references,
		}).Get(ctx, &states); err != nil {
			return VettingResult{}, err
		}

		for _, reference := range references {
			switch states[reference] {
			case documentStatusApproved:
				approved[reference] = struct{}{}
				reviews[reference] = profiledomain.DocumentApproved
			case documentStatusRejected:
				reviews[reference] = profiledomain.DocumentRejected
				rejected = true
			}
		}
	}

	if rejected {
		if err := appendEvent(profiledomain.RejectedEvent, ""); err != nil {
			return VettingResult{}, err
		}
		current = result(profiledomain.StatusRejected, false, approved)
		return current, nil
	}

	gitErr := gitFuture.Get(ctx, nil)
	if gitErr != nil {
		for _, reference := range sortedReferences(approved) {
			reviews[reference] = profiledomain.DocumentPendingReview
			if err := appendEvent(profiledomain.DocumentApprovalRevertedEvent, reference); err != nil {
				return VettingResult{}, err
			}
		}
		if err := appendEvent(profiledomain.RejectedEvent, ""); err != nil {
			return VettingResult{}, err
		}
		current = result(profiledomain.StatusRejected, false, approved)
		return current, nil
	}
	if err := appendEvent(profiledomain.GitVerifiedEvent, ""); err != nil {
		return VettingResult{}, err
	}
	if err := appendEvent(profiledomain.VettedEvent, ""); err != nil {
		return VettingResult{}, err
	}
	current = result(profiledomain.StatusVetted, true, approved)

	// BR-TP62: from here the profile is Vetted, and the workflow stays
	// running for exactly as long as that remains true. The cover timer lives
	// inside this call and nowhere else, which is what makes "an armed timer
	// exists if and only if the profile is Vetted" structural rather than a
	// cleanup step someone has to remember.
	//
	// This replaces BR-TP28's per-transporter Temporal Schedule (38h-ii).
	// That polled every 5 minutes forever — 184 executions against 16 vetting
	// workflows on the live stack, growing without bound — to detect an
	// instant that is written down in the document itself.
	return watchCover(ctx, input, approved)
}

// watchCover sleeps until the transporter's goods-in-transit cover lapses,
// re-arming whenever the expiry moves (BR-TP60/BR-TP61). It returns when the
// profile leaves Vetted, which today means exactly one thing: the cover
// lapsed and BR-TP28's drop ran.
func watchCover(ctx workflow.Context, input VettingInput, approved map[string]struct{}) (VettingResult, error) {
	changed := workflow.GetSignalChannel(ctx, CoverChangedSignalName)

	for cycle := 0; cycle < maxCoverCycles; cycle++ {
		var expiresAt *int64
		if err := workflow.ExecuteActivity(ctx, CoverExpiryActivity, CoverExpiryInput{
			Tenant: input.Tenant, Context: input.Context, OrganizationID: input.OrganizationID,
		}).Get(ctx, &expiresAt); err != nil {
			return VettingResult{}, err
		}

		// A cover with no expiry cannot lapse by time, so there is nothing to
		// arm — the run waits on the signal alone rather than polling to ask
		// again. This is the common case until an expiry is actually set
		// (BR-TP59), and it is why the workflow parks instead of completing:
		// a completed run has nothing left to receive the signal.
		lapsed := false
		if expiresAt == nil {
			changed.Receive(ctx, nil)
		} else {
			timerCtx, cancelTimer := workflow.WithCancel(ctx)
			timer := workflow.NewTimer(timerCtx, time.Unix(*expiresAt, 0).UTC().Sub(workflow.Now(ctx)))

			selector := workflow.NewSelector(ctx)
			selector.AddFuture(timer, func(workflow.Future) { lapsed = true })
			selector.AddReceive(changed, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
			selector.Select(ctx)
			// Cancelled on every path, not only the signal one: an un-cancelled
			// timer keeps its place in history and would fire again after a
			// continue-as-new.
			cancelTimer()
		}

		if !lapsed {
			// The expiry moved. Re-read it rather than trusting the signal to
			// carry it — the document is the authority, and a payload can be
			// stale by the time it is delivered.
			continue
		}

		if err := workflow.ExecuteActivity(ctx, HandleGitStatusDropActivity, GitMonitorInput{
			Tenant: input.Tenant, Context: input.Context, OrganizationID: input.OrganizationID,
			ActivityTimeouts: input.ActivityTimeouts,
		}).Get(ctx, nil); err != nil {
			return VettingResult{}, err
		}
		// BR-TP63: the drop moved the profile to CoverLapsed, so Vetted no
		// longer holds and the watch — with it, this run — is over. Getting
		// back to Vetted is the normal resubmit-and-re-vet path (BR-TP26),
		// which starts a fresh run under the same workflow ID.
		return result(profiledomain.StatusCoverLapsed, false, approved), nil
	}

	// History would otherwise grow for the lifetime of the transporter.
	return VettingResult{}, workflow.NewContinueAsNewError(ctx, TransporterVettingWorkflow, VettingInput{
		Tenant: input.Tenant, Context: input.Context, OrganizationID: input.OrganizationID,
		AttemptNumber: input.AttemptNumber, RequiredDocumentReferences: input.RequiredDocumentReferences,
		ActivityTimeouts: input.ActivityTimeouts, Resubmitted: true, AlreadyVetted: true,
	})
}

func cloneReviews(reviews map[string]profiledomain.DocumentReviewStatus) map[string]profiledomain.DocumentReviewStatus {
	clone := make(map[string]profiledomain.DocumentReviewStatus, len(reviews))
	for reference, status := range reviews {
		clone[reference] = status
	}
	return clone
}

func result(status profiledomain.Status, gate bool, approved map[string]struct{}) VettingResult {
	return VettingResult{Status: status, FleetAvailabilityGate: gate, ApprovedDocumentReferences: sortedReferences(approved)}
}

func sortedReferences(references map[string]struct{}) []string {
	out := make([]string, 0, len(references))
	for reference := range references {
		out = append(out, reference)
	}
	sort.Strings(out)
	return out
}

func isSequenceConflict(err error) bool {
	if err == nil {
		return false
	}
	var appErr *temporal.ApplicationError
	return (errors.As(err, &appErr) && appErr.Type() == SequenceConflictErrorType) ||
		containsSequenceConflict(err.Error())
}

func containsSequenceConflict(message string) bool {
	return strings.Contains(message, "sequence conflict")
}
