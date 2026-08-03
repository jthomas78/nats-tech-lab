package dictionary

// Phase 13b integration spec (Main-POC-Plan.md, Phase 13b — creds mechanism
// updated in Phase 14a): proves the tenant switch through the actual
// application path — rest.Handlers.SwitchTenant and real HTTP requests —
// not just a synthetic NATS-level check like 13a's. Loads the real shipping
// nats/nats.conf (operator mode, resolver_preload) into an embedded server,
// same as internal/natsaccounts/isolation_test.go, so this exercises the
// shipped config rather than a re-description of it.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/rest"
)

// tenantSwitchNatsDir mirrors internal/natsaccounts's own path constant:
// this test file sits one directory shallower (dictionary/ vs.
// internal/natsaccounts/), hence one fewer "..".
const tenantSwitchNatsDir = "../../../nats"

func tenantSwitchCredsPath(name string) string {
	return filepath.Join(tenantSwitchNatsDir, "creds", name+".creds")
}

// loadTenantSwitchServer mirrors internal/natsaccounts.newSpikeServer's
// config-rewrite step (see that function's doc comment for why the two
// docker-only absolute paths need rewriting before an embedded server can
// load the shipped nats.conf).
func loadTenantSwitchServer() *server.Server {
	GinkgoHelper()

	raw, err := os.ReadFile(filepath.Join(tenantSwitchNatsDir, "nats.conf"))
	Expect(err).NotTo(HaveOccurred())
	operatorPath, err := filepath.Abs(filepath.Join(tenantSwitchNatsDir, "operator.jwt"))
	Expect(err).NotTo(HaveOccurred())
	rewritten := regexp.MustCompile(`operator:\s*\S+`).ReplaceAll(raw, []byte("operator: "+operatorPath))
	rewritten = regexp.MustCompile(`dir:\s*"[^"]*"`).ReplaceAll(rewritten, []byte(`dir: "`+GinkgoT().TempDir()+`"`))

	confPath := filepath.Join(GinkgoT().TempDir(), "nats.conf")
	Expect(os.WriteFile(confPath, rewritten, 0o600)).To(Succeed())

	opts, err := server.ProcessConfigFile(confPath)
	Expect(err).NotTo(HaveOccurred())
	opts.Port = -1
	opts.HTTPPort = 0
	opts.Websocket.Port = 0
	opts.StoreDir = GinkgoT().TempDir()

	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())
	return srv
}

var _ = Describe("Phase 13b — tenant switch", func() {
	var (
		ctx      context.Context
		srv      *server.Server
		handlers *rest.Handlers
		client   *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		srv = loadTenantSwitchServer()

		deps := rest.Deps{
			Ports:         commands.NewPortHandler(newFakePortRepo()),
			Log:           slog.New(slog.DiscardHandler),
			ShipRepo:      newFakeRepo(),
			ContainerRepo: newFakeContainerRepo(),
			PortRepo:      newFakePortRepo(),
			NatsURL:       srv.ClientURL(),
			CredsDir:      filepath.Join(tenantSwitchNatsDir, "creds"),
		}
		handlers = rest.NewHandlers(deps)
		Expect(handlers.SwitchTenant(ctx, "acme")).To(Succeed())

		mux := http.NewServeMux()
		handlers.Mount(mux)
		client = httptest.NewServer(mux)
		DeferCleanup(client.Close)
	})

	connectAs := func(name string) (*nats.Conn, jetstream.JetStream) {
		GinkgoHelper()
		nc, err := nats.Connect(srv.ClientURL(), nats.Name("tenant-switch-test"), nats.UserCredentials(tenantSwitchCredsPath(name)))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)
		js, err := jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())
		return nc, js
	}

	arriveShip := func(shipID, port string) {
		GinkgoHelper()
		body := `{"context":"acme","shipID":"` + shipID + `","shipName":"Tenant Spike","port":"` + port + `"}`
		resp, err := client.Client().Post(client.URL+"/api/ships/arrive", "application/json", strings.NewReader(body))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
	}

	fleetShipIDs := func() []string {
		GinkgoHelper()
		resp, err := client.Client().Get(client.URL + "/api/shape-c/fleet")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var fleet struct {
			Fleet []struct {
				ShipID string `json:"shipID"`
			} `json:"fleet"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&fleet)).To(Succeed())
		var ids []string
		for _, s := range fleet.Fleet {
			ids = append(ids, s.ShipID)
		}
		return ids
	}

	switchTo := func(tenant string) {
		GinkgoHelper()
		body := `{"tenant":"` + tenant + `"}`
		resp, err := client.Client().Post(client.URL+"/api/tenant/switch", "application/json", strings.NewReader(body))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}

	It("makes tenant A's ships unreachable after switching to tenant B, with the isolation originating server-side, and recovers on switching back", func() {
		By("registering a ship while acme is active")
		arriveShip("acme-spike-ship", "Hamburg")
		Expect(fleetShipIDs()).To(ContainElement("acme-spike-ship"))

		By("confirming no InactiveThreshold is set on the projector durables — a durable with one is reaped after inactivity and would lose its position across a long tenant switch")
		_, acmeJS := connectAs("acme")
		cons, err := acmeJS.Consumer(ctx, "SHIPPING", "ship-shape-a")
		Expect(err).NotTo(HaveOccurred())
		info, err := cons.Info(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Config.InactiveThreshold).To(BeZero())

		By("switching to globex")
		switchTo("globex")
		Expect(fleetShipIDs()).NotTo(ContainElement("acme-spike-ship"),
			"tenant B must not see tenant A's ship through the same API call")

		By("proving that's a server-side isolation fact, not an application filter: globex's own SHIPPING stream — independently created by SwitchTenant — genuinely has zero messages")
		_, globexJS := connectAs("globex")
		globexStream, err := globexJS.Stream(ctx, "SHIPPING")
		Expect(err).NotTo(HaveOccurred(), "SwitchTenant must create SHIPPING in every tenant account it connects to")
		globexInfo, err := globexStream.Info(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(globexInfo.State.Msgs).To(BeZero(), "globex's independent SHIPPING stream must never have received acme's event")

		By("switching back to acme recovers the ship — the durable's stream position was never lost")
		switchTo("acme")
		Expect(fleetShipIDs()).To(ContainElement("acme-spike-ship"))

		By("regression: a switch triggered over HTTP must not leave the new tenant's projectors permanently broken — POST /api/tenant/switch's r.Context() is canceled the instant that response is sent, so a projector wired to it (not context.WithoutCancel'd) would fail every event it processes afterward with \"context canceled\" and redeliver forever")
		arriveShip("acme-post-switch-ship", "Rotterdam")
		Eventually(fleetShipIDs, 3*time.Second, 50*time.Millisecond).Should(ContainElement("acme-post-switch-ship"),
			"an event published after a REST-triggered switch must still reach its projector and land in the read model")
	})

	// BR-030 (BUSINESS_RULES-SHIPPING.md): a browser (Sea Freight Flow, Phase
	// 15d) connects directly to a tenant's account and never calls
	// SwitchTenant — before this, a tenant minted by accounts-service after
	// this process started had NO working api.* adapter until an operator
	// happened to switch the Admin UI to it (EnsureAllTenants only covers
	// tenants known at startup). EnsureTenantByName is what
	// composition.go's notify.accounts.account.created subscriber calls
	// reactively; this proves the piece that matters — that calling it makes
	// a tenant's adapter answer, without ever touching SwitchTenant.
	It("EnsureTenantByName provisions globex's api.* adapter without ever calling SwitchTenant for it", func() {
		globexNC, _ := connectAs("globex")

		By("before EnsureTenantByName, nothing is listening on globex's account — this process has never touched that tenant")
		_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 300*time.Millisecond)
		Expect(err).To(HaveOccurred(), "no reply should come back on globex's account before it's ever been ensured")

		By("EnsureTenantByName provisions it reactively")
		Expect(handlers.EnsureTenantByName(ctx, "globex")).To(Succeed())

		reply, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 2*time.Second)
		Expect(err).NotTo(HaveOccurred(), "globex's adapter must now answer, without SwitchTenant(\"globex\") ever having been called")
		var body struct {
			Ships []any `json:"ships"`
		}
		Expect(json.Unmarshal(reply.Data, &body)).To(Succeed())
		Expect(body.Ships).To(BeEmpty(), "a freshly-ensured tenant starts with no ships")

		By("calling it again for an already-ensured tenant is a harmless no-op")
		Expect(handlers.EnsureTenantByName(ctx, "acme")).To(Succeed())
	})

	It("EnsureTenantByName is a no-op, not an error, for a name with no creds file", func() {
		Expect(handlers.EnsureTenantByName(ctx, "no-such-tenant")).To(Succeed())
	})

	// BR-031 (BUSINESS_RULES-SHIPPING.md): the mirror of BR-030 — when
	// accounts-service suspends a tenant, its notify.accounts.account.suspended
	// subscriber (composition.go) calls TeardownTenantByName so this process
	// stops holding that tenant's connection open (and, left unhandled,
	// reconnect-looping against a .creds file the suspend has already
	// deleted — see ARCHITECTURE-ACCOUNTS.md § 2t-a). This proves the piece
	// that matters: calling it makes a previously-answering tenant adapter
	// go silent, by actually closing shipping-service's own connection to
	// that tenant's account rather than merely forgetting about it.
	It("TeardownTenantByName stops globex's api.* adapter by closing shipping-service's own connection to it", func() {
		By("ensuring globex has resources to tear down")
		Expect(handlers.EnsureTenantByName(ctx, "globex")).To(Succeed())

		globexNC, _ := connectAs("globex")
		_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 2*time.Second)
		Expect(err).NotTo(HaveOccurred(), "globex's adapter must be answering before teardown")

		By("tearing globex down")
		Expect(handlers.TeardownTenantByName(ctx, "globex")).To(Succeed())

		By("globex's adapter no longer answers — shipping-service's own connection to that account was closed, not just its handlers stopped locally")
		_, err = globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 300*time.Millisecond)
		Expect(err).To(HaveOccurred(), "no responder should remain on globex's account after teardown")

		By("calling it again for an already-torn-down tenant is a harmless no-op, not an error")
		Expect(handlers.TeardownTenantByName(ctx, "globex")).To(Succeed())
	})

	It("TeardownTenantByName is a no-op, not an error, for a tenant that was never provisioned", func() {
		Expect(handlers.TeardownTenantByName(ctx, "globex")).To(Succeed())
	})
})
