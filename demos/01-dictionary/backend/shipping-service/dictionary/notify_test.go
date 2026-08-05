package dictionary

// Tests for Phase 15b's notify.* fire-and-forget publishes — the projector
// side of the browser reactive-update story (api.* commands are
// browserrpc_test.go's concern; this file is purely "does a KV-changing
// event also notify correctly"). Uses newNatsConnAndJS() (browserrpc_test.go)
// since these publishes need a real *nats.Conn, not just a
// jetstream.JetStream.

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// subscribeNotify subscribes to subject and returns a channel of raw message
// payloads, unsubscribing on cleanup.
func subscribeNotify(nc *nats.Conn, subject string) chan []byte {
	GinkgoHelper()
	out := make(chan []byte, 8)
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) { out <- m.Data })
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })
	return out
}

// waitForNotify drains ch until a payload satisfies match, or fails the
// spec after 3 seconds. A ship's first arrival fires the projector twice
// (an implicit .registered event with no port yet, then .arrived) — each
// one notifies — so a test asserting on the arrived state can't just take
// the first message off the channel.
func waitForNotify(ch chan []byte, match func([]byte) bool) []byte {
	GinkgoHelper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case payload := <-ch:
			if match(payload) {
				return payload
			}
		case <-deadline:
			Fail("timed out waiting for a matching notify payload")
			return nil
		}
	}
}

var _ = Describe("notify.* publishes (Phase 15b)", func() {
	const fleetCtx = "acme-pacific-fleet"

	var (
		ctx context.Context
		nc  *nats.Conn
		js  jetstream.JetStream
		log *slog.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		nc, js = newNatsConnAndJS()
		log = slog.New(slog.DiscardHandler)
	})

	It("publishes notify.{context}.shipping.ship.changed with the full ShipState after Shape A projects an arrive", func() {
		kvA := kvstore.New(js, "dict-a")
		cc, err := eventhandler.RegisterShapeA(ctx, js, kvA, nc, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cc.Stop)

		notifyCh := subscribeNotify(nc, "notify."+fleetCtx+".shipping.ship.changed")

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = ships.ArrivePort(ctx, commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
		Expect(err).NotTo(HaveOccurred())

		var state domain.ShipState
		payload := waitForNotify(notifyCh, func(data []byte) bool {
			return json.Unmarshal(data, &state) == nil && state.CurrentPort == "Hamburg"
		})
		Expect(json.Unmarshal(payload, &state)).To(Succeed())
		Expect(state.ShipID).To(Equal("orient-express"))
		Expect(state.Context).To(Equal(fleetCtx))
	})

	It("publishes notify.{context}.shipping.raw.ship.{event} with the raw event alongside the projected-state notify (Phase 23)", func() {
		kvA := kvstore.New(js, "dict-a")
		cc, err := eventhandler.RegisterShapeA(ctx, js, kvA, nc, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cc.Stop)

		notifyCh := subscribeNotify(nc, "notify."+fleetCtx+".shipping.raw.ship.arrived")

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = ships.ArrivePort(ctx, commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
		Expect(err).NotTo(HaveOccurred())

		var payload []byte
		Eventually(notifyCh, 3*time.Second).Should(Receive(&payload))
		var event domain.ShipEvent
		Expect(json.Unmarshal(payload, &event)).To(Succeed())
		Expect(event.ShipID).To(Equal("orient-express"))
		Expect(event.Port).To(Equal("Hamburg"))
	})

	It("publishes notify.{context}.shipping.container.changed with the full ContainerState after the container projector runs", func() {
		kvContainers := kvstore.New(js, "container")
		cc, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nc, newFakeContainerRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cc.Stop)

		notifyCh := subscribeNotify(nc, "notify."+fleetCtx+".shipping.container.changed")

		portRepo := newFakePortRepo()
		containers := commands.NewContainerHandler(jstream.NewPublisher(js), js, portRepo)
		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU1234567", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		var payload []byte
		Eventually(notifyCh, 3*time.Second).Should(Receive(&payload))
		var state domain.ContainerState
		Expect(json.Unmarshal(payload, &state)).To(Succeed())
		Expect(state.ContainerID).To(Equal("TCKU1234567"))
		Expect(state.TerminalPort).NotTo(BeNil())
		Expect(*state.TerminalPort).To(Equal("Hamburg"))
	})

	It("publishes notify.{context}.shipping.raw.container.{event} with the raw event alongside the projected-state notify (Phase 23)", func() {
		kvContainers := kvstore.New(js, "container")
		cc, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nc, newFakeContainerRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cc.Stop)

		notifyCh := subscribeNotify(nc, "notify."+fleetCtx+".shipping.raw.container.registered")

		portRepo := newFakePortRepo()
		containers := commands.NewContainerHandler(jstream.NewPublisher(js), js, portRepo)
		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU9998887", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		var payload []byte
		Eventually(notifyCh, 3*time.Second).Should(Receive(&payload))
		var event domain.ContainerEvent
		Expect(json.Unmarshal(payload, &event)).To(Succeed())
		Expect(event.ContainerID).To(Equal("TCKU9998887"))
	})

	It("publishes notify.{context}.shipping.meta.changed with the updated known-containers array when a new container is registered", func() {
		kvContainers := kvstore.New(js, "container")
		kvMeta := kvstore.New(js, "meta")
		ccCont, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nil, newFakeContainerRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccCont.Stop)
		ccMeta, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nc, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccMeta.Stop)

		notifyCh := subscribeNotify(nc, "notify."+fleetCtx+".shipping.meta.changed")

		containers := commands.NewContainerHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU7654321", Cargo: "grain", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		var payload []byte
		Eventually(notifyCh, 3*time.Second).Should(Receive(&payload))
		var values []string
		Expect(json.Unmarshal(payload, &values)).To(Succeed())
		Expect(values).To(ContainElement("TCKU7654321"))
	})

	It("does not publish or panic when nc is nil (nil-safe, matching this repo's Deps convention)", func() {
		kvA := kvstore.New(js, "dict-a")
		var noNC *nats.Conn
		cc, err := eventhandler.RegisterShapeA(ctx, js, kvA, noNC, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cc.Stop)

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		Expect(func() {
			_, err := ships.ArrivePort(ctx, commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			Expect(err).NotTo(HaveOccurred())
		}).NotTo(Panic())
	})

	It("publishes notify.{context}.shipping.port.changed from the browserrpc adapter after port.register.v1", func() {
		portRepo := newFakePortRepo()
		ports := commands.NewPortHandler(portRepo)
		adapter, err := browserrpc.New(nc, browserrpc.Deps{Ports: ports, Log: log})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		notifyCh := subscribeNotify(nc, "notify."+fleetCtx+".shipping.port.changed")

		reqBody, err := json.Marshal(map[string]string{"name": "Singapore"})
		Expect(err).NotTo(HaveOccurred())
		_, err = nc.Request("api."+fleetCtx+".shipping.port.register.v1", reqBody, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var payload []byte
		Eventually(notifyCh, 3*time.Second).Should(Receive(&payload))
		// Bare array — same wire shape as notify.*.shipping.meta.changed, not
		// the {"values": [...]} envelope api.*.shipping.port.list.v1 uses (see
		// publishPortsChanged's doc comment).
		var values []string
		Expect(json.Unmarshal(payload, &values)).To(Succeed())
		Expect(values).To(ContainElement("Singapore"))
	})
})
