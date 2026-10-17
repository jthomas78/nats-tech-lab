package dictionary

// BR-037 (Phase 28d, moved to OTel in Phase 54d): each of the three
// eventhandler Consume callbacks (handler.go's shared register(), used by
// RegisterShips; container_handler.go's RegisterContainers; meta_handler.go's
// RegisterMeta) makes exactly one CONSUMER span per message it processes,
// with entity_id and consumer.durable attributes. The api.* → evt.* half of
// the async tail, and the two-projector durable split, are specified in
// browserrpc_otel_test.go. Uses newNatsConnAndJS() (browserrpc_test.go) for a
// real embedded NATS/JetStream server.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

var _ = Describe("BR-037: each JetStream projector Consume callback is exactly one CONSUMER span per message", func() {
	const fleetCtx = "acme-pacific-fleet"

	var (
		ctx  context.Context
		nc   *nats.Conn
		js   jetstream.JetStream
		log  *slog.Logger
		prov *otelnats.Provider
		exp  *tracetest.InMemoryExporter
	)

	BeforeEach(func() {
		ctx = context.Background()
		nc, js = newNatsConnAndJS()
		log = slog.New(slog.DiscardHandler)
		exp = tracetest.NewInMemoryExporter()
		var err error
		prov, err = otelnats.Init(ctx, otelnats.Config{ServiceName: "shipping-service", Exporter: exp})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = prov.Shutdown(context.Background()) })
	})

	// consumers returns the CONSUMER spans whose subject ends in .action. A
	// ship's first arrival also registers it (an extra .registered event), so
	// a spec on one action filters by it.
	consumers := func(action string) []tracetest.SpanStub {
		Expect(prov.ForceFlush(context.Background())).To(Succeed())
		var out []tracetest.SpanStub
		for _, s := range exp.GetSpans() {
			if s.SpanKind == trace.SpanKindConsumer && strings.HasSuffix(s.Name, "."+action) {
				out = append(out, s)
			}
		}
		return out
	}
	attr := func(s tracetest.SpanStub, key string) string {
		for _, kv := range s.Attributes {
			if string(kv.Key) == key {
				return kv.Value.Emit()
			}
		}
		return ""
	}
	// exactlyOne waits for one CONSUMER span for action, then checks that no
	// second one follows — the "exactly one" half of the rule.
	exactlyOne := func(action string) tracetest.SpanStub {
		GinkgoHelper()
		Eventually(func() []tracetest.SpanStub { return consumers(action) }, 3*time.Second, 20*time.Millisecond).Should(HaveLen(1))
		Consistently(func() []tracetest.SpanStub { return consumers(action) }, 300*time.Millisecond, 20*time.Millisecond).Should(HaveLen(1))
		return consumers(action)[0]
	}
	tracing := func() eventhandler.Option { return eventhandler.WithTracing(prov, fleetCtx) }

	It("RegisterShips makes one span for the .arrived event, with an entity_id and its durable", func() {
		kvB := kvstore.New(js, "ships")
		ccB, err := eventhandler.RegisterShips(ctx, js, kvB, nc, newFakeRepo(), log, tracing())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccB.Stop)

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = ships.ArrivePort(ctx, commands.ShipInput{Context: fleetCtx, ShipID: "span-ship-b", ShipName: "Span Ship B", Port: "Hamburg"})
		Expect(err).NotTo(HaveOccurred())

		span := exactlyOne("arrived")
		Expect(attr(span, "entity_id")).NotTo(BeEmpty())
		Expect(attr(span, "consumer.durable")).To(Equal("ship-projector"))
	})

	It("RegisterContainers makes one span for the .registered event, with an entity_id and its durable", func() {
		kvContainers := kvstore.New(js, "container")
		ccCont, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nc, newFakeContainerRepo(), log, tracing())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccCont.Stop)

		containers := commands.NewContainerHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU1112223", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		span := exactlyOne("registered")
		Expect(attr(span, "entity_id")).NotTo(BeEmpty())
		Expect(attr(span, "consumer.durable")).To(Equal("container-projector"))
	})

	It("RegisterMeta makes one span for the .registered event, with an entity_id and its durable", func() {
		// RegisterContainers is deliberately NOT registered here — both
		// consumers read the same evt.*.shipping.container.> subject, and
		// registering both would make "exactly one" ambiguous.
		kvMeta := kvstore.New(js, "meta")
		ccMeta, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nc, log, tracing())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccMeta.Stop)

		containers := commands.NewContainerHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU4445556", Cargo: "grain", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		span := exactlyOne("registered")
		Expect(attr(span, "entity_id")).NotTo(BeEmpty())
		Expect(attr(span, "consumer.durable")).To(Equal("meta-projector"))
	})
})
