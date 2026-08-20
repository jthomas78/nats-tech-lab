package worker_test

import (
	"context"
	"reflect"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	profileworker "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/worker"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/workflow"
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
			commands := &fakeResubmitter{order: &order, state: profiledomain.State{Context: "acme", ID: "partner-1", Status: profiledomain.StatusDocumentsInReview, AttemptNumber: 2}}
			executor := &fakeWorkflowExecutor{order: &order}
			service := profileworker.NewVettingService(executor, commands, profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second})

			Expect(service.Resubmit(context.Background(), "acme", "partner-1", []string{"doc-a"})).To(Succeed())
			Expect(order).To(HaveExactElements("append", "start"))
			Expect(executor.options.ID).To(Equal("acme-transporter-vetting-partner-1"))
			Expect(executor.options.WorkflowIDReusePolicy).To(Equal(enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE))
			Expect(executor.input.AttemptNumber).To(Equal(2))
			Expect(executor.input.Resubmitted).To(BeTrue())
		})
	})

	Context("BR-TP28 monitor creation uses a Temporal Schedule", func() {
		It("builds an interval Schedule action rather than legacy CronSchedule", func() {
			options, err := profileworker.GitMonitorScheduleOptions("acme", "partner-1", 24*time.Hour, profileworkflow.ActivityTimeouts{StartToClose: time.Second, ScheduleToClose: 2 * time.Second})
			Expect(err).NotTo(HaveOccurred())
			Expect(options.Spec.Intervals).To(HaveExactElements(client.ScheduleIntervalSpec{Every: 24 * time.Hour}))
			action, ok := options.Action.(*client.ScheduleWorkflowAction)
			Expect(ok).To(BeTrue())
			Expect(action.TaskQueue).To(Equal(profileworkflow.TaskQueue))
			Expect(reflect.ValueOf(action.Workflow).Pointer()).To(Equal(reflect.ValueOf(profileworkflow.TransporterGitMonitorWorkflow).Pointer()))
		})
	})
})
