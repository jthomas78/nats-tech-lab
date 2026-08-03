package dictionary

// Integration tests for shipping-service's api.* frontend-to-service
// adapter (Phase 15a, renamed from rpc.*/natsrpc in Phase 16b) against an
// embedded in-process NATS server (real JetStream, real KV) — same
// embedded-server convention as integration_test.go's newJetStream(),
// extended here to also expose the *nats.Conn a browserrpc.Adapter needs.
// Mirrors the parity discipline of refdata-service's own natsrpc_test.go
// (BR-D25: an exposed operation must run the identical commands/queries
// method REST already calls) adapted to shipping-service's commands/queries
// and subject taxonomy.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// newNatsConnAndJS mirrors newJetStream() (integration_test.go) but also
// returns the *nats.Conn — browserrpc.New needs the connection itself (to
// register a NATS Micro/Services instance and to fall back to plain
// nc.Publish for obs.api.* when JS isn't configured), not just a
// jetstream.JetStream context.
func newNatsConnAndJS() (*nats.Conn, jetstream.JetStream) {
	GinkgoHelper()
	opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("shipping-service-browserrpc-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	Expect(nc.Opts.Name).NotTo(BeEmpty(), "nats connection must be named")

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	_, err = jstream.CreateStream(context.Background(), js, domain.StreamName, domain.StreamSubjects())
	Expect(err).NotTo(HaveOccurred())
	return nc, js
}

var _ = Describe("Browser API Adapter (Phase 15a/16b)", func() {
	const fleetCtx = "acme"

	var (
		ctx        context.Context
		nc         *nats.Conn
		ships      *commands.ShipHandler
		containers *commands.ContainerHandler
		ports      *commands.PortHandler
		portRepo   *fakePortRepo
		kvA        *kvstore.Store
		adapter    *browserrpc.Adapter
	)

	BeforeEach(func() {
		ctx = context.Background()
		var js jetstream.JetStream
		nc, js = newNatsConnAndJS()

		kvA = kvstore.New(js, "dict-a")
		kvContainers := kvstore.New(js, "container")
		kvMeta := kvstore.New(js, "meta")
		log := slog.New(slog.DiscardHandler)

		ccA, err := eventhandler.RegisterShapeA(ctx, js, kvA, nc, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccA.Stop)

		ccCont, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nc, newFakeContainerRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccCont.Stop)

		ccMeta, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nc, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(ccMeta.Stop)

		pub := jstream.NewPublisher(js)
		portRepo = newFakePortRepo()
		ships = commands.NewShipHandler(pub, js, portRepo)
		containers = commands.NewContainerHandler(pub, js, portRepo)
		ports = commands.NewPortHandler(portRepo)

		adapter, err = browserrpc.New(nc, browserrpc.Deps{
			Ships:      ships,
			Containers: containers,
			Ports:      ports,
			Terminal:   queries.NewTerminal(kvContainers),
			Meta:       queries.NewMeta(kvMeta),
			ShapeA:     queries.NewShapeA(kvA),
			Log:        log,
			Tenant:     fleetCtx,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })
	})

	request := func(subject string, body any) *nats.Msg {
		GinkgoHelper()
		data, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		msg, err := nc.Request(subject, data, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		return msg
	}

	Context("BR parity: api.* commands run the identical application-layer methods REST already calls", func() {
		It("arrives a ship via api.* identically to ArrivePort called directly, deriving context from the subject, not the body", func() {
			subject := "api." + fleetCtx + ".shipping.ship.arrive.v1"
			// The body's Context is deliberately a different, otherwise-valid
			// token — proving contextFromSubject wins, not a client-supplied
			// field (see adapter.go's contextFromSubject doc comment: this is
			// the actual tenant-isolation boundary for a browser client).
			msg := request(subject, commands.ShipInput{
				Context: "attacker-controlled", ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
			})

			var resp struct {
				Ship domain.ShipState `json:"ship"`
			}
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Ship.ShipID).To(Equal("orient-express"))
			Expect(resp.Ship.CurrentPort).To(Equal("Hamburg"))
			Expect(resp.Ship.Context).To(Equal(fleetCtx))

			// The api.* reply returns once the event is published, not once
			// the Shape A projector has consumed it and written KV — same
			// publish/project race the bootstrap-query tests below guard
			// against with eventually().
			var direct []domain.ShipState
			eventually(func() error {
				var err error
				direct, err = queries.NewShapeA(kvA).ListShips(ctx, fleetCtx)
				if err != nil {
					return err
				}
				if len(direct) != 1 {
					return fmt.Errorf("got %d ships, want 1", len(direct))
				}
				return nil
			})
			Expect(direct[0].ShipID).To(Equal("orient-express"), "the api.* call's projected event must land in the SAME KV bucket a direct call would use")
		})

		It("departs a docked ship via api.*", func() {
			request("api."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			msg := request("api."+fleetCtx+".shipping.ship.depart.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg"})

			var resp struct {
				Ship domain.ShipState `json:"ship"`
			}
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Ship.CurrentPort).To(BeEmpty(), "a departed ship has no current port")
		})

		It("registers, loads, and unloads a container via api.*, matching the REST lifecycle", func() {
			request("api."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})

			registerMsg := request("api."+fleetCtx+".shipping.container.register.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU1234567", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam",
			})
			var registerResp struct {
				Container domain.ContainerState `json:"container"`
			}
			Expect(json.Unmarshal(registerMsg.Data, &registerResp)).To(Succeed())
			Expect(registerResp.Container.TerminalPort).NotTo(BeNil())
			Expect(*registerResp.Container.TerminalPort).To(Equal("Hamburg"))

			loadMsg := request("api."+fleetCtx+".shipping.container.load.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU1234567", ShipID: "orient-express",
			})
			var loadResp struct {
				Container domain.ContainerState `json:"container"`
			}
			Expect(json.Unmarshal(loadMsg.Data, &loadResp)).To(Succeed())
			Expect(loadResp.Container.OnShipID).NotTo(BeNil())
			Expect(*loadResp.Container.OnShipID).To(Equal("orient-express"))

			request("api."+fleetCtx+".shipping.ship.depart.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg"})
			request("api."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", Port: "Rotterdam"})

			unloadMsg := request("api."+fleetCtx+".shipping.container.unload.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU1234567", ShipID: "orient-express",
			})
			var unloadResp struct {
				Container domain.ContainerState `json:"container"`
			}
			Expect(json.Unmarshal(unloadMsg.Data, &unloadResp)).To(Succeed())
			Expect(unloadResp.Container.OnShipID).To(BeNil())
			Expect(*unloadResp.Container.TerminalPort).To(Equal("Rotterdam"))
		})

		It("registers and lists ports via api.*, matching PortHandler.Register/List called directly", func() {
			registerMsg := request("api."+fleetCtx+".shipping.port.register.v1", map[string]string{"name": "Singapore"})
			var registerResp struct {
				Port string `json:"port"`
			}
			Expect(json.Unmarshal(registerMsg.Data, &registerResp)).To(Succeed())
			Expect(registerResp.Port).To(Equal("Singapore"))

			listMsg := request("api."+fleetCtx+".shipping.port.list.v1", map[string]any{})
			var listResp struct {
				Values []string `json:"values"`
			}
			Expect(json.Unmarshal(listMsg.Data, &listResp)).To(Succeed())
			Expect(listResp.Values).To(ContainElement("Singapore"))

			direct, err := ports.List(ctx, fleetCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.Values).To(ConsistOf(direct))
		})
	})

	Context("BR parity: api.* bootstrap queries back the browser's reconnect flow (Phase 15d)", func() {
		It("ship.list.v1 returns every ship in the context, identical to queries.ShapeA.ListShips called directly", func() {
			request("api."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			request("api."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "north-star", ShipName: "North Star", Port: "Rotterdam"})

			// The api.* reply returns once the event is published, not once
			// the Shape A projector has consumed it and written KV — same
			// publish/project race integration_test.go's eventually() helper
			// guards against.
			var resp struct {
				Ships []domain.ShipState `json:"ships"`
			}
			eventually(func() error {
				msg := request("api."+fleetCtx+".shipping.ship.list.v1", map[string]any{})
				if err := json.Unmarshal(msg.Data, &resp); err != nil {
					return err
				}
				if len(resp.Ships) != 2 {
					return fmt.Errorf("got %d ships, want 2", len(resp.Ships))
				}
				return nil
			})

			direct, err := queries.NewShapeA(kvA).ListShips(ctx, fleetCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Ships).To(ConsistOf(direct))
		})

		It("container.list.v1 and meta.known-containers.v1 match queries.Terminal.List/Meta.KnownContainers called directly", func() {
			request("api."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			request("api."+fleetCtx+".shipping.container.register.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU7654321", Cargo: "grain", OriginPort: "Hamburg", DestPort: "Rotterdam",
			})

			var containerResp struct {
				Containers []domain.ContainerState `json:"containers"`
			}
			eventually(func() error {
				containerMsg := request("api."+fleetCtx+".shipping.container.list.v1", map[string]any{})
				if err := json.Unmarshal(containerMsg.Data, &containerResp); err != nil {
					return err
				}
				if len(containerResp.Containers) != 1 {
					return fmt.Errorf("got %d containers, want 1", len(containerResp.Containers))
				}
				return nil
			})
			Expect(containerResp.Containers[0].ContainerID).To(Equal("TCKU7654321"))

			var metaResp struct {
				Values []string `json:"values"`
			}
			eventually(func() error {
				metaMsg := request("api."+fleetCtx+".shipping.meta.known-containers.v1", map[string]any{})
				if err := json.Unmarshal(metaMsg.Data, &metaResp); err != nil {
					return err
				}
				if len(metaResp.Values) == 0 {
					return fmt.Errorf("known-containers is empty")
				}
				return nil
			})
			Expect(metaResp.Values).To(ContainElement("TCKU7654321"))
		})
	})

	Context("BR parity: api.* errors surface the same domain error as REST, plus a notFound flag", func() {
		It("returns notFound for a container.load call on an unregistered container", func() {
			msg := request("api."+fleetCtx+".shipping.container.load.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000000", ShipID: "no-such-ship",
			})

			var errResp struct {
				Error    string `json:"error"`
				NotFound bool   `json:"notFound"`
			}
			Expect(json.Unmarshal(msg.Data, &errResp)).To(Succeed())
			Expect(errResp.Error).NotTo(BeEmpty())
			Expect(errResp.NotFound).To(BeTrue())

			_, directErr := containers.LoadContainer(ctx, commands.ContainerInput{Context: fleetCtx, ContainerID: "TCKU0000000", ShipID: "no-such-ship"})
			Expect(directErr).To(HaveOccurred())
			Expect(errResp.Error).To(Equal(directErr.Error()))
		})
	})

	Context("BR-D26 parity: an obs.api.* publish must never block or fail the real RPC reply", func() {
		It("still returns the real reply promptly with no obs.api.> subscriber at all", func() {
			start := time.Now()
			msg := request("api."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{
				Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
			})
			Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))

			var resp struct {
				Ship domain.ShipState `json:"ship"`
			}
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Ship.ShipID).To(Equal("orient-express"))
		})
	})

	Context("BR-026: every obs.api.* event carries its headers, a publisher-side timestamp, and its payload size", func() {
		type obsEnvelope struct {
			Direction     string              `json:"direction"`
			Error         string              `json:"error,omitempty"`
			Headers       map[string][]string `json:"headers,omitempty"`
			Timestamp     time.Time           `json:"timestamp"`
			PayloadBytes  int                 `json:"payloadBytes"`
			CorrelationID string              `json:"correlationId"`
		}

		It("stamps the request-side event with the caller's real headers, a non-zero timestamp, and the true payload size", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.api.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			Expect(err).NotTo(HaveOccurred())

			msg := nats.NewMsg("api." + fleetCtx + ".shipping.ship.arrive.v1")
			msg.Data = reqBody
			msg.Header = nats.Header{"X-Client": []string{"seafreight-app-test"}}
			before := time.Now()
			reply, err := nc.RequestMsg(msg, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(reply).NotTo(BeNil())

			var reqEnv obsEnvelope
			Eventually(func() bool {
				select {
				case m := <-obsMsgs:
					Expect(json.Unmarshal(m.Data, &reqEnv)).To(Succeed())
					return reqEnv.Direction == "request"
				default:
					return false
				}
			}, 2*time.Second).Should(BeTrue(), "expected a request-side obs.api.* event")

			Expect(reqEnv.Headers).To(HaveKeyWithValue("X-Client", []string{"seafreight-app-test"}))
			Expect(reqEnv.Timestamp).To(BeTemporally(">=", before))
			Expect(reqEnv.PayloadBytes).To(Equal(len(reqBody)))
		})

		It("attaches real Nats-Service-Error/-Code headers to a failed reply, on both the obs event and the actual wire reply", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.api.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(commands.ContainerInput{Context: fleetCtx, ContainerID: "TCKU0000000", ShipID: "no-such-ship"})
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request("api."+fleetCtx+".shipping.container.load.v1", reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			Expect(reply.Header.Get("Nats-Service-Error")).NotTo(BeEmpty(), "the real wire reply must carry the error header, not just the obs event")
			Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("404"))

			var sawErrorHeaders bool
			for i := 0; i < 2; i++ {
				select {
				case m := <-obsMsgs:
					var env obsEnvelope
					Expect(json.Unmarshal(m.Data, &env)).To(Succeed())
					if env.Direction == "reply" {
						Expect(env.Headers).To(HaveKeyWithValue("Nats-Service-Error-Code", []string{"404"}))
						sawErrorHeaders = true
					}
				case <-time.After(2 * time.Second):
					Fail("timed out waiting for obs.api.* events")
				}
			}
			Expect(sawErrorHeaders).To(BeTrue())
		})

		It("still decodes an old-shape obs event with no headers/timestamp/payloadBytes fields at all", func() {
			var env obsEnvelope
			old := []byte(`{"direction":"reply","correlationId":"legacy-1","subject":"api.acme.shipping.ship.arrive.v1","payload":{"ship":{}}}`)
			Expect(json.Unmarshal(old, &env)).To(Succeed())
			Expect(env.Direction).To(Equal("reply"))
			Expect(env.Headers).To(BeNil())
			Expect(env.Timestamp).To(BeZero())
		})
	})

	Context("BR-027: every api.* request carries a Nats-Requestor header identifying its caller, and every reply a Nats-Responder header identifying the answering service instance", func() {
		type obsEnvelope struct {
			Direction     string              `json:"direction"`
			Error         string              `json:"error,omitempty"`
			Headers       map[string][]string `json:"headers,omitempty"`
			Timestamp     time.Time           `json:"timestamp"`
			PayloadBytes  int                 `json:"payloadBytes"`
			CorrelationID string              `json:"correlationId"`
		}

		It("forwards the caller's Nats-Requestor header into the obs.api.* request event, same as any other caller-supplied header", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.api.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			Expect(err).NotTo(HaveOccurred())

			msg := nats.NewMsg("api." + fleetCtx + ".shipping.ship.arrive.v1")
			msg.Data = reqBody
			// Instance-qualified "<app>/<instance ID>" — the same
			// name/instance split Nats-Responder uses (BR-027); the browser
			// generates the instance half once per tab (useNatsConnection.js).
			msg.Header = nats.Header{"Nats-Requestor": []string{"seafreight-app/test-tab-1"}}
			reply, err := nc.RequestMsg(msg, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(reply).NotTo(BeNil())

			var reqEnv obsEnvelope
			Eventually(func() bool {
				select {
				case m := <-obsMsgs:
					Expect(json.Unmarshal(m.Data, &reqEnv)).To(Succeed())
					return reqEnv.Direction == "request"
				default:
					return false
				}
			}, 2*time.Second).Should(BeTrue(), "expected a request-side obs.api.* event")

			Expect(reqEnv.Headers).To(HaveKeyWithValue("Nats-Requestor", []string{"seafreight-app/test-tab-1"}))
		})

		It("attaches a Nats-Responder header (service name/instance ID) to a successful reply, on both the real wire reply and the obs.api.* event", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.api.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(commands.ShipInput{Context: fleetCtx, ShipID: "orient-express-2", ShipName: "Orient Express 2", Port: "Hamburg"})
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request("api."+fleetCtx+".shipping.ship.arrive.v1", reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			responder := reply.Header.Get("Nats-Responder")
			Expect(responder).To(HavePrefix("shipping-service/"), "the real wire reply must carry the responder header, not just the obs event")

			var replyEnv obsEnvelope
			Eventually(func() bool {
				select {
				case m := <-obsMsgs:
					Expect(json.Unmarshal(m.Data, &replyEnv)).To(Succeed())
					return replyEnv.Direction == "reply"
				default:
					return false
				}
			}, 2*time.Second).Should(BeTrue(), "expected a reply-side obs.api.* event")
			Expect(replyEnv.Headers).To(HaveKeyWithValue("Nats-Responder", []string{responder}))
		})

		It("attaches a Nats-Responder header to a failed reply too", func() {
			reqBody, err := json.Marshal(commands.ContainerInput{Context: fleetCtx, ContainerID: "TCKU0000001", ShipID: "no-such-ship"})
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request("api."+fleetCtx+".shipping.container.load.v1", reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			Expect(reply.Header.Get("Nats-Responder")).To(HavePrefix("shipping-service/"))
		})
	})

	Context("BR-028: in the Admin UI, a service/connection's account resolves to a friendly name where possible — this connection's micro registration is tagged with its tenant", func() {
		It("responds to $SRV.PING with metadata.tenant equal to the connection's fleet context", func() {
			subject, err := micro.ControlSubject(micro.PingVerb, "", "")
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request(subject, nil, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var ping micro.Ping
			Expect(json.Unmarshal(reply.Data, &ping)).To(Succeed())
			Expect(ping.Metadata).To(HaveKeyWithValue("tenant", fleetCtx))
		})
	})
})
