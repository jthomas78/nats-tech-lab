package dictionary

// Phase 54b-4: shipping-service on otelnats. With a Provider, every api.*
// endpoint is an EdgeAPI SERVER span (BR-064, BR-069, BR-071); the handler
// ctx reaches the command layer, so an evt.* publish is a PRODUCER span under
// it; each projector consumes under a CONSUMER span that continues that
// trace; and the notify.* publishes (projector, KV, port register) are
// PRODUCER spans under whichever span caused them. Without a Provider the
// natstrace specs (browserrpc_test.go, trace_async_test.go) hold.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

var _ = Describe("shipping-service on otelnats (Phase 54b-4)", func() {
	const (
		tenant   = "acme"
		fleetCtx = "acme-pacific-fleet"
	)

	var (
		nc  *nats.Conn
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
	)

	BeforeEach(func() {
		ctx := context.Background()
		var js jetstream.JetStream
		nc, js = newNatsConnAndJS()

		exp = tracetest.NewInMemoryExporter()
		var err error
		p, err = otelnats.Init(ctx, otelnats.Config{ServiceName: "shipping-service", Exporter: exp})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = p.Shutdown(context.Background()) })

		log := slog.New(slog.DiscardHandler)
		kvShips := kvstore.New(js, "ships")
		kvContainers := kvstore.New(js, "container")
		kvMeta := kvstore.New(js, "meta")
		for _, kv := range []*kvstore.Store{kvShips, kvContainers, kvMeta} {
			kv.EnableNotify(nc, log, natsnotify.WithOTel(p))
		}

		tracing := eventhandler.WithTracing(p, tenant)
		ccShips, err := eventhandler.RegisterShips(ctx, js, kvShips, nc, newFakeRepo(), log, tracing)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccShips.Stop)
		ccCont, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nc, newFakeContainerRepo(), log, tracing)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccCont.Stop)
		ccMeta, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nc, log, tracing)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccMeta.Stop)

		pub := jstream.NewPublisher(js, jstream.WithObservation(nc), jstream.WithOTel(p))
		portRepo := newFakePortRepo()
		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			Ships:      commands.NewShipHandler(pub, js, portRepo),
			Containers: commands.NewContainerHandler(pub, js, portRepo),
			Ports:      commands.NewPortHandler(portRepo),
			Terminal:   queries.NewTerminal(kvContainers),
			Meta:       queries.NewMeta(kvMeta),
			ShipReads:  queries.NewShips(kvShips, newFakeRepo()),
			Log:        log,
			Tenant:     tenant,
			Tracing:    p,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })
	})

	requestMsg := func(m *nats.Msg) {
		GinkgoHelper()
		reply, err := nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(reply.Header.Get("Nats-Service-Error")).To(BeEmpty(), string(reply.Data))
	}

	request := func(action string, body any) {
		GinkgoHelper()
		m := nats.NewMsg("api." + fleetCtx + ".shipping." + action + ".v1")
		m.Data, _ = json.Marshal(body)
		requestMsg(m)
	}

	registerContainer := func() {
		GinkgoHelper()
		request("container.register", commands.ContainerInput{
			ContainerID: "TCKU1234567", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam",
		})
	}

	spans := func() tracetest.SpanStubs {
		GinkgoHelper()
		Expect(p.ForceFlush(context.Background())).To(Succeed())
		return exp.GetSpans()
	}

	find := func(kind trace.SpanKind, nameHas string) []tracetest.SpanStub {
		var out []tracetest.SpanStub
		for _, s := range spans() {
			if s.SpanKind == kind && strings.Contains(s.Name, nameHas) {
				out = append(out, s)
			}
		}
		return out
	}

	attr := func(s tracetest.SpanStub, key string) (string, bool) {
		for _, kv := range s.Attributes {
			if string(kv.Key) == key {
				return kv.Value.Emit(), true
			}
		}
		return "", false
	}

	Context("BR-064, BR-069 — every api.* endpoint is a SERVER span in the adapter's tenant", func() {
		DescribeTable("one SERVER span named for the subject, receiving account = tenant, no originating tenant",
			func(action string, body any) {
				request(action, body)

				server := find(trace.SpanKindServer, ".shipping."+action+".v1")
				Expect(server).To(HaveLen(1))
				account, _ := attr(server[0], string(otelnats.AttrReceivingAccount))
				Expect(account).To(Equal(tenant))
				_, hasOrigin := attr(server[0], string(otelnats.AttrOriginatingTenant))
				Expect(hasOrigin).To(BeFalse())
			},
			Entry("ship.list", "ship.list", map[string]any{}),
			Entry("container.list", "container.list", map[string]any{}),
			Entry("container.manifest", "container.manifest", map[string]any{"shipID": "orient-express"}),
			Entry("port.list", "port.list", map[string]any{}),
			Entry("meta.known-containers", "meta.known-containers", map[string]any{}),
			Entry("ship.arrive", "ship.arrive", commands.ShipInput{ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"}),
			Entry("container.register", "container.register", commands.ContainerInput{ContainerID: "TCKU1234567", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam"}),
		)
	})

	Context("BR-071 — a browser traceparent is a link, never a parent", func() {
		It("starts a new trace and keeps the sent context as one link", func() {
			const sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
			m := nats.NewMsg("api." + fleetCtx + ".shipping.port.list.v1")
			m.Data = []byte("{}")
			m.Header.Set("Traceparent", "00-"+sentTraceID+"-00f067aa0ba902b7-01")
			requestMsg(m)

			server := find(trace.SpanKindServer, ".shipping.port.list.v1")
			Expect(server).To(HaveLen(1))
			Expect(server[0].SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
			Expect(server[0].Links).To(HaveLen(1))
			Expect(server[0].Links[0].SpanContext.TraceID().String()).To(Equal(sentTraceID))
		})
	})

	Context("the handler ctx reaches the command layer", func() {
		It("publishes the evt.* event as a PRODUCER span under the SERVER span", func() {
			registerContainer()

			server := find(trace.SpanKindServer, ".shipping.container.register.v1")
			Expect(server).To(HaveLen(1))
			producer := find(trace.SpanKindProducer, "evt."+fleetCtx+".shipping.container.")
			Expect(producer).To(HaveLen(1))
			Expect(producer[0].Parent.SpanID()).To(Equal(server[0].SpanContext.SpanID()))
		})

		It("publishes port register's notify.* as a PRODUCER span under the SERVER span", func() {
			request("port.register", map[string]any{"name": "Hamburg"})

			server := find(trace.SpanKindServer, ".shipping.port.register.v1")
			Expect(server).To(HaveLen(1))
			producer := find(trace.SpanKindProducer, "notify."+fleetCtx+".shipping.port.changed")
			Expect(producer).To(HaveLen(1))
			Expect(producer[0].Parent.SpanID()).To(Equal(server[0].SpanContext.SpanID()))
		})
	})

	Context("decision 5 — each projector continues the evt.* trace under a CONSUMER span", func() {
		It("gives the container and meta projectors one CONSUMER span each, child of the PRODUCER span", func() {
			registerContainer()

			Eventually(func() []tracetest.SpanStub {
				return find(trace.SpanKindConsumer, "evt."+fleetCtx+".shipping.container.")
			}, 5*time.Second, 50*time.Millisecond).Should(HaveLen(2))

			producer := find(trace.SpanKindProducer, "evt."+fleetCtx+".shipping.container.")
			Expect(producer).To(HaveLen(1))
			var durables []string
			for _, c := range find(trace.SpanKindConsumer, "evt."+fleetCtx+".shipping.container.") {
				Expect(c.Parent.SpanID()).To(Equal(producer[0].SpanContext.SpanID()))
				account, _ := attr(c, string(otelnats.AttrReceivingAccount))
				Expect(account).To(Equal(tenant))
				d, _ := attr(c, "consumer.durable")
				durables = append(durables, d)
			}
			Expect(durables).To(ConsistOf(eventhandler.ContainerProjectorDurable, eventhandler.MetaProjectorDurable))
		})

		It("publishes the projector's notify.* and KV notify as PRODUCER spans under its CONSUMER span", func() {
			registerContainer()

			var consumer tracetest.SpanStub
			Eventually(func() bool {
				for _, c := range find(trace.SpanKindConsumer, "evt."+fleetCtx+".shipping.container.") {
					if d, _ := attr(c, "consumer.durable"); d == eventhandler.ContainerProjectorDurable {
						consumer = c
						return true
					}
				}
				return false
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

			for _, name := range []string{
				"notify." + fleetCtx + ".shipping.container.changed",
				"notify." + fleetCtx + ".kv.container.",
			} {
				Eventually(func() []tracetest.SpanStub {
					return find(trace.SpanKindProducer, name)
				}, 5*time.Second, 50*time.Millisecond).ShouldNot(BeEmpty(), name)
				for _, s := range find(trace.SpanKindProducer, name) {
					Expect(s.Parent.SpanID()).To(Equal(consumer.SpanContext.SpanID()), name)
				}
			}
		})
	})

	It("publishes no natstrace span (the path is OTel only)", func() {
		legacy := make(chan *nats.Msg, 16)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		registerContainer()
		request("port.register", map[string]any{"name": "Hamburg"})
		Eventually(func() []tracetest.SpanStub {
			return find(trace.SpanKindConsumer, "evt.")
		}, 5*time.Second, 50*time.Millisecond).Should(HaveLen(2))
		Consistently(legacy, 300*time.Millisecond).ShouldNot(Receive())
	})
})
