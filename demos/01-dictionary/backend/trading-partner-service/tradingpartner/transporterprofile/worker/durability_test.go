package worker_test

import (
	"context"
	"os"
	"sync"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	profileworkflow "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/workflow"
)

type durableActivities struct {
	mu     sync.Mutex
	events []profiledomain.Event
}

func (a *durableActivities) append(_ context.Context, input profileworkflow.ProfileEventInput) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, event := range a.events {
		if event.TradingPartnerID == input.Event.TradingPartnerID && event.Type == input.Event.Type && event.AttemptNumber == input.Event.AttemptNumber && event.Step == input.Step {
			return nil
		}
	}
	event := input.Event
	event.Step = input.Step
	a.events = append(a.events, event)
	return nil
}

func (*durableActivities) verifyGIT(context.Context, profileworkflow.GitVerificationInput) error {
	return nil
}

func (a *durableActivities) documentApprovalCount(reference string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, event := range a.events {
		if event.Type == profiledomain.DocumentApprovedEvent && event.DocumentReference == reference {
			count++
		}
	}
	return count
}

func registerDurabilityWorker(w temporalworker.Worker, activities *durableActivities) {
	w.RegisterWorkflow(profileworkflow.TransporterVettingWorkflow)
	w.RegisterActivityWithOptions(activities.append, activity.RegisterOptions{Name: profileworkflow.AppendProfileEventActivity})
	w.RegisterActivityWithOptions(activities.verifyGIT, activity.RegisterOptions{Name: profileworkflow.RequestGitVerificationActivity})
}

var _ = Describe("Temporal durability harness", func() {
	Context("BR-TP27 worker restart preserves workflow progress", func() {
		It("stops one worker, starts another, and completes without re-sending satisfied signals", func() {
			address := os.Getenv("TEMPORAL_TEST_ADDRESS")
			if address == "" {
				Skip("set TEMPORAL_TEST_ADDRESS to run the compose-backed worker restart harness")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			temporalClient, err := client.Dial(client.Options{HostPort: address})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(temporalClient.Close)

			taskQueue := "organizations-vetting-durability-" + time.Now().UTC().Format("150405.000000000")
			activities := &durableActivities{}
			workerOne := temporalworker.New(temporalClient, taskQueue, temporalworker.Options{})
			registerDurabilityWorker(workerOne, activities)
			Expect(workerOne.Start()).To(Succeed())

			workflowID := "durability-" + time.Now().UTC().Format("150405.000000000")
			run, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflowID, TaskQueue: taskQueue}, profileworkflow.TransporterVettingWorkflow, profileworkflow.VettingInput{
				Context:                    "acme",
				TradingPartnerID:           "partner-durable",
				AttemptNumber:              1,
				RequiredDocumentReferences: []string{"doc-a", "doc-b"},
				ActivityTimeouts:           profileworkflow.ActivityTimeouts{StartToClose: 2 * time.Second, ScheduleToClose: 3 * time.Second},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(temporalClient.SignalWorkflow(ctx, workflowID, run.GetRunID(), profileworkflow.DocumentReviewSignalName, profileworkflow.DocumentReviewSignal{Reference: "doc-a", Approved: true})).To(Succeed())
			Eventually(func() int { return activities.documentApprovalCount("doc-a") }, 10*time.Second).Should(Equal(1))

			workerOne.Stop()
			workerTwo := temporalworker.New(temporalClient, taskQueue, temporalworker.Options{})
			registerDurabilityWorker(workerTwo, activities)
			Expect(workerTwo.Start()).To(Succeed())
			DeferCleanup(workerTwo.Stop)

			Expect(temporalClient.SignalWorkflow(ctx, workflowID, run.GetRunID(), profileworkflow.DocumentReviewSignalName, profileworkflow.DocumentReviewSignal{Reference: "doc-b", Approved: true})).To(Succeed())
			var result profileworkflow.VettingResult
			Expect(run.Get(ctx, &result)).To(Succeed())
			Expect(result.Status).To(Equal(profiledomain.StatusVetted))
			Expect(result.ApprovedDocumentReferences).To(ConsistOf("doc-a", "doc-b"))
			Expect(activities.documentApprovalCount("doc-a")).To(Equal(1))
		})
	})
})
