package dictionary

// Regression tests for hydration consumer cleanup.
//
// Every write command replays the SHIPPING stream through an OrderedConsumer.
// Ordered consumers are ephemeral, but "ephemeral" only means the server
// reaps them after their 5m InactiveThreshold — stopping the client-side pull
// does NOT remove them. Left alone, each command therefore holds a consumer
// slot for five minutes, and a tenant account's JetStream MaxConsumers limit
// (20, three of which are the durable projectors) is exhausted after ~17 writes:
//
//	nats: API error: code=400 err_code=10026 description=maximum consumers limit reached
//
// These specs pin the invariant that hydration is consumer-slot-neutral.

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
)

// consumerCount reports how many consumers currently exist on the SHIPPING
// stream. It reads Info (not CachedInfo) so the count is server-side truth.
func consumerCount(ctx context.Context, js jetstream.JetStream) int {
	GinkgoHelper()
	stream, err := js.Stream(ctx, domain.StreamName)
	Expect(err).NotTo(HaveOccurred())
	info, err := stream.Info(ctx)
	Expect(err).NotTo(HaveOccurred())
	return info.State.Consumers
}

var _ = Describe("Hydration consumer cleanup", func() {
	var (
		ctx        context.Context
		js         jetstream.JetStream
		ship       *commands.ShipHandler
		containers *commands.ContainerHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		js = newJetStream()
		pub := jstream.NewPublisher(js)
		ports := newFakePortRepo()
		ship = commands.NewShipHandler(pub, js, ports)
		containers = commands.NewContainerHandler(pub, js, ports)

		Expect(consumerCount(ctx, js)).To(Equal(0), "stream should start with no consumers")
	})

	Context("ship commands", func() {
		It("leaves no consumer behind after a run of arrivals and departures", func() {
			// Comfortably more commands than the 16 free slots a tenant
			// account has — this is the run that used to fail outright.
			for i := 0; i < 25; i++ {
				shipID := fmt.Sprintf("MV-SEED-%02d", i)
				_, err := ship.ArrivePort(ctx, commands.ShipInput{
					Context: "acme-atlantic-fleet", ShipID: shipID,
					ShipName: shipID, Port: "Hamburg",
				})
				Expect(err).NotTo(HaveOccurred())
				_, err = ship.DepartPort(ctx, commands.ShipInput{
					Context: "acme-atlantic-fleet", ShipID: shipID, Port: "Hamburg",
				})
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(consumerCount(ctx, js)).To(Equal(0))
		})

		It("leaves no consumer behind when the command fails on a domain rule", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: "acme-atlantic-fleet", ShipID: "MV-NORDWIND",
				ShipName: "MV Nordwind", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())

			// BR-001: already docked here.
			_, err = ship.ArrivePort(ctx, commands.ShipInput{
				Context: "acme-atlantic-fleet", ShipID: "MV-NORDWIND", Port: "Hamburg",
			})
			Expect(err).To(HaveOccurred())

			Expect(consumerCount(ctx, js)).To(Equal(0))
		})
	})

	Context("container commands", func() {
		It("leaves no consumer behind across register/load/unload", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: "acme-atlantic-fleet", ShipID: "MV-NORDWIND",
				ShipName: "MV Nordwind", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())

			for i := 1; i <= 10; i++ {
				containerID := fmt.Sprintf("TCKU%07d", i)
				_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
					Context: "acme-atlantic-fleet", ContainerID: containerID,
					Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Rotterdam",
				})
				Expect(err).NotTo(HaveOccurred())
				_, err = containers.LoadContainer(ctx, commands.ContainerInput{
					Context: "acme-atlantic-fleet", ContainerID: containerID, ShipID: "MV-NORDWIND",
				})
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(consumerCount(ctx, js)).To(Equal(0))
		})
	})
})
