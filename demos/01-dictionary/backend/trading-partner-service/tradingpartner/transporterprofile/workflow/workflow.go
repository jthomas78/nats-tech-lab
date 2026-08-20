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

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
)

const (
	TaskQueue                      = "organizations-vetting"
	AppendProfileEventActivity     = "AppendProfileEvent"
	RequestGitVerificationActivity = "RequestGitVerification"
	HandleGitStatusDropActivity    = "HandleGitStatusDrop"
	DocumentReviewSignalName       = "DocumentReview"
	SequenceConflictErrorType      = "TransporterProfileSequenceConflict"
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
	Context                    string
	TradingPartnerID           string
	AttemptNumber              int
	RequiredDocumentReferences []string
	ActivityTimeouts           ActivityTimeouts
	Resubmitted                bool
}

type VettingResult struct {
	Status                     profiledomain.Status
	FleetAvailabilityGate      bool
	ApprovedDocumentReferences []string
}

type DocumentReviewSignal struct {
	Reference string
	Approved  bool
}
type ProfileEventInput struct {
	Event profiledomain.Event
	Step  int
}
type GitVerificationInput struct {
	Context, TradingPartnerID string
	AttemptNumber             int
}
type GitMonitorInput struct {
	Context, TradingPartnerID string
	ActivityTimeouts          ActivityTimeouts
}

func TransporterVettingWorkflow(ctx workflow.Context, input VettingInput) (VettingResult, error) {
	if err := input.ActivityTimeouts.Validate(); err != nil {
		return VettingResult{}, err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    input.ActivityTimeouts.StartToClose,
		ScheduleToCloseTimeout: input.ActivityTimeouts.ScheduleToClose,
		RetryPolicy:            &temporal.RetryPolicy{MaximumAttempts: 1},
	})
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
			Type: eventType, Context: input.Context, TradingPartnerID: input.TradingPartnerID,
			AttemptNumber: input.AttemptNumber, Step: step, DocumentReference: reference,
			DocumentReviews: cloneReviews(reviews),
			OccurredAt:      workflow.Now(ctx).UTC(),
		}
		switch eventType {
		case profiledomain.VettingStartedEvent, profiledomain.VettingResubmittedEvent:
			event.Status = profiledomain.StatusDocumentsInReview
		case profiledomain.VettedEvent:
			event.Status, event.FleetAvailabilityGate = profiledomain.StatusVetted, true
			event.GitVerified = true
		case profiledomain.GitVerifiedEvent:
			event.GitVerified = true
		case profiledomain.RejectedEvent:
			event.Status = profiledomain.StatusRejected
		}
		err := workflow.ExecuteActivity(ctx, AppendProfileEventActivity, ProfileEventInput{Event: event, Step: step}).Get(ctx, nil)
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
		Context: input.Context, TradingPartnerID: input.TradingPartnerID, AttemptNumber: input.AttemptNumber,
	})

	signals := workflow.GetSignalChannel(ctx, DocumentReviewSignalName)
	rejected := false
	for len(approved) < len(required) && !rejected {
		var signal DocumentReviewSignal
		signals.Receive(ctx, &signal)
		if _, ok := required[signal.Reference]; !ok {
			continue
		}
		if !signal.Approved {
			reviews[signal.Reference] = profiledomain.DocumentRejected
			if err := appendEvent(profiledomain.DocumentRejectedEvent, signal.Reference); err != nil {
				return VettingResult{}, err
			}
			rejected = true
			break
		}
		if _, duplicate := approved[signal.Reference]; duplicate {
			continue
		}
		approved[signal.Reference] = struct{}{}
		reviews[signal.Reference] = profiledomain.DocumentApproved
		if err := appendEvent(profiledomain.DocumentApprovedEvent, signal.Reference); err != nil {
			return VettingResult{}, err
		}
	}

	if rejected {
		if err := appendEvent(profiledomain.RejectedEvent, ""); err != nil {
			return VettingResult{}, err
		}
		return result(profiledomain.StatusRejected, false, approved), nil
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
		return result(profiledomain.StatusRejected, false, approved), nil
	}
	if err := appendEvent(profiledomain.GitVerifiedEvent, ""); err != nil {
		return VettingResult{}, err
	}
	if err := appendEvent(profiledomain.VettedEvent, ""); err != nil {
		return VettingResult{}, err
	}
	return result(profiledomain.StatusVetted, true, approved), nil
}

func cloneReviews(reviews map[string]profiledomain.DocumentReviewStatus) map[string]profiledomain.DocumentReviewStatus {
	clone := make(map[string]profiledomain.DocumentReviewStatus, len(reviews))
	for reference, status := range reviews {
		clone[reference] = status
	}
	return clone
}

func TransporterGitMonitorWorkflow(ctx workflow.Context, input GitMonitorInput) error {
	if err := input.ActivityTimeouts.Validate(); err != nil {
		return err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: input.ActivityTimeouts.StartToClose, ScheduleToCloseTimeout: input.ActivityTimeouts.ScheduleToClose, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
	return workflow.ExecuteActivity(ctx, HandleGitStatusDropActivity, input).Get(ctx, nil)
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
