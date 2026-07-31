package dictionary

// Integration tests for shipping-service's rpc.* dual-transport adapter
// (Phase 15a) against an embedded in-process NATS server (real JetStream,
// real KV) — same embedded-server convention as integration_test.go's
// newJetStream(), extended here to also expose the *nats.Conn a
// natsrpc.Adapter needs. Mirrors refdata-service's own
// natsrpc_test.go (BR-D25: an rpc.* operation must exist as a commands/
// queries method already exposed via REST) adapted to shipping-service's
// commands/queries and subject taxonomy.

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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/natsrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// newNatsConnAndJS mirrors newJetStream() (integration_test.go) but also
// returns the *nats.Conn — natsrpc.New needs the connection itself (to
// register a NATS Micro/Services instance and to fall back to plain
// nc.Publish for obs.rpc.* when JS isn't configured), not just a
// jetstream.JetStream context.
func newNatsConnAndJS() (*nats.Conn, jetstream.JetStream) {
	GinkgoHelper()
	opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("shipping-service-natsrpc-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	Expect(nc.Opts.Name).NotTo(BeEmpty(), "nats connection must be named")

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	_, err = jstream.CreateStream(context.Background(), js, domain.StreamName, domain.StreamSubjects())
	Expect(err).NotTo(HaveOccurred())
	return nc, js
}

var _ = Describe("NATS RPC Adapter (Phase 15a)", func() {
	const fleetCtx = "global"

	var (
		ctx        context.Context
		nc         *nats.Conn
		ships      *commands.ShipHandler
		containers *commands.ContainerHandler
		ports      *commands.PortHandler
		portRepo   *fakePortRepo
		kvA        *kvstore.Store
		adapter    *natsrpc.Adapter
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

		adapter, err = natsrpc.New(nc, natsrpc.Deps{
			Ships:      ships,
			Containers: containers,
			Ports:      ports,
			Terminal:   queries.NewTerminal(kvContainers),
			Meta:       queries.NewMeta(kvMeta),
			ShapeA:     queries.NewShapeA(kvA),
			Log:        log,
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

	Context("BR parity: rpc.* commands run the identical application-layer methods REST already calls", func() {
		It("arrives a ship via rpc.* identically to ArrivePort called directly, deriving context from the subject, not the body", func() {
			subject := "rpc." + fleetCtx + ".shipping.ship.arrive.v1"
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

			// The rpc.* reply returns once the event is published, not once
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
			Expect(direct[0].ShipID).To(Equal("orient-express"), "the rpc.* call's projected event must land in the SAME KV bucket a direct call would use")
		})

		It("departs a docked ship via rpc.*", func() {
			request("rpc."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			msg := request("rpc."+fleetCtx+".shipping.ship.depart.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg"})

			var resp struct {
				Ship domain.ShipState `json:"ship"`
			}
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Ship.CurrentPort).To(BeEmpty(), "a departed ship has no current port")
		})

		It("registers, loads, and unloads a container via rpc.*, matching the REST lifecycle", func() {
			request("rpc."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})

			registerMsg := request("rpc."+fleetCtx+".shipping.container.register.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU1234567", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam",
			})
			var registerResp struct {
				Container domain.ContainerState `json:"container"`
			}
			Expect(json.Unmarshal(registerMsg.Data, &registerResp)).To(Succeed())
			Expect(registerResp.Container.TerminalPort).NotTo(BeNil())
			Expect(*registerResp.Container.TerminalPort).To(Equal("Hamburg"))

			loadMsg := request("rpc."+fleetCtx+".shipping.container.load.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU1234567", ShipID: "orient-express",
			})
			var loadResp struct {
				Container domain.ContainerState `json:"container"`
			}
			Expect(json.Unmarshal(loadMsg.Data, &loadResp)).To(Succeed())
			Expect(loadResp.Container.OnShipID).NotTo(BeNil())
			Expect(*loadResp.Container.OnShipID).To(Equal("orient-express"))

			request("rpc."+fleetCtx+".shipping.ship.depart.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg"})
			request("rpc."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", Port: "Rotterdam"})

			unloadMsg := request("rpc."+fleetCtx+".shipping.container.unload.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU1234567", ShipID: "orient-express",
			})
			var unloadResp struct {
				Container domain.ContainerState `json:"container"`
			}
			Expect(json.Unmarshal(unloadMsg.Data, &unloadResp)).To(Succeed())
			Expect(unloadResp.Container.OnShipID).To(BeNil())
			Expect(*unloadResp.Container.TerminalPort).To(Equal("Rotterdam"))
		})

		It("registers and lists ports via rpc.*, matching PortHandler.Register/List called directly", func() {
			registerMsg := request("rpc."+fleetCtx+".shipping.port.register.v1", map[string]string{"name": "Singapore"})
			var registerResp struct {
				Port string `json:"port"`
			}
			Expect(json.Unmarshal(registerMsg.Data, &registerResp)).To(Succeed())
			Expect(registerResp.Port).To(Equal("Singapore"))

			listMsg := request("rpc."+fleetCtx+".shipping.port.list.v1", map[string]any{})
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

	Context("BR parity: rpc.* bootstrap queries back the browser's reconnect flow (Phase 15d)", func() {
		It("ship.list.v1 returns every ship in the context, identical to queries.ShapeA.ListShips called directly", func() {
			request("rpc."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			request("rpc."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "north-star", ShipName: "North Star", Port: "Rotterdam"})

			// The rpc.* reply returns once the event is published, not once
			// the Shape A projector has consumed it and written KV — same
			// publish/project race integration_test.go's eventually() helper
			// guards against.
			var resp struct {
				Ships []domain.ShipState `json:"ships"`
			}
			eventually(func() error {
				msg := request("rpc."+fleetCtx+".shipping.ship.list.v1", map[string]any{})
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
			request("rpc."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg"})
			request("rpc."+fleetCtx+".shipping.container.register.v1", commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU7654321", Cargo: "grain", OriginPort: "Hamburg", DestPort: "Rotterdam",
			})

			var containerResp struct {
				Containers []domain.ContainerState `json:"containers"`
			}
			eventually(func() error {
				containerMsg := request("rpc."+fleetCtx+".shipping.container.list.v1", map[string]any{})
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
				metaMsg := request("rpc."+fleetCtx+".shipping.meta.known-containers.v1", map[string]any{})
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

	Context("BR parity: rpc.* errors surface the same domain error as REST, plus a notFound flag", func() {
		It("returns notFound for a container.load call on an unregistered container", func() {
			msg := request("rpc."+fleetCtx+".shipping.container.load.v1", commands.ContainerInput{
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

	Context("BR-D26 parity: an obs.rpc.* publish must never block or fail the real RPC reply", func() {
		It("still returns the real reply promptly with no obs.rpc.> subscriber at all", func() {
			start := time.Now()
			msg := request("rpc."+fleetCtx+".shipping.ship.arrive.v1", commands.ShipInput{
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
})
