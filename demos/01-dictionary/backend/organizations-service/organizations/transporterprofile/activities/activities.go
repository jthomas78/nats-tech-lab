// Package activities provides all non-deterministic I/O used by the
// transporter Temporal workflows.
package activities

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/workflow"
)

type WorkflowEventPublisher interface {
	AppendWorkflowEvent(context.Context, profiledomain.Event, string) error
}

// PublisherResolver hands back the event publisher for one tenant's NATS
// connection. BR-TP58: activities resolve their connection per invocation
// from the Tenant on their own input, never from one bound at construction.
//
// workflow.TaskQueue is a single constant, so every tenant's tasks arrive at
// the same worker — a bound publisher would send one tenant's events over
// another tenant's connection. internal/tenants.Manager implements this,
// alongside its existing DocumentStore/SecretStore/ProfileCommands
// resolvers, and returns ErrTenantNotConnected for a tenant this service
// holds no credential for.
type PublisherResolver interface {
	ProfileEvents(tenant string) (WorkflowEventPublisher, error)
}
type GitVerifier interface {
	Verify(context.Context, profileworkflow.GitVerificationInput) error
}
type GitStatusDropCommand interface {
	HandleGitStatusDrop(context.Context, string) error
}

// GitStatusDropResolver returns the drop command bound to one tenant *and*
// one context. Unlike PublisherResolver's single key, the handler appends to
// the tenant's stream under a specific {context}, so both axes are needed —
// orchestration.NewGitStatusDropHandler takes the contextKey at construction.
type GitStatusDropResolver interface {
	GitStatusDrop(tenant, contextKey string) (GitStatusDropCommand, error)
}

type GitStatusReader interface {
	IsGitActive(context.Context, string) (bool, error)
}

// CoverExpiryReader answers BR-TP60's "when does this transporter's cover
// lapse" — the earliest expiry across its current goods-in-transit documents,
// or nil when none of them can lapse by time.
type CoverExpiryReader interface {
	CoverExpiry(ctx context.Context, organizationID string) (*int64, error)
}

// DocumentReviewReader answers decision 14's "what does storage actually say
// about these documents" — the question that replaced trusting the review
// signal's payload. Values are organizations domain DocumentStatus strings,
// and a reference with no row is simply absent from the map rather than
// defaulted, so "not written yet" and "written as PENDING" stay distinct.
type DocumentReviewReader interface {
	DocumentReviewState(ctx context.Context, organizationID string, references []string) (map[string]string, error)
}

type ProfileActivities struct {
	publishers PublisherResolver
	verifier   GitVerifier
	drops      GitStatusDropResolver
	gitStatus  GitStatusReader
	expiries   CoverExpiryReader
	reviews    DocumentReviewReader
}

func NewProfileActivities(publishers PublisherResolver, verifier GitVerifier) *ProfileActivities {
	return &ProfileActivities{publishers: publishers, verifier: verifier}
}

func (a *ProfileActivities) WithGitStatusDropCommand(drops GitStatusDropResolver) *ProfileActivities {
	a.drops = drops
	return a
}

// WithCoverExpiryReader supplies BR-TP60's expiry lookup.
func (a *ProfileActivities) WithCoverExpiryReader(expiries CoverExpiryReader) *ProfileActivities {
	a.expiries = expiries
	return a
}

// CoverExpiry is what the vetting workflow arms its timer against (BR-TP60).
// Unconfigured it fails closed rather than returning nil — a nil expiry means
// "this cover cannot lapse by time", and answering that by accident would
// leave a vetted transporter permanently unwatched.
func (a *ProfileActivities) CoverExpiry(ctx context.Context, input profileworkflow.CoverExpiryInput) (*int64, error) {
	if a.expiries == nil {
		return nil, errors.New("cover expiry reader is not configured")
	}
	return a.expiries.CoverExpiry(ctx, input.OrganizationID)
}

// WithDocumentReviewReader supplies decision 14's persisted-state lookup.
func (a *ProfileActivities) WithDocumentReviewReader(reviews DocumentReviewReader) *ProfileActivities {
	a.reviews = reviews
	return a
}

// DocumentReviewState is the activity behind the vetting workflow's read of
// its own required documents. Unconfigured it fails closed for the same
// reason CoverExpiry does: answering "nothing is approved" by accident would
// park an attempt forever on reviews that had already happened.
func (a *ProfileActivities) DocumentReviewState(ctx context.Context, input profileworkflow.DocumentReviewStateInput) (map[string]string, error) {
	if a.reviews == nil {
		return nil, errors.New("document review reader is not configured")
	}
	return a.reviews.DocumentReviewState(ctx, input.OrganizationID, input.References)
}

func (a *ProfileActivities) WithGitStatusReader(reader GitStatusReader) *ProfileActivities {
	a.gitStatus = reader
	return a
}

func (a *ProfileActivities) AppendProfileEvent(ctx context.Context, input profileworkflow.ProfileEventInput) error {
	event := input.Event
	event.Step = input.Step
	messageID := fmt.Sprintf("%s:%s:%d:%d", event.OrganizationID, event.Type, event.AttemptNumber, input.Step)
	// Resolved per invocation, and the error is returned before anything is
	// appended: an activity naming a tenant we hold no credential for writes
	// nothing rather than falling back to another connection (BR-TP58).
	publisher, err := a.publishers.ProfileEvents(input.Tenant)
	if err != nil {
		return err
	}
	if err := publisher.AppendWorkflowEvent(ctx, event, messageID); err != nil {
		if strings.Contains(err.Error(), "sequence conflict") {
			return temporal.NewNonRetryableApplicationError(err.Error(), profileworkflow.SequenceConflictErrorType, err)
		}
		return err
	}
	return nil
}

func (a *ProfileActivities) RequestGitVerification(ctx context.Context, input profileworkflow.GitVerificationInput) error {
	if a.verifier == nil {
		return nil
	}
	return a.verifier.Verify(ctx, input)
}

func (a *ProfileActivities) HandleGitStatusDrop(ctx context.Context, input profileworkflow.GitMonitorInput) error {
	if a.gitStatus != nil {
		active, err := a.gitStatus.IsGitActive(ctx, input.OrganizationID)
		if err != nil {
			return err
		}
		if active {
			return nil
		}
	}
	if a.drops == nil {
		return errors.New("GIT status drop command is not configured")
	}
	// Resolved per invocation from the input's own tenant/context, for the
	// same reason AppendProfileEvent is (BR-TP58): one worker serves every
	// tenant off one task queue.
	drop, err := a.drops.GitStatusDrop(input.Tenant, input.Context)
	if err != nil {
		return err
	}
	return drop.HandleGitStatusDrop(ctx, input.OrganizationID)
}

type MockGitOutcome string

const (
	GitPass    MockGitOutcome = "pass"
	GitFail    MockGitOutcome = "fail"
	GitTimeout MockGitOutcome = "timeout"
)

type MockGitVerifier struct {
	Outcome MockGitOutcome
	Delay   time.Duration
}

func (v MockGitVerifier) Verify(ctx context.Context, _ profileworkflow.GitVerificationInput) error {
	switch v.Outcome {
	case GitFail:
		return errors.New("GIT verification rejected")
	case GitTimeout:
		delay := v.Delay
		if delay <= 0 {
			delay = 24 * time.Hour
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			return errors.New("GIT verification timed out")
		}
	default:
		return nil
	}
}
