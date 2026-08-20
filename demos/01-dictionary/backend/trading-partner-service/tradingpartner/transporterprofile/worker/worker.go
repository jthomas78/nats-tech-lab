// Package worker wires transporter workflows and activities to Temporal and
// exposes the command facade used to start/resubmit them.
package worker

import (
	"context"
	"errors"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/workflow"
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
}

func NewVettingService(executor WorkflowExecutor, commands ResubmitCommands, timeouts profileworkflow.ActivityTimeouts) *VettingService {
	return &VettingService{executor: executor, commands: commands, timeouts: timeouts}
}

func (s *VettingService) Resubmit(ctx context.Context, contextKey, tradingPartnerID string, required []string) error {
	if err := s.timeouts.Validate(); err != nil {
		return err
	}
	state, err := s.commands.Resubmit(ctx, contextKey, tradingPartnerID)
	if err != nil {
		return err
	}
	_, err = s.executor.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: workflowID(contextKey, tradingPartnerID), TaskQueue: profileworkflow.TaskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, profileworkflow.TransporterVettingWorkflow, profileworkflow.VettingInput{
		Context: contextKey, TradingPartnerID: tradingPartnerID, AttemptNumber: state.AttemptNumber,
		RequiredDocumentReferences: required, ActivityTimeouts: s.timeouts, Resubmitted: true,
	})
	return err
}

func workflowID(contextKey, tradingPartnerID string) string {
	return contextKey + "-transporter-vetting-" + tradingPartnerID
}

func GitMonitorScheduleOptions(contextKey, tradingPartnerID string, interval time.Duration, timeouts profileworkflow.ActivityTimeouts) (client.ScheduleOptions, error) {
	if interval <= 0 {
		return client.ScheduleOptions{}, errors.New("GIT monitor interval must be positive")
	}
	if err := timeouts.Validate(); err != nil {
		return client.ScheduleOptions{}, err
	}
	return client.ScheduleOptions{
		ID:   contextKey + "-transporter-git-monitor-" + tradingPartnerID,
		Spec: client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: interval}}},
		Action: &client.ScheduleWorkflowAction{
			ID:       contextKey + "-transporter-git-monitor-run-" + tradingPartnerID,
			Workflow: profileworkflow.TransporterGitMonitorWorkflow, TaskQueue: profileworkflow.TaskQueue,
			Args: []interface{}{profileworkflow.GitMonitorInput{Context: contextKey, TradingPartnerID: tradingPartnerID, ActivityTimeouts: timeouts}},
		},
	}, nil
}

func CreateGitMonitorSchedule(ctx context.Context, temporalClient client.Client, contextKey, tradingPartnerID string, interval time.Duration, timeouts profileworkflow.ActivityTimeouts) (client.ScheduleHandle, error) {
	options, err := GitMonitorScheduleOptions(contextKey, tradingPartnerID, interval, timeouts)
	if err != nil {
		return nil, err
	}
	return temporalClient.ScheduleClient().Create(ctx, options)
}

type Activities interface {
	AppendProfileEvent(context.Context, profileworkflow.ProfileEventInput) error
	RequestGitVerification(context.Context, profileworkflow.GitVerificationInput) error
	HandleGitStatusDrop(context.Context, profileworkflow.GitMonitorInput) error
}

func New(temporalClient client.Client, activities Activities) temporalworker.Worker {
	w := temporalworker.New(temporalClient, profileworkflow.TaskQueue, temporalworker.Options{})
	w.RegisterWorkflow(profileworkflow.TransporterVettingWorkflow)
	w.RegisterWorkflow(profileworkflow.TransporterGitMonitorWorkflow)
	w.RegisterActivityWithOptions(activities.AppendProfileEvent, activity.RegisterOptions{Name: profileworkflow.AppendProfileEventActivity})
	w.RegisterActivityWithOptions(activities.RequestGitVerification, activity.RegisterOptions{Name: profileworkflow.RequestGitVerificationActivity})
	w.RegisterActivityWithOptions(activities.HandleGitStatusDrop, activity.RegisterOptions{Name: profileworkflow.HandleGitStatusDropActivity})
	return w
}
