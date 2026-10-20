package dictionary

// BR-082 (Phase 57): the shipping projector's two base contexts. This is a
// characterization spec: it records what the code did before Phase 57 moved
// the span into otelnats.Consume, and must stay green after it. Phase 110
// owns any change to it.
//
// Each projector builds msgCtx as context.WithoutCancel(registration ctx).
// The untraced path runs on msgCtx; the traced path runs on
// context.Background(). So neither is cancelled with the registration ctx,
// and only the untraced one carries its values.

import (
	"context"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// regValueKey is a value set on the registration ctx.
type regValueKey struct{}

// ctxRepo is fakeRepo that hands each Upsert's ctx to the spec.
type ctxRepo struct {
	*fakeRepo
	seen chan context.Context
}

func (r *ctxRepo) Upsert(ctx context.Context, state domain.ShipState) (domain.ShipState, error) {
	r.seen <- ctx
	return r.fakeRepo.Upsert(ctx, state)
}

var _ = Describe("BR-082: the shipping projector keeps its traced and untraced base contexts (characterization, Phase 110)", func() {
	const fleetCtx = "acme-pacific-fleet"

	// projectOne registers the ship projector on a cancellable registration
	// ctx that carries a value, cancels that ctx, then projects one arrival
	// and returns the ctx the projection ran on.
	projectOne := func(opts ...eventhandler.Option) context.Context {
		GinkgoHelper()
		nc, js := newNatsConnAndJS()
		repo := &ctxRepo{fakeRepo: newFakeRepo(), seen: make(chan context.Context, 8)}

		regCtx, cancel := context.WithCancel(context.WithValue(context.Background(), regValueKey{}, "registration"))
		cc, err := eventhandler.RegisterShips(regCtx, js, kvstore.New(js, "ships"), nc, repo, slog.New(slog.DiscardHandler), opts...)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cc.Stop)
		cancel()

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = ships.ArrivePort(context.Background(), commands.ShipInput{Context: fleetCtx, ShipID: "ctx-ship", ShipName: "Ctx Ship", Port: "Hamburg"})
		Expect(err).NotTo(HaveOccurred())

		var got context.Context
		Eventually(repo.seen, 3*time.Second).Should(Receive(&got))
		return got
	}

	Context("untraced", func() {
		It("is not cancelled with the registration ctx, and carries its values", func() {
			got := projectOne()
			Expect(got.Err()).NotTo(HaveOccurred())
			Expect(got.Done()).To(BeNil(), "the message ctx can never be cancelled")
			Expect(got.Value(regValueKey{})).To(Equal("registration"))
		})
	})

	Context("traced", func() {
		It("is not cancelled with the registration ctx, and does not carry its values", func() {
			prov, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "shipping-service", Exporter: tracetest.NewInMemoryExporter()})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = prov.Shutdown(context.Background()) })

			got := projectOne(eventhandler.WithTracing(prov, fleetCtx))
			Expect(got.Err()).NotTo(HaveOccurred())
			Expect(got.Done()).To(BeNil(), "the message ctx can never be cancelled")
			Expect(got.Value(regValueKey{})).To(BeNil())
		})
	})
})
