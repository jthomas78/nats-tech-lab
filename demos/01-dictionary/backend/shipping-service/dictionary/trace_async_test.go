package dictionary

// Tests for Phase 28d's async tail (BR-037): the evt.* JetStream publish an
// api.* command triggers must carry the same traceId as that request's
// reply-side span (natstrace.ContextWithSpan/SpanFromContext riding ctx down
// through commands.go/container.go's publish, Piece 1/2 of this phase), and
// each of the three eventhandler Consume callbacks (handler.go's shared
// register(), used by RegisterShips; container_handler.go's
// RegisterContainers; meta_handler.go's RegisterMeta) publishes exactly one
// obs.trace.* span per message it processes, labeled with the entity/action
// parsed off the evt.* subject plus an entity_id attribute (Piece 3). Uses
// newNatsConnAndJS() (browserrpc_test.go) for a real embedded NATS/JetStream
// server, same convention as notify_test.go.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// traceparentSpan is the subset of natstrace's traceSpan wire shape these
// specs need to decode.
type traceparentSpan struct {
	TraceID    string            `json:"traceId"`
	SpanID     string            `json:"spanId"`
	Service    string            `json:"service"`
	Entity     string            `json:"entity"`
	Action     string            `json:"action"`
	Attributes map[string]string `json:"attributes"`
}

// traceIDFromTraceparent extracts the traceId ("00-<traceid>-<spanid>-01")
// segment from a Traceparent header value.
func traceIDFromTraceparent(tp string) string {
	GinkgoHelper()
	parts := strings.Split(tp, "-")
	Expect(parts).To(HaveLen(4), "malformed traceparent: %q", tp)
	return parts[1]
}

// subscribeMsgs subscribes to subject and returns a channel of full *nats.Msg
// (headers included, unlike notify_test.go's subscribeNotify which only
// keeps Data) — these specs need headers to read Traceparent.
func subscribeMsgs(nc *nats.Conn, subject string) chan *nats.Msg {
	GinkgoHelper()
	out := make(chan *nats.Msg, 16)
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) { out <- m })
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })
	return out
}

var _ = Describe("BR-037 (Phase 28d): async tail carries the originating request's traceId", func() {
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

	It("propagates the api.* request's traceId onto the resulting evt.* JetStream publish", func() {
		kvB := kvstore.New(js, "ships")
		ccB, err := eventhandler.RegisterShips(ctx, js, kvB, nc, newFakeRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccB.Stop)

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			Ships:     ships,
			ShipReads: queries.NewShips(kvB, newFakeRepo()),
			Log:       log,
			Tenant:    fleetCtx,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		evts := subscribeMsgs(nc, "evt.>")
		spans := subscribeMsgs(nc, "obs.trace.>")
		Expect(nc.Flush()).To(Succeed())

		reqBody, err := json.Marshal(commands.ShipInput{
			Context: fleetCtx, ShipID: "trace-ship", ShipName: "Trace Ship", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = nc.Request("api."+fleetCtx+".shipping.ship.arrive.v1", reqBody, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var evtMsg *nats.Msg
		Eventually(evts).Should(Receive(&evtMsg))
		Expect(evtMsg.Header.Get(natstrace.TraceparentHeader)).NotTo(BeEmpty(),
			"the evt.* publish must carry a traceparent (PublishWithTrace, commands.go's publish)")

		var spanMsg *nats.Msg
		Eventually(spans).Should(Receive(&spanMsg))
		var span traceparentSpan
		Expect(json.Unmarshal(spanMsg.Data, &span)).To(Succeed())
		Expect(span.TraceID).NotTo(BeEmpty())

		Expect(traceIDFromTraceparent(evtMsg.Header.Get(natstrace.TraceparentHeader))).To(Equal(span.TraceID),
			"the evt.* publish and the api.* reply-side span must share the same traceId")
	})

	It("still propagates a traceId with no inbound traceparent header at all (a root span minted by browserrpc.Adapter)", func() {
		kvB := kvstore.New(js, "ships")
		ccB, err := eventhandler.RegisterShips(ctx, js, kvB, nc, newFakeRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccB.Stop)

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			Ships:     ships,
			ShipReads: queries.NewShips(kvB, newFakeRepo()),
			Log:       log,
			Tenant:    fleetCtx,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		evts := subscribeMsgs(nc, "evt.>")
		Expect(nc.Flush()).To(Succeed())

		reqBody, err := json.Marshal(commands.ShipInput{
			Context: fleetCtx, ShipID: "root-span-ship", ShipName: "Root Span Ship", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = nc.Request("api."+fleetCtx+".shipping.ship.arrive.v1", reqBody, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var evtMsg *nats.Msg
		Eventually(evts).Should(Receive(&evtMsg))
		tp := evtMsg.Header.Get(natstrace.TraceparentHeader)
		Expect(tp).NotTo(BeEmpty())
		Expect(traceIDFromTraceparent(tp)).NotTo(BeEmpty())
	})
})

var _ = Describe("BR-037 (Phase 28d): each JetStream projector Consume callback publishes exactly one span per message", func() {
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

	// firstMatching drains ch until a span decodes with the wanted action,
	// returning it — a ship's first arrival implicitly registers it too
	// (an extra .registered span alongside .arrived), so a spec asserting on
	// one action can't just take the first message off the channel.
	firstMatching := func(ch chan *nats.Msg, action string) traceparentSpan {
		GinkgoHelper()
		var found traceparentSpan
		Eventually(func() bool {
			select {
			case m := <-ch:
				var s traceparentSpan
				if json.Unmarshal(m.Data, &s) == nil && s.Action == action {
					found = s
					return true
				}
				return false
			default:
				return false
			}
		}, 3*time.Second, 20*time.Millisecond).Should(BeTrue(), "no span with action %q arrived in time", action)
		return found
	}

	// noMoreMatching asserts no further span with the wanted action arrives
	// within a short window — the "exactly one" half of "exactly one span
	// per message".
	noMoreMatching := func(ch chan *nats.Msg, action string) {
		GinkgoHelper()
		Consistently(func() bool {
			select {
			case m := <-ch:
				var s traceparentSpan
				if json.Unmarshal(m.Data, &s) == nil && s.Action == action {
					return true
				}
				return false
			default:
				return false
			}
		}, 300*time.Millisecond, 20*time.Millisecond).Should(BeFalse())
	}

	It("handler.go's shared register() (RegisterShips) publishes exactly one span for the .arrived event, labeled ship/arrived with an entity_id", func() {
		kvB := kvstore.New(js, "ships")
		ccB, err := eventhandler.RegisterShips(ctx, js, kvB, nc, newFakeRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccB.Stop)

		spans := subscribeMsgs(nc, "obs.trace.>")
		Expect(nc.Flush()).To(Succeed())

		ships := commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = ships.ArrivePort(ctx, commands.ShipInput{Context: fleetCtx, ShipID: "span-ship-b", ShipName: "Span Ship B", Port: "Hamburg"})
		Expect(err).NotTo(HaveOccurred())

		span := firstMatching(spans, "arrived")
		Expect(span.Service).To(Equal("shipping"))
		Expect(span.Entity).To(Equal("ship"))
		Expect(span.Attributes).NotTo(BeEmpty())
		Expect(span.Attributes["entity_id"]).NotTo(BeEmpty())

		noMoreMatching(spans, "arrived")
	})

	It("container_handler.go's RegisterContainers publishes exactly one span for the .registered event, labeled container/registered with an entity_id", func() {
		kvContainers := kvstore.New(js, "container")
		ccCont, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nc, newFakeContainerRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccCont.Stop)

		spans := subscribeMsgs(nc, "obs.trace.>")
		Expect(nc.Flush()).To(Succeed())

		containers := commands.NewContainerHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU1112223", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		span := firstMatching(spans, "registered")
		Expect(span.Service).To(Equal("shipping"))
		Expect(span.Entity).To(Equal("container"))
		Expect(span.Attributes["entity_id"]).NotTo(BeEmpty())

		noMoreMatching(spans, "registered")
	})

	It("meta_handler.go's RegisterMeta publishes exactly one span for the .registered event, labeled container/registered with an entity_id", func() {
		// RegisterContainers is deliberately NOT registered in this spec —
		// both consumers read the same evt.*.shipping.container.> subject,
		// and registering both would make "exactly one" ambiguous about
		// which projector emitted which span. containers.RegisterContainer
		// only needs the SHIPPING stream to exist (already true via
		// newNatsConnAndJS), not a running container projector.
		kvMeta := kvstore.New(js, "meta")
		ccMeta, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nc, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccMeta.Stop)

		spans := subscribeMsgs(nc, "obs.trace.>")
		Expect(nc.Flush()).To(Succeed())

		containers := commands.NewContainerHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU4445556", Cargo: "grain", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		span := firstMatching(spans, "registered")
		Expect(span.Service).To(Equal("shipping"))
		Expect(span.Entity).To(Equal("container"))
		Expect(span.Attributes["entity_id"]).NotTo(BeEmpty())

		noMoreMatching(spans, "registered")
	})
})
