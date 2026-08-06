package dictionary

// Phase 13b integration spec (Main-POC-Plan.md, Phase 13b — creds mechanism
// updated in Phase 14a, server setup migrated to synthetic operator mode in
// Phase 24a): proves the tenant switch through the actual application path —
// rest.Handlers.SwitchTenant and real HTTP requests — not just a synthetic
// NATS-level check like 13a's. Uses a fully in-process synthetic NATS server
// (see tenant_testserver_test.go) instead of loading nats/nats.conf, so the
// spec stands alone from whatever bootstrap-operator.sh last produced.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/rest"
)

var _ = Describe("Phase 13b — tenant switch", func() {
	var (
		ctx      context.Context
		synthSrv *tenantSwitchServer
		handlers *rest.Handlers
		client   *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
		synthSrv = newTenantSwitchServer()

		deps := rest.Deps{
			Ports:         commands.NewPortHandler(newFakePortRepo()),
			Log:           slog.New(slog.DiscardHandler),
			ShipRepo:      newFakeRepo(),
			ContainerRepo: newFakeContainerRepo(),
			PortRepo:      newFakePortRepo(),
			NatsURL:       synthSrv.srv.ClientURL(),
			CredsDir:      synthSrv.CredsDir,
		}
		handlers = rest.NewHandlers(deps)
		Expect(handlers.SwitchTenant(ctx, "acme")).To(Succeed())

		mux := http.NewServeMux()
		handlers.Mount(mux)
		client = httptest.NewServer(mux)
		DeferCleanup(client.Close)
	})

	arriveShip := func(shipID, port string) {
		GinkgoHelper()
		body := `{"context":"acme-pacific-fleet","shipID":"` + shipID + `","shipName":"Tenant Spike","port":"` + port + `"}`
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
		_, acmeJS := synthSrv.connectAs("acme")
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
		_, globexJS := synthSrv.connectAs("globex")
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
		globexNC, _ := synthSrv.connectAs("globex")

		By("before EnsureTenantByName, nothing is listening on globex's account — this process has never touched that tenant")
		_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 300*time.Millisecond)
		Expect(err).To(HaveOccurred(), "no reply should come back on globex's account before it's ever been ensured")

		By("EnsureTenantByName provisions it reactively")
		Expect(handlers.EnsureTenantByName(ctx, "globex")).To(Succeed())

		// Eventually, not a single shot: micro.AddService does not flush its
		// subscriptions before returning, so a request issued in the same
		// instant can still get "no responders" — a test-only race (production
		// drives this from an async notify.* delivery, never from a caller
		// racing it) that made this spec fail roughly one run in four.
		var reply *nats.Msg
		Eventually(func() error {
			var err error
			reply, err = globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), time.Second)
			return err
		}, 3*time.Second, 25*time.Millisecond).Should(Succeed(),
			"globex's adapter must now answer, without SwitchTenant(\"globex\") ever having been called")
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

		globexNC, _ := synthSrv.connectAs("globex")
		Eventually(func() error {
			_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), time.Second)
			return err
		}, 3*time.Second, 25*time.Millisecond).Should(Succeed(),
			"globex's adapter must be answering before teardown") // see BR-030's spec above on why this polls

		By("tearing globex down")
		Expect(handlers.TeardownTenantByName(ctx, "globex")).To(Succeed())

		By("globex's adapter no longer answers — shipping-service's own connection to that account was closed, not just its handlers stopped locally")
		_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 300*time.Millisecond)
		Expect(err).To(HaveOccurred(), "no responder should remain on globex's account after teardown")

		By("calling it again for an already-torn-down tenant is a harmless no-op, not an error")
		Expect(handlers.TeardownTenantByName(ctx, "globex")).To(Succeed())
	})

	It("TeardownTenantByName is a no-op, not an error, for a tenant that was never provisioned", func() {
		Expect(handlers.TeardownTenantByName(ctx, "globex")).To(Succeed())
	})

	// BR-032 (BUSINESS_RULES-SHIPPING.md): the third leg of the lifecycle.
	// BR-031's teardown is a one-way door on its own — this proves the round
	// trip actually closes, i.e. that a tenant torn down on suspension can be
	// brought back by the same reactive path BR-030 uses, with no restart and
	// without SwitchTenant ever being called. Regression guard: before BR-032,
	// accounts-service published nothing on reactivation, so a suspended-then-
	// reactivated tenant stayed permanently dark to Sea Freight Flow.
	It("a torn-down tenant is fully restored by EnsureTenantByName, closing the suspend/reactivate round trip", func() {
		globexNC, _ := synthSrv.connectAs("globex")

		By("provisioned, then torn down as BR-031's suspension handler would")
		Expect(handlers.EnsureTenantByName(ctx, "globex")).To(Succeed())
		Eventually(func() error {
			_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), time.Second)
			return err
		}, 3*time.Second, 25*time.Millisecond).Should(Succeed(), "precondition: the tenant must be answering before we tear it down")
		Expect(handlers.TeardownTenantByName(ctx, "globex")).To(Succeed())
		_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 300*time.Millisecond)
		Expect(err).To(HaveOccurred(), "precondition: the tenant must genuinely be dark after teardown")

		By("reactivation's event handler rebuilds it from scratch")
		Expect(handlers.EnsureTenantByName(ctx, "globex")).To(Succeed())

		// Eventually, not a single shot: micro.AddService's subscriptions are
		// not flushed to the server before it returns, so a request issued in
		// the same instant can still get "no responders". Production never sees
		// this — the rebuild is driven by an async notify.* delivery, not by a
		// caller racing it — so polling here tests the real invariant (the
		// adapter comes back) without encoding a startup race as a hard timing
		// requirement.
		Eventually(func() error {
			_, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), time.Second)
			return err
		}, 3*time.Second, 25*time.Millisecond).Should(Succeed(),
			"a reactivated tenant must answer again without a restart or any SwitchTenant call")

		By("and the rebuilt tenant is fully functional, not just answering — a command it accepts must reach its projectors and land in its read model")
		arriveBody := `{"context":"acme-pacific-fleet","shipID":"globex-reactivated-ship","shipName":"Reactivated","port":"Hamburg"}`
		_, err = globexNC.Request("api.acme.shipping.ship.arrive.v1", []byte(arriveBody), 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() []any {
			reply, err := globexNC.Request("api.acme.shipping.ship.list.v1", []byte(`{}`), 2*time.Second)
			if err != nil {
				return nil
			}
			var out struct {
				Ships []any `json:"ships"`
			}
			if json.Unmarshal(reply.Data, &out) != nil {
				return nil
			}
			return out.Ships
		}, 3*time.Second, 50*time.Millisecond).Should(HaveLen(1),
			"the rebuilt tenant's projectors must be running too, not just its api.* adapter")
	})
})
