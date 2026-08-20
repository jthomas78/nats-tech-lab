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

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/workflow"
)

type WorkflowEventPublisher interface {
	AppendWorkflowEvent(context.Context, profiledomain.Event, string) error
}
type GitVerifier interface {
	Verify(context.Context, profileworkflow.GitVerificationInput) error
}
type GitStatusDropCommand interface {
	HandleGitStatusDrop(context.Context, string) error
}
type GitStatusReader interface {
	IsGitActive(context.Context, string) (bool, error)
}

type ProfileActivities struct {
	publisher WorkflowEventPublisher
	verifier  GitVerifier
	drop      GitStatusDropCommand
	gitStatus GitStatusReader
}

func NewProfileActivities(publisher WorkflowEventPublisher, verifier GitVerifier) *ProfileActivities {
	return &ProfileActivities{publisher: publisher, verifier: verifier}
}

func (a *ProfileActivities) WithGitStatusDropCommand(command GitStatusDropCommand) *ProfileActivities {
	a.drop = command
	return a
}

func (a *ProfileActivities) WithGitStatusReader(reader GitStatusReader) *ProfileActivities {
	a.gitStatus = reader
	return a
}

func (a *ProfileActivities) AppendProfileEvent(ctx context.Context, input profileworkflow.ProfileEventInput) error {
	event := input.Event
	event.Step = input.Step
	messageID := fmt.Sprintf("%s:%s:%d:%d", event.TradingPartnerID, event.Type, event.AttemptNumber, input.Step)
	if err := a.publisher.AppendWorkflowEvent(ctx, event, messageID); err != nil {
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
		active, err := a.gitStatus.IsGitActive(ctx, input.TradingPartnerID)
		if err != nil {
			return err
		}
		if active {
			return nil
		}
	}
	if a.drop == nil {
		return errors.New("GIT status drop command is not configured")
	}
	return a.drop.HandleGitStatusDrop(ctx, input.TradingPartnerID)
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
