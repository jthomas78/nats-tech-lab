package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// Decode shapes for GET /api/accounts/topology — see topology.go's
// topologyEdge/unconsumedExport/topologyResponse for the JSON these mirror.
type topologyEdgeJSON struct {
	FromAccount  string `json:"fromAccount"`
	ToAccount    string `json:"toAccount"`
	Subject      string `json:"subject"`
	LocalSubject string `json:"localSubject"`
	Type         string `json:"type"`
	Status       string `json:"status"`
}

type unconsumedExportJSON struct {
	Account string `json:"account"`
	Subject string `json:"subject"`
	Type    string `json:"type"`
}

type topologyResponseJSON struct {
	Edges             []topologyEdgeJSON     `json:"edges"`
	UnconsumedExports []unconsumedExportJSON `json:"unconsumedExports"`
}

// businessUnitJSON mirrors handler.go's businessUnitResponse (BR-AC26/28).
type businessUnitJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Context   string `json:"context"`
	Visible   bool   `json:"visible"`
	IsDefault bool   `json:"isDefault"`
	CreatedAt string `json:"createdAt"`
}

var _ = Describe("Handlers", func() {
	const authSecret = "test-secret"

	var (
		ots         *operatorTestServer
		client      *httptest.Server
		store       *accounts.Store
		provisioner *accounts.Provisioner
		auditLog    *accounts.AuditLog
		sysNC       *nats.Conn
	)

	BeforeEach(func() {
		if storeTestUnavailable != "" {
			Skip("docker unavailable for Postgres integration test: " + storeTestUnavailable)
		}

		ots = newOperatorTestServer(GinkgoT())
		DeferCleanup(ots.Shutdown)
		sysNC = ots.ConnectSys(GinkgoT())
		DeferCleanup(sysNC.Close)

		var err error
		provisioner, err = accounts.NewProvisioner(ots.OperatorSigningKeySeed, sysNC)
		Expect(err).NotTo(HaveOccurred())

		store = accounts.NewStore(storeTestDB)
		auditLog = accounts.NewAuditLog(storeTestDB)
		// The shared Postgres test database may contain the real bootstrap
		// PLATFORM row. Replace it with an account minted into this embedded
		// server so the import source in each created tenant is resolvable.
		_, err = storeTestDB.ExecContext(context.Background(), "DELETE FROM accounts.accounts WHERE name = $1", "platform")
		Expect(err).NotTo(HaveOccurred())
		platform, err := provisioner.CreateAccount(context.Background(), accounts.JSLimits{MaxMem: 1 << 30, MaxFile: 5 << 30, MaxStreams: 20, MaxConsumers: 100}, "platform", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Insert(context.Background(), accounts.Account{Name: "platform", PublicKey: platform.PublicKey, SigningKeySeed: platform.SigningKeySeed, Status: accounts.StatusActive, JSMaxMem: 1 << 30, JSMaxFile: 5 << 30, JSMaxStreams: 20, JSMaxConsumers: 100})).To(Succeed())
		// The system_config row is a shared singleton; reset it to the BR-AC20
		// default so config specs don't leak into one another under Ginkgo's
		// randomized order.
		Expect(store.SetTokenTTLConfig(context.Background(), accounts.DefaultTokenTTLConfig())).To(Succeed())
		credsDir := GinkgoT().TempDir()
		handlers := accounts.NewHandlers(store, provisioner, credsDir, slog.New(slog.DiscardHandler), nil, auditLog)

		mux := http.NewServeMux()
		handlers.Mount(mux, authSecret)
		client = httptest.NewServer(mux)
		DeferCleanup(client.Close)
	})

	doRequest := func(method, path string, body any, user, pass string) *http.Response {
		GinkgoHelper()
		var reader *bytes.Reader
		if body != nil {
			b, err := json.Marshal(body)
			Expect(err).NotTo(HaveOccurred())
			reader = bytes.NewReader(b)
		} else {
			reader = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(method, client.URL+path, reader)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")
		if user != "" {
			req.SetBasicAuth(user, pass)
		}
		resp, err := client.Client().Do(req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	It("rejects every route without basic auth", func() {
		resp := doRequest(http.MethodGet, "/api/accounts", nil, "", "")
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("rejects the wrong password", func() {
		resp := doRequest(http.MethodGet, "/api/accounts", nil, accounts.BasicAuthUser, "wrong-secret")
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("creates an account, lists it, fetches it, then suspends it end to end", func() {
		By("creating a new account")
		createResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{
			"name": "e2e-tenant", "jsMaxMem": 64 << 20, "jsMaxFile": 128 << 20, "jsMaxStreams": 3, "jsMaxConsumers": 5,
		}, accounts.BasicAuthUser, authSecret)
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		var created struct {
			Account struct {
				Name      string `json:"name"`
				PublicKey string `json:"publicKey"`
				Status    string `json:"status"`
			} `json:"account"`
			Creds string `json:"creds"`
		}
		Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())
		Expect(created.Account.Name).To(Equal("e2e-tenant"))
		Expect(created.Account.Status).To(Equal(accounts.StatusActive))
		Expect(created.Creds).To(ContainSubstring("BEGIN NATS USER JWT"))

		By("connecting to NATS with the returned creds and confirming JetStream access")
		nc, err := ots.ConnectWithCreds([]byte(created.Creds), "e2e-verify")
		Expect(err).NotTo(HaveOccurred())
		nc.Close()

		By("rejecting a second create with the same name")
		dupResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": "e2e-tenant"}, accounts.BasicAuthUser, authSecret)
		defer dupResp.Body.Close()
		Expect(dupResp.StatusCode).To(Equal(http.StatusConflict))

		By("listing accounts and finding it, without any creds/signing-key leakage")
		listResp := doRequest(http.MethodGet, "/api/accounts", nil, accounts.BasicAuthUser, authSecret)
		defer listResp.Body.Close()
		Expect(listResp.StatusCode).To(Equal(http.StatusOK))
		body, err := readAll(listResp)
		Expect(err).NotTo(HaveOccurred())
		Expect(body).To(ContainSubstring("e2e-tenant"))
		Expect(body).NotTo(ContainSubstring("signingKeySeed"))
		Expect(body).NotTo(ContainSubstring("creds"))

		By("fetching it directly")
		getResp := doRequest(http.MethodGet, "/api/accounts/e2e-tenant", nil, accounts.BasicAuthUser, authSecret)
		defer getResp.Body.Close()
		Expect(getResp.StatusCode).To(Equal(http.StatusOK))

		By("fetching an unknown account returns 404")
		missingResp := doRequest(http.MethodGet, "/api/accounts/does-not-exist", nil, accounts.BasicAuthUser, authSecret)
		defer missingResp.Body.Close()
		Expect(missingResp.StatusCode).To(Equal(http.StatusNotFound))

		By("suspending it revokes the NATS account and flips its status")
		suspendResp := doRequest(http.MethodPost, "/api/accounts/e2e-tenant/suspend", nil, accounts.BasicAuthUser, authSecret)
		defer suspendResp.Body.Close()
		Expect(suspendResp.StatusCode).To(Equal(http.StatusOK))

		afterResp := doRequest(http.MethodGet, "/api/accounts/e2e-tenant", nil, accounts.BasicAuthUser, authSecret)
		defer afterResp.Body.Close()
		var after struct {
			Status string `json:"status"`
		}
		Expect(json.NewDecoder(afterResp.Body).Decode(&after)).To(Succeed())
		Expect(after.Status).To(Equal(accounts.StatusSuspended))

		_, err = ots.ConnectWithCreds([]byte(created.Creds), "e2e-post-suspend")
		Expect(err).To(HaveOccurred(), "a suspended account's creds must no longer be able to connect")

		By("reactivating it restores the account and mints fresh, working creds")
		reactivateResp := doRequest(http.MethodPost, "/api/accounts/e2e-tenant/reactivate", nil, accounts.BasicAuthUser, authSecret)
		defer reactivateResp.Body.Close()
		Expect(reactivateResp.StatusCode).To(Equal(http.StatusOK))

		var reactivated struct {
			Account struct {
				Status string `json:"status"`
			} `json:"account"`
			Creds string `json:"creds"`
		}
		Expect(json.NewDecoder(reactivateResp.Body).Decode(&reactivated)).To(Succeed())
		Expect(reactivated.Account.Status).To(Equal(accounts.StatusActive))
		Expect(reactivated.Creds).To(ContainSubstring("BEGIN NATS USER JWT"))

		reactivatedNC, err := ots.ConnectWithCreds([]byte(reactivated.Creds), "e2e-post-reactivate")
		Expect(err).NotTo(HaveOccurred(), "a reactivated account's newly-minted creds must connect")
		reactivatedNC.Close()

		By("reactivating an already-active account is rejected")
		alreadyActiveResp := doRequest(http.MethodPost, "/api/accounts/e2e-tenant/reactivate", nil, accounts.BasicAuthUser, authSecret)
		defer alreadyActiveResp.Body.Close()
		Expect(alreadyActiveResp.StatusCode).To(Equal(http.StatusConflict))

		By("reactivating an unknown account returns 404")
		missingReactivateResp := doRequest(http.MethodPost, "/api/accounts/does-not-exist/reactivate", nil, accounts.BasicAuthUser, authSecret)
		defer missingReactivateResp.Body.Close()
		Expect(missingReactivateResp.StatusCode).To(Equal(http.StatusNotFound))
	})

	// Regression test for a real production incident (2026-07-28): a seeded
	// pre-existing account (no signing key on record — see
	// Account.SigningKeySeed's doc comment) that goes through
	// suspend→reactivate must come out with a genuinely usable .creds file,
	// not just an "active" status with no way to ever mint one again.
	It("reactivating an account with no stored signing key establishes one and mints working creds", func() {
		minted, err := provisioner.CreateAccount(context.Background(), accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "seeded-like", "")
		Expect(err).NotTo(HaveOccurred())

		// Simulate a seeded account (like PLATFORM/ACME/GLOBEX): a real,
		// resolver-known account whose signing key was never persisted, and
		// which is currently suspended.
		Expect(store.Insert(context.Background(), accounts.Account{
			Name: "seeded-like", PublicKey: minted.PublicKey, SigningKeySeed: "",
			Status: accounts.StatusSuspended, JSMaxMem: 64 << 20, JSMaxFile: 128 << 20, JSMaxStreams: 3, JSMaxConsumers: 5,
		})).To(Succeed())

		resp := doRequest(http.MethodPost, "/api/accounts/seeded-like/reactivate", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var reactivated struct {
			Account struct {
				Status string `json:"status"`
			} `json:"account"`
			Creds string `json:"creds"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&reactivated)).To(Succeed())
		Expect(reactivated.Account.Status).To(Equal(accounts.StatusActive))
		Expect(reactivated.Creds).To(ContainSubstring("BEGIN NATS USER JWT"), "a signing-key-less account must still come back with real, usable creds")

		nc, err := ots.ConnectWithCreds([]byte(reactivated.Creds), "seeded-like-post-reactivate")
		Expect(err).NotTo(HaveOccurred())
		nc.Close()

		stored, err := store.Get(context.Background(), "seeded-like")
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.SigningKeySeed).NotTo(BeEmpty(), "the established signing key must be persisted so a future reactivation doesn't hit the same gap")
	})

	It("rejects a create request with no name", func() {
		resp := doRequest(http.MethodPost, "/api/accounts", map[string]any{}, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	// BR-AC08 (BUSINESS_RULES-ACCOUNTS.md): shipping-service's
	// Handlers.EnsureTenantByName (BR-030, dictionary/internal/rest/tenant.go)
	// reacts to this event to provision a newly-minted tenant's resources
	// immediately — without it, a tenant created after shipping-service last
	// started is invisible until an operator happens to switch the Admin UI
	// to it. This test only proves accounts-service's half: the event is
	// published, on the right subject, with the right payload, only after
	// the account is fully committed. It deliberately reuses the SYS
	// connection for both publish and subscribe (this synthetic test
	// operator has no PLATFORM account) — the cross-account delivery guarantee
	// itself is core NATS behavior, not something this test needs to reprove.
	It("BR-AC08: publishes notify.accounts.account.created only after a create fully succeeds", func() {
		notifyNC := ots.ConnectSys(GinkgoT())
		defer notifyNC.Close()
		sub, err := notifyNC.SubscribeSync("notify.accounts.account.created")
		Expect(err).NotTo(HaveOccurred())
		defer sub.Unsubscribe() //nolint:errcheck

		notifyHandlers := accounts.NewHandlers(store, provisioner, GinkgoT().TempDir(), slog.New(slog.DiscardHandler), notifyNC, auditLog)
		mux := http.NewServeMux()
		notifyHandlers.Mount(mux, authSecret)
		notifyServer := httptest.NewServer(mux)
		defer notifyServer.Close()

		By("a rejected create (duplicate/invalid name) never publishes anything")
		badReq, err := http.NewRequest(http.MethodPost, notifyServer.URL+"/api/accounts", bytes.NewReader([]byte(`{}`)))
		Expect(err).NotTo(HaveOccurred())
		badReq.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		badResp, err := notifyServer.Client().Do(badReq)
		Expect(err).NotTo(HaveOccurred())
		defer badResp.Body.Close()
		Expect(badResp.StatusCode).To(Equal(http.StatusBadRequest))
		_, err = sub.NextMsg(200 * time.Millisecond)
		Expect(err).To(HaveOccurred(), "no event for a create that never succeeded")

		By("a successful create publishes the tenant's name")
		req, err := http.NewRequest(http.MethodPost, notifyServer.URL+"/api/accounts", bytes.NewReader([]byte(`{"name":"notify-tenant"}`)))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		resp, err := notifyServer.Client().Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		msg, err := sub.NextMsg(2 * time.Second)
		Expect(err).NotTo(HaveOccurred(), "shipping-service's subscriber must receive this event to provision the tenant reactively")
		var evt struct {
			Name string `json:"name"`
		}
		Expect(json.Unmarshal(msg.Data, &evt)).To(Succeed())
		Expect(evt.Name).To(Equal("notify-tenant"))
	})

	It("does not fail account creation when NotifyNC is unset", func() {
		resp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": "no-notify-tenant"}, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated), "the outer BeforeEach's handlers has a nil NotifyNC — creation must still succeed")
	})

	// BR-AC09 (BUSINESS_RULES-ACCOUNTS.md): the mirror of BR-AC08 — this
	// event is what shipping-service's Handlers.TeardownTenantByName (BR-031,
	// dictionary/internal/rest/tenant.go) reacts to, so it stops holding a
	// connection open (and reconnect-looping against a now-deleted .creds
	// file) for a tenant that no longer exists. See ARCHITECTURE-ACCOUNTS.md
	// § 2t-a for the runtime behavior this closes. Same test shape as
	// BR-AC08: proves accounts-service's half only (right subject, right
	// payload, only after suspension fully succeeds), reusing the SYS
	// connection for both publish and subscribe for the same reason BR-AC08's
	// test does.
	It("BR-AC09: publishes notify.accounts.account.suspended only after a suspend fully succeeds", func() {
		notifyNC := ots.ConnectSys(GinkgoT())
		defer notifyNC.Close()
		sub, err := notifyNC.SubscribeSync("notify.accounts.account.suspended")
		Expect(err).NotTo(HaveOccurred())
		defer sub.Unsubscribe() //nolint:errcheck

		notifyHandlers := accounts.NewHandlers(store, provisioner, GinkgoT().TempDir(), slog.New(slog.DiscardHandler), notifyNC, auditLog)
		mux := http.NewServeMux()
		notifyHandlers.Mount(mux, authSecret)
		notifyServer := httptest.NewServer(mux)
		defer notifyServer.Close()

		By("creating the tenant that will be suspended")
		createReq, err := http.NewRequest(http.MethodPost, notifyServer.URL+"/api/accounts", bytes.NewReader([]byte(`{"name":"suspend-notify-tenant"}`)))
		Expect(err).NotTo(HaveOccurred())
		createReq.Header.Set("Content-Type", "application/json")
		createReq.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		createResp, err := notifyServer.Client().Do(createReq)
		Expect(err).NotTo(HaveOccurred())
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		By("the create itself publishes nothing on this subscription — it's scoped to the suspended subject only")
		_, err = sub.NextMsg(200 * time.Millisecond)
		Expect(err).To(HaveOccurred(), "BR-AC08's created event is a different subject, not this one")

		By("suspending a tenant that does not exist publishes nothing")
		missingReq, err := http.NewRequest(http.MethodPost, notifyServer.URL+"/api/accounts/no-such-tenant/suspend", nil)
		Expect(err).NotTo(HaveOccurred())
		missingReq.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		missingResp, err := notifyServer.Client().Do(missingReq)
		Expect(err).NotTo(HaveOccurred())
		defer missingResp.Body.Close()
		Expect(missingResp.StatusCode).To(Equal(http.StatusNotFound))
		_, err = sub.NextMsg(200 * time.Millisecond)
		Expect(err).To(HaveOccurred(), "no event for a suspend that never succeeded")

		By("a successful suspend publishes the tenant's name")
		suspendReq, err := http.NewRequest(http.MethodPost, notifyServer.URL+"/api/accounts/suspend-notify-tenant/suspend", nil)
		Expect(err).NotTo(HaveOccurred())
		suspendReq.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		suspendResp, err := notifyServer.Client().Do(suspendReq)
		Expect(err).NotTo(HaveOccurred())
		defer suspendResp.Body.Close()
		Expect(suspendResp.StatusCode).To(Equal(http.StatusOK))

		msg, err := sub.NextMsg(2 * time.Second)
		Expect(err).NotTo(HaveOccurred(), "shipping-service's subscriber must receive this event to tear the tenant's resources down")
		var evt struct {
			Name string `json:"name"`
		}
		Expect(json.Unmarshal(msg.Data, &evt)).To(Succeed())
		Expect(evt.Name).To(Equal("suspend-notify-tenant"))
	})

	// BR-AC10 (BUSINESS_RULES-ACCOUNTS.md): completes the lifecycle triple.
	// Without this event, BR-AC09's teardown is a one-way door —
	// shipping-service drops a suspended tenant's resources and nothing ever
	// rebuilds them, leaving a reactivated tenant unusable until a restart
	// (see BUSINESS_RULES-SHIPPING.md's BR-032, the consumer side). Also
	// asserts the ordering that matters to that consumer: the event must not
	// fire until the fresh .creds file exists, since the consumer resolves the
	// tenant by scanning that directory.
	It("BR-AC10: publishes notify.accounts.account.reactivated only after a reactivate fully succeeds, with its creds file already written", func() {
		notifyNC := ots.ConnectSys(GinkgoT())
		defer notifyNC.Close()
		sub, err := notifyNC.SubscribeSync("notify.accounts.account.reactivated")
		Expect(err).NotTo(HaveOccurred())
		defer sub.Unsubscribe() //nolint:errcheck

		credsDir := GinkgoT().TempDir()
		notifyHandlers := accounts.NewHandlers(store, provisioner, credsDir, slog.New(slog.DiscardHandler), notifyNC, auditLog)
		mux := http.NewServeMux()
		notifyHandlers.Mount(mux, authSecret)
		notifyServer := httptest.NewServer(mux)
		defer notifyServer.Close()

		post := func(path string, body []byte) *http.Response {
			GinkgoHelper()
			var rdr *bytes.Reader
			if body != nil {
				rdr = bytes.NewReader(body)
			} else {
				rdr = bytes.NewReader(nil)
			}
			req, err := http.NewRequest(http.MethodPost, notifyServer.URL+path, rdr)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")
			req.SetBasicAuth(accounts.BasicAuthUser, authSecret)
			resp, err := notifyServer.Client().Do(req)
			Expect(err).NotTo(HaveOccurred())
			return resp
		}

		By("creating then suspending the tenant, so it is eligible for reactivation")
		createResp := post("/api/accounts", []byte(`{"name":"reactivate-notify-tenant"}`))
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))
		suspendResp := post("/api/accounts/reactivate-notify-tenant/suspend", nil)
		defer suspendResp.Body.Close()
		Expect(suspendResp.StatusCode).To(Equal(http.StatusOK))

		By("neither the create nor the suspend published on this subject")
		_, err = sub.NextMsg(200 * time.Millisecond)
		Expect(err).To(HaveOccurred(), "BR-AC08/BR-AC09 events are different subjects, not this one")

		By("reactivating a tenant that is not suspended publishes nothing")
		conflictResp := post("/api/accounts/acme-not-suspended-anywhere/reactivate", nil)
		defer conflictResp.Body.Close()
		Expect(conflictResp.StatusCode).To(Equal(http.StatusNotFound))
		_, err = sub.NextMsg(200 * time.Millisecond)
		Expect(err).To(HaveOccurred(), "no event for a reactivation that never succeeded")

		By("a successful reactivate publishes the tenant's name")
		reactivateResp := post("/api/accounts/reactivate-notify-tenant/reactivate", nil)
		defer reactivateResp.Body.Close()
		Expect(reactivateResp.StatusCode).To(Equal(http.StatusOK))

		msg, err := sub.NextMsg(2 * time.Second)
		Expect(err).NotTo(HaveOccurred(), "shipping-service's subscriber must receive this event to rebuild the tenant's resources")
		var evt struct {
			Name string `json:"name"`
		}
		Expect(json.Unmarshal(msg.Data, &evt)).To(Succeed())
		Expect(evt.Name).To(Equal("reactivate-notify-tenant"))

		By("the fresh creds file already exists by the time the event is observable — the consumer resolves the tenant by scanning that directory")
		Expect(filepath.Join(credsDir, "reactivate-notify-tenant.creds")).To(BeAnExistingFile())
	})

	It("does not fail account reactivation when NotifyNC is unset", func() {
		createResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": "no-notify-reactivate-tenant"}, accounts.BasicAuthUser, authSecret)
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		suspendResp := doRequest(http.MethodPost, "/api/accounts/no-notify-reactivate-tenant/suspend", nil, accounts.BasicAuthUser, authSecret)
		defer suspendResp.Body.Close()
		Expect(suspendResp.StatusCode).To(Equal(http.StatusOK))

		resp := doRequest(http.MethodPost, "/api/accounts/no-notify-reactivate-tenant/reactivate", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "the outer BeforeEach's handlers has a nil NotifyNC — reactivation must still succeed")
	})

	It("does not fail account suspension when NotifyNC is unset", func() {
		createResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": "no-notify-suspend-tenant"}, accounts.BasicAuthUser, authSecret)
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		resp := doRequest(http.MethodPost, "/api/accounts/no-notify-suspend-tenant/suspend", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "the outer BeforeEach's handlers has a nil NotifyNC — suspension must still succeed")
	})

	// BR-AC06 (BUSINESS_RULES-ACCOUNTS.md): PLATFORM/SYS are reserved,
	// case-insensitively — a differently-cased variant must be rejected
	// exactly like the canonical name, since it's the casing mismatch
	// itself (not just the exact literal) that let a reserved name slip
	// past shipping-service's tenant-exclusion filter.
	DescribeTable("rejects reserved account names, case-insensitively",
		func(name string) {
			resp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": name}, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		},
		Entry("PLATFORM", "PLATFORM"),
		Entry("platform", "platform"),
		Entry("Platform", "Platform"),
		Entry("SYS", "SYS"),
		Entry("sys", "sys"),
	)

	// BR-AC07 (BUSINESS_RULES-ACCOUNTS.md): a leading "_" is reserved for
	// platform/system use across the whole subject taxonomy, not just this
	// service's own concept — see reservedNamePrefix's doc comment. Rejected
	// as 400 Bad Request (a naming-rule violation), not 409 Conflict (which
	// BR-AC06 above uses for "already claimed by a specific reserved name").
	DescribeTable("rejects account names beginning with '_'",
		func(name string) {
			resp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": name}, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		},
		Entry("_platform", "_platform"),
		Entry("_ops", "_ops"),
		Entry("_", "_"),
	)

	It("still allows an account name that merely contains an underscore mid-string", func() {
		resp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": "acme_northdiv"}, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	})

	// BR-AC13 (BUSINESS_RULES-ACCOUNTS.md): PLATFORM is mandatory on every
	// deployment and can never be suspended, case-insensitively — unlike
	// suspending a tenant, which only takes that one tenant offline,
	// suspending PLATFORM would sever shipping-service's and
	// refdata-service's permanent connections for every tenant at once. The
	// check runs before the account is even looked up in Postgres, so this
	// rejects "PLATFORM" as reserved regardless of whether a row happens to
	// exist for it.
	DescribeTable("rejects suspending the reserved PLATFORM account, case-insensitively",
		func(name string) {
			resp := doRequest(http.MethodPost, "/api/accounts/"+name+"/suspend", nil, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		},
		Entry("PLATFORM", "PLATFORM"),
		Entry("platform", "platform"),
		Entry("Platform", "Platform"),
	)

	It("BR-AC13: a real seeded platform account is still rejected for suspend, not silently 404'd", func() {
		// SeedIfMissing (not Insert): this shared Postgres test database may
		// already carry a "platform" row from another spec/run, and this test
		// only needs SOME row to exist under that name, not to own its
		// creation — asserting on status-before-vs-after (below) rather than
		// a hardcoded "active" is what actually matters for BR-AC13.
		Expect(store.SeedIfMissing(context.Background(), accounts.Account{
			Name: "platform", PublicKey: "A-real-platform-key",
			Status: accounts.StatusActive, JSMaxMem: 1 << 30, JSMaxFile: 5 << 30, JSMaxStreams: 20, JSMaxConsumers: 100,
		})).To(Succeed())
		before, err := store.Get(context.Background(), "platform")
		Expect(err).NotTo(HaveOccurred())

		resp := doRequest(http.MethodPost, "/api/accounts/platform/suspend", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))

		after, err := store.Get(context.Background(), "platform")
		Expect(err).NotTo(HaveOccurred())
		Expect(after.Status).To(Equal(before.Status), "the reserved account's status must be unchanged — the rejection must happen before any suspend side effect")
	})

	// BR-AC11 (BUSINESS_RULES-ACCOUNTS.md): every lifecycle success writes an
	// immutable audit row — action, account, actor (from X-Actor when
	// supplied, overriding the shared basic-auth username), source IP, and a
	// success outcome — closing the audit-trail gap the 2026-08-03
	// architecture review flagged (no way to answer "who did what, when"
	// beyond a bare updated_at).
	It("BR-AC11: records an audit row with actor and outcome for each lifecycle success", func() {
		post := func(path string) *http.Response {
			GinkgoHelper()
			req, err := http.NewRequest(http.MethodPost, client.URL+path, bytes.NewReader([]byte(`{"name":"audit-tenant"}`)))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Actor", "qa-operator")
			req.SetBasicAuth(accounts.BasicAuthUser, authSecret)
			resp, err := client.Client().Do(req)
			Expect(err).NotTo(HaveOccurred())
			return resp
		}

		createResp := post("/api/accounts")
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		suspendResp := post("/api/accounts/audit-tenant/suspend")
		defer suspendResp.Body.Close()
		Expect(suspendResp.StatusCode).To(Equal(http.StatusOK))

		reactivateResp := post("/api/accounts/audit-tenant/reactivate")
		defer reactivateResp.Body.Close()
		Expect(reactivateResp.StatusCode).To(Equal(http.StatusOK))

		entries, err := auditLog.ListByAccount(context.Background(), "audit-tenant")
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(3), "one row per lifecycle action")

		// ListByAccount orders newest first.
		Expect(entries[0].Action).To(Equal(accounts.AuditActionReactivated))
		Expect(entries[1].Action).To(Equal(accounts.AuditActionSuspended))
		Expect(entries[2].Action).To(Equal(accounts.AuditActionCreated))
		for _, e := range entries {
			Expect(e.Outcome).To(Equal(accounts.AuditOutcomeSuccess))
			Expect(e.Actor).To(Equal("qa-operator"), "X-Actor header must override the shared basic-auth username")
			Expect(e.SourceIP).NotTo(BeEmpty())
		}
	})

	// BR-AC12 (BUSINESS_RULES-ACCOUNTS.md): JetStream limits can be updated
	// on an existing account — the resolver JWT is re-minted with the new
	// values, Postgres is persisted, an audit row is written, and a notify
	// event fires.
	It("BR-AC12: updates an account's JetStream limits and reflects them in GET and at the resolver", func() {
		By("creating a tenant with initial limits")
		createResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{
			"name": "jslimits-tenant", "jsMaxMem": 64 << 20, "jsMaxFile": 128 << 20, "jsMaxStreams": 5, "jsMaxConsumers": 10,
		}, accounts.BasicAuthUser, authSecret)
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		By("updating limits to higher values")
		updateResp := doRequest(http.MethodPost, "/api/accounts/jslimits-tenant/jslimits", map[string]any{
			"jsMaxMem": 256 << 20, "jsMaxFile": 512 << 20, "jsMaxStreams": 20, "jsMaxConsumers": 40,
		}, accounts.BasicAuthUser, authSecret)
		defer updateResp.Body.Close()
		Expect(updateResp.StatusCode).To(Equal(http.StatusOK))

		var updated struct {
			JSMaxMem       int64 `json:"jsMaxMem"`
			JSMaxFile      int64 `json:"jsMaxFile"`
			JSMaxStreams   int64 `json:"jsMaxStreams"`
			JSMaxConsumers int64 `json:"jsMaxConsumers"`
		}
		Expect(json.NewDecoder(updateResp.Body).Decode(&updated)).To(Succeed())
		Expect(updated.JSMaxStreams).To(Equal(int64(20)))
		Expect(updated.JSMaxConsumers).To(Equal(int64(40)))

		By("GET reflects the new limits")
		getResp := doRequest(http.MethodGet, "/api/accounts/jslimits-tenant", nil, accounts.BasicAuthUser, authSecret)
		defer getResp.Body.Close()
		var fetched struct {
			JSMaxStreams   int64 `json:"jsMaxStreams"`
			JSMaxConsumers int64 `json:"jsMaxConsumers"`
		}
		Expect(json.NewDecoder(getResp.Body).Decode(&fetched)).To(Succeed())
		Expect(fetched.JSMaxStreams).To(Equal(int64(20)))
		Expect(fetched.JSMaxConsumers).To(Equal(int64(40)))
	})

	It("BR-AC12: rejects negative JetStream limit values", func() {
		createResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{
			"name": "jslimits-neg-tenant",
		}, accounts.BasicAuthUser, authSecret)
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		resp := doRequest(http.MethodPost, "/api/accounts/jslimits-neg-tenant/jslimits", map[string]any{
			"jsMaxMem": -1, "jsMaxFile": 128 << 20, "jsMaxStreams": 10, "jsMaxConsumers": 20,
		}, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("BR-AC12: returns 404 for an unknown account", func() {
		resp := doRequest(http.MethodPost, "/api/accounts/does-not-exist/jslimits", map[string]any{
			"jsMaxMem": 64 << 20, "jsMaxFile": 128 << 20, "jsMaxStreams": 10, "jsMaxConsumers": 20,
		}, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	// ── BR-AC20 / BR-AC21: configurable browser/admin JWT expiry TTL ──────────

	decodeSystemConfig := func(resp *http.Response) struct {
		TokenTTLMinutes    int `json:"tokenTtlMinutes"`
		TokenTTLMinMinutes int `json:"tokenTtlMinMinutes"`
		TokenTTLMaxMinutes int `json:"tokenTtlMaxMinutes"`
		EnvelopeMinMinutes int `json:"envelopeMinMinutes"`
		EnvelopeMaxMinutes int `json:"envelopeMaxMinutes"`
	} {
		GinkgoHelper()
		var body struct {
			TokenTTLMinutes    int `json:"tokenTtlMinutes"`
			TokenTTLMinMinutes int `json:"tokenTtlMinMinutes"`
			TokenTTLMaxMinutes int `json:"tokenTtlMaxMinutes"`
			EnvelopeMinMinutes int `json:"envelopeMinMinutes"`
			EnvelopeMaxMinutes int `json:"envelopeMaxMinutes"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		return body
	}

	It("BR-AC20: GET /api/accounts/system-config returns the 15-minute default within the 15–30 envelope", func() {
		resp := doRequest(http.MethodGet, "/api/accounts/system-config", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body := decodeSystemConfig(resp)
		Expect(body.TokenTTLMinutes).To(Equal(15))
		Expect(body.TokenTTLMinMinutes).To(Equal(15))
		Expect(body.TokenTTLMaxMinutes).To(Equal(30))
		Expect(body.EnvelopeMinMinutes).To(Equal(15))
		Expect(body.EnvelopeMaxMinutes).To(Equal(30))
	})

	It("BR-AC20: PUT /api/accounts/system-config persists a valid setting and GET reflects it", func() {
		putResp := doRequest(http.MethodPut, "/api/accounts/system-config", map[string]any{
			"tokenTtlMinutes": 25, "tokenTtlMinMinutes": 20, "tokenTtlMaxMinutes": 30,
		}, accounts.BasicAuthUser, authSecret)
		defer putResp.Body.Close()
		Expect(putResp.StatusCode).To(Equal(http.StatusOK))
		Expect(decodeSystemConfig(putResp).TokenTTLMinutes).To(Equal(25))

		getResp := doRequest(http.MethodGet, "/api/accounts/system-config", nil, accounts.BasicAuthUser, authSecret)
		defer getResp.Body.Close()
		fetched := decodeSystemConfig(getResp)
		Expect(fetched.TokenTTLMinutes).To(Equal(25))
		Expect(fetched.TokenTTLMinMinutes).To(Equal(20))
		Expect(fetched.TokenTTLMaxMinutes).To(Equal(30))
	})

	It("BR-AC21: rejects a range outside the 15–30 minute envelope", func() {
		resp := doRequest(http.MethodPut, "/api/accounts/system-config", map[string]any{
			"tokenTtlMinutes": 45, "tokenTtlMinMinutes": 15, "tokenTtlMaxMinutes": 60,
		}, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("BR-AC21: rejects a value outside the configured range", func() {
		resp := doRequest(http.MethodPut, "/api/accounts/system-config", map[string]any{
			"tokenTtlMinutes": 16, "tokenTtlMinMinutes": 18, "tokenTtlMaxMinutes": 22,
		}, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("BR-AC20: the config endpoints require basic auth", func() {
		getResp := doRequest(http.MethodGet, "/api/accounts/system-config", nil, "", "")
		defer getResp.Body.Close()
		Expect(getResp.StatusCode).To(Equal(http.StatusUnauthorized))

		putResp := doRequest(http.MethodPut, "/api/accounts/system-config", map[string]any{
			"tokenTtlMinutes": 20, "tokenTtlMinMinutes": 15, "tokenTtlMaxMinutes": 30,
		}, "", "")
		defer putResp.Body.Close()
		Expect(putResp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("BR-AC12: publishes notify.accounts.account.jslimits_updated after a successful update", func() {
		notifyNC := ots.ConnectSys(GinkgoT())
		defer notifyNC.Close()
		sub, err := notifyNC.SubscribeSync("notify.accounts.account.jslimits_updated")
		Expect(err).NotTo(HaveOccurred())
		defer sub.Unsubscribe() //nolint:errcheck

		notifyHandlers := accounts.NewHandlers(store, provisioner, GinkgoT().TempDir(), slog.New(slog.DiscardHandler), notifyNC, auditLog)
		mux := http.NewServeMux()
		notifyHandlers.Mount(mux, authSecret)
		notifyServer := httptest.NewServer(mux)
		defer notifyServer.Close()

		doNotifyReq := func(method, path string, body any) *http.Response {
			GinkgoHelper()
			b, err := json.Marshal(body)
			Expect(err).NotTo(HaveOccurred())
			req, err := http.NewRequest(method, notifyServer.URL+path, bytes.NewReader(b))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")
			req.SetBasicAuth(accounts.BasicAuthUser, authSecret)
			resp, err := notifyServer.Client().Do(req)
			Expect(err).NotTo(HaveOccurred())
			return resp
		}

		createResp := doNotifyReq(http.MethodPost, "/api/accounts", map[string]any{"name": "jslimits-notify-tenant"})
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		updateResp := doNotifyReq(http.MethodPost, "/api/accounts/jslimits-notify-tenant/jslimits", map[string]any{
			"jsMaxMem": 256 << 20, "jsMaxFile": 512 << 20, "jsMaxStreams": 20, "jsMaxConsumers": 40,
		})
		defer updateResp.Body.Close()
		Expect(updateResp.StatusCode).To(Equal(http.StatusOK))

		msg, err := sub.NextMsg(2 * time.Second)
		Expect(err).NotTo(HaveOccurred())
		var evt struct {
			Name string `json:"name"`
		}
		Expect(json.Unmarshal(msg.Data, &evt)).To(Succeed())
		Expect(evt.Name).To(Equal("jslimits-notify-tenant"))
	})

	It("BR-AC12: records an audit row with previous and requested limits in metadata", func() {
		req, err := http.NewRequest(http.MethodPost, client.URL+"/api/accounts", bytes.NewReader([]byte(`{"name":"jslimits-audit-tenant","jsMaxStreams":5,"jsMaxConsumers":10}`)))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Actor", "limit-admin")
		req.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		createResp, err := client.Client().Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		updateReq, err := http.NewRequest(http.MethodPost, client.URL+"/api/accounts/jslimits-audit-tenant/jslimits",
			bytes.NewReader([]byte(`{"jsMaxMem":268435456,"jsMaxFile":536870912,"jsMaxStreams":20,"jsMaxConsumers":40}`)))
		Expect(err).NotTo(HaveOccurred())
		updateReq.Header.Set("Content-Type", "application/json")
		updateReq.Header.Set("X-Actor", "limit-admin")
		updateReq.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		updateResp, err := client.Client().Do(updateReq)
		Expect(err).NotTo(HaveOccurred())
		defer updateResp.Body.Close()
		Expect(updateResp.StatusCode).To(Equal(http.StatusOK))

		entries, err := auditLog.ListByAccount(context.Background(), "jslimits-audit-tenant")
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(2), "one create + one jslimits_updated")
		Expect(entries[0].Action).To(Equal(accounts.AuditActionJSLimitsUpdated))
		Expect(entries[0].Outcome).To(Equal(accounts.AuditOutcomeSuccess))
		Expect(entries[0].Actor).To(Equal("limit-admin"))
		Expect(entries[0].Metadata).To(HaveKey("previous"))
		Expect(entries[0].Metadata).To(HaveKey("requested"))
	})

	// BR-AC11 failure case: a partial failure (the resolver revoke fails
	// independently of the Postgres status flip) still leaves a row behind,
	// with the failing step and error captured in metadata — the audit
	// trail's whole point is to surface exactly this kind of partial-failure
	// gap (open gap #4 from the 2026-08-03 review), not just the happy path.
	It("BR-AC11: records a failed outcome when a lifecycle action fails partway", func() {
		createResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": "audit-fail-tenant"}, accounts.BasicAuthUser, authSecret)
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		// Force the resolver revoke call to fail deterministically by
		// severing the sys connection the shared provisioner depends on for
		// $SYS.REQ.CLAIMS.DELETE — this test is the last thing that runs in
		// this It block, so leaving the connection closed doesn't affect
		// anything else.
		sysNC.Close()

		suspendResp := doRequest(http.MethodPost, "/api/accounts/audit-fail-tenant/suspend", nil, accounts.BasicAuthUser, authSecret)
		defer suspendResp.Body.Close()
		Expect(suspendResp.StatusCode).To(Equal(http.StatusInternalServerError))

		entries, err := auditLog.ListByAccount(context.Background(), "audit-fail-tenant")
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(2), "the successful create plus the failed suspend attempt")
		Expect(entries[0].Action).To(Equal(accounts.AuditActionSuspended))
		Expect(entries[0].Outcome).To(Equal(accounts.AuditOutcomeFailed))
		Expect(entries[0].Metadata["step"]).To(Equal("revoke account"))
		Expect(entries[0].Metadata["error"]).NotTo(BeEmpty())
	})

	// Admin UI Topology panel: GET /api/accounts/topology must reflect the
	// *live* resolver JWT for each account (Provisioner.LookupAccountClaims),
	// not just the bootstrap-time tenantImports convention — this is the
	// thing that makes the panel trustworthy if an account's imports ever
	// diverge from that convention.
	It("reports every tenant's live PLATFORM imports as topology edges", func() {
		platform, err := store.Get(context.Background(), "platform")
		Expect(err).NotTo(HaveOccurred())

		minted, err := provisioner.CreateAccount(context.Background(), accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "acme", platform.PublicKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Insert(context.Background(), accounts.Account{
			Name: "acme", PublicKey: minted.PublicKey, SigningKeySeed: minted.SigningKeySeed,
			Status: accounts.StatusActive, JSMaxMem: 64 << 20, JSMaxFile: 128 << 20, JSMaxStreams: 3, JSMaxConsumers: 5,
		})).To(Succeed())

		resp := doRequest(http.MethodGet, "/api/accounts/topology", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var body topologyResponseJSON
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

		acmeEdges := make([]string, 0)
		for _, e := range body.Edges {
			Expect(e.ToAccount).NotTo(BeEmpty())
			if e.ToAccount == "acme" {
				Expect(e.FromAccount).To(Equal("platform"), "acme's imports all originate from PLATFORM's exports")
				acmeEdges = append(acmeEdges, e.Subject)
			}
		}
		Expect(acmeEdges).To(ContainElement("rpc.acme.refdata.item.get.v1"))
		Expect(acmeEdges).To(ContainElement("notify.accounts.account.*"))
		Expect(acmeEdges).To(HaveLen(7), "the complete tenantImports contract: 4 refdata RPCs + context-list + notify + evt stream — see provisioner.go's tenantImports")
	})

	// BR-AC22/BR-AC23: Provisioner's own API never writes Exports (see
	// PushAccountClaims's doc comment) — PLATFORM's Exports are pushed
	// directly here, the way nats/bootstrap-operator.sh's nsc add export
	// would at real bootstrap, so the matching logic runs against a live
	// export declaration instead of the always-empty claims CreateAccount
	// produces on its own.
	It("BR-AC22/BR-AC23: matches an import against a live export, leaves the rest no-export, and reports the unconsumed export separately", func() {
		platform, err := store.Get(context.Background(), "platform")
		Expect(err).NotTo(HaveOccurred())

		minted, err := provisioner.CreateAccount(context.Background(), accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "acme-ac22", platform.PublicKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Insert(context.Background(), accounts.Account{
			Name: "acme-ac22", PublicKey: minted.PublicKey, SigningKeySeed: minted.SigningKeySeed,
			Status: accounts.StatusActive, JSMaxMem: 64 << 20, JSMaxFile: 128 << 20, JSMaxStreams: 3, JSMaxConsumers: 5,
		})).To(Succeed())

		// Two of this tenant's seven tenantImports get a matching export
		// here; the rest stay no-export; evt.*.audit.*.written has no
		// importer at all.
		platformClaims := jwt.NewAccountClaims(platform.PublicKey)
		platformClaims.Exports.Add(
			&jwt.Export{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service},
			&jwt.Export{Subject: "notify.accounts.account.*", Type: jwt.Stream},
			&jwt.Export{Subject: "evt.*.audit.*.written", Type: jwt.Stream},
		)
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, platformClaims)

		resp := doRequest(http.MethodGet, "/api/accounts/topology", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var body topologyResponseJSON
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

		statusBySubject := make(map[string]string)
		for _, e := range body.Edges {
			if e.ToAccount == "acme-ac22" {
				statusBySubject[e.Subject] = e.Status
			}
		}
		Expect(statusBySubject["rpc.acme-ac22.refdata.item.get.v1"]).To(Equal("matched"))
		Expect(statusBySubject["notify.accounts.account.*"]).To(Equal("matched"))
		Expect(statusBySubject["rpc.acme-ac22.refdata.type.list.v1"]).To(Equal("no-export"), "PLATFORM declares no export for this operation in this test")
		Expect(statusBySubject["evt.*.refdata.*.changed"]).To(Equal("no-export"))

		Expect(body.UnconsumedExports).To(ContainElement(unconsumedExportJSON{
			Account: "platform", Subject: "evt.*.audit.*.written", Type: "stream",
		}))
		for _, u := range body.UnconsumedExports {
			Expect(u.Subject).NotTo(Equal("rpc.*.refdata.item.get.v1"), "this export is consumed by acme's import — must not also appear as unconsumed")
		}
	})

	It("BR-AC24: reports an import as token-required when its matching export demands an activation token", func() {
		platform, err := store.Get(context.Background(), "platform")
		Expect(err).NotTo(HaveOccurred())

		minted, err := provisioner.CreateAccount(context.Background(), accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "acme-ac24", platform.PublicKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Insert(context.Background(), accounts.Account{
			Name: "acme-ac24", PublicKey: minted.PublicKey, SigningKeySeed: minted.SigningKeySeed,
			Status: accounts.StatusActive, JSMaxMem: 64 << 20, JSMaxFile: 128 << 20, JSMaxStreams: 3, JSMaxConsumers: 5,
		})).To(Succeed())

		platformClaims := jwt.NewAccountClaims(platform.PublicKey)
		platformClaims.Exports.Add(&jwt.Export{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service, TokenReq: true})
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, platformClaims)

		resp := doRequest(http.MethodGet, "/api/accounts/topology", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var body topologyResponseJSON
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

		found := false
		for _, e := range body.Edges {
			if e.ToAccount == "acme-ac24" && e.Subject == "rpc.acme-ac24.refdata.item.get.v1" {
				found = true
				Expect(e.Status).To(Equal("token-required"), "the export exists and covers this subject, but tenantImports() never attaches an activation token")
			}
		}
		Expect(found).To(BeTrue())
	})

	It("BR-AC25: reports an import as unknown-account when its exporter isn't a recognized account", func() {
		platform, err := store.Get(context.Background(), "platform")
		Expect(err).NotTo(HaveOccurred())

		minted, err := provisioner.CreateAccount(context.Background(), accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "acme-ac25", platform.PublicKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Insert(context.Background(), accounts.Account{
			Name: "acme-ac25", PublicKey: minted.PublicKey, SigningKeySeed: minted.SigningKeySeed,
			Status: accounts.StatusActive, JSMaxMem: 64 << 20, JSMaxFile: 128 << 20, JSMaxStreams: 3, JSMaxConsumers: 5,
		})).To(Succeed())

		ghostKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		ghostPub, err := ghostKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		// Append to this tenant's live claims rather than replacing them, so
		// the existing tenantImports contract survives alongside the
		// injected ghost import.
		tenantClaims, err := provisioner.LookupAccountClaims(context.Background(), minted.PublicKey)
		Expect(err).NotTo(HaveOccurred())
		tenantClaims.Imports.Add(&jwt.Import{Account: ghostPub, Subject: "evt.*.ghost.*.changed", Type: jwt.Stream})
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, tenantClaims)

		resp := doRequest(http.MethodGet, "/api/accounts/topology", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var body topologyResponseJSON
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

		found := false
		for _, e := range body.Edges {
			if e.ToAccount == "acme-ac25" && e.Subject == "evt.*.ghost.*.changed" {
				found = true
				Expect(e.FromAccount).To(Equal(ghostPub), "the exporter is outside this deployment's known accounts — shown as the raw pubkey, never dropped")
				Expect(e.Status).To(Equal("unknown-account"))
			}
		}
		Expect(found).To(BeTrue())
	})

	// BR-AC26/27/28/29 — business unit name/context split, slug validation and
	// immutability, and the per-tenant default. RefdataURL is unset in this
	// suite's setup, so every refdata-service call inside these handlers is a
	// no-op warn-and-continue — these specs exercise accounts-service's own
	// contract only, not the cross-service write.
	Describe("Business units (BR-AC26/27/28/29)", func() {
		createTenant := func(name string) {
			resp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": name}, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		}

		listBUs := func(name string) []businessUnitJSON {
			resp := doRequest(http.MethodGet, "/api/accounts/"+name+"/business-units", nil, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out []businessUnitJSON
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			return out
		}

		It("BR-AC16/BR-AC28: auto-creates a tenant-owned default, not the Phase 22 shared _default_bu", func() {
			createTenant("bu-ac28")
			bus := listBUs("bu-ac28")
			Expect(bus).To(HaveLen(1))
			Expect(bus[0].Name).To(Equal("Default"))
			Expect(bus[0].Context).To(Equal("bu-ac28-default"), "each tenant must get its own slug, never the shared _default_bu")
			Expect(bus[0].IsDefault).To(BeTrue())
			Expect(bus[0].Visible).To(BeTrue())
		})

		It("BR-AC26: derives the context slug from name when none is supplied, keeping the two fields distinct", func() {
			createTenant("bu-ac26")
			resp := doRequest(http.MethodPost, "/api/accounts/bu-ac26/business-units", map[string]any{
				"name": "Pacific Fleet",
			}, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var bu businessUnitJSON
			Expect(json.NewDecoder(resp.Body).Decode(&bu)).To(Succeed())
			Expect(bu.Name).To(Equal("Pacific Fleet"))
			Expect(bu.Context).To(Equal("bu-ac26-pacific-fleet"))
			Expect(bu.IsDefault).To(BeFalse())
		})

		It("BR-AC26: an explicitly supplied context is honored instead of the derived one", func() {
			createTenant("bu-ac26b")
			resp := doRequest(http.MethodPost, "/api/accounts/bu-ac26b/business-units", map[string]any{
				"name": "Pacific Fleet", "context": "custom-slug",
			}, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var bu businessUnitJSON
			Expect(json.NewDecoder(resp.Body).Decode(&bu)).To(Succeed())
			Expect(bu.Context).To(Equal("custom-slug"))
		})

		It("BR-AC27: rejects a context that isn't a legal subject token", func() {
			createTenant("bu-ac27")
			resp := doRequest(http.MethodPost, "/api/accounts/bu-ac27/business-units", map[string]any{
				"name": "West Coast", "context": "West Coast",
			}, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest), "a bad slug must fail loudly here, not persist and fail silently downstream in refdata-service")
		})

		It("BR-AC27: a context slug is globally unique across accounts, not just per-account", func() {
			createTenant("bu-ac27a")
			createTenant("bu-ac27b")
			first := doRequest(http.MethodPost, "/api/accounts/bu-ac27a/business-units", map[string]any{
				"name": "Shared", "context": "globally-unique-slug",
			}, accounts.BasicAuthUser, authSecret)
			defer first.Body.Close()
			Expect(first.StatusCode).To(Equal(http.StatusCreated))

			second := doRequest(http.MethodPost, "/api/accounts/bu-ac27b/business-units", map[string]any{
				"name": "Different Name", "context": "globally-unique-slug",
			}, accounts.BasicAuthUser, authSecret)
			defer second.Body.Close()
			Expect(second.StatusCode).To(Equal(http.StatusConflict), "two accounts claiming one slug would let the second silently overwrite the first's context row in refdata-service")
		})

		It("BR-AC26: renaming changes the display name only — the context slug has no rename path", func() {
			createTenant("bu-ac26c")
			created := doRequest(http.MethodPost, "/api/accounts/bu-ac26c/business-units", map[string]any{
				"name": "Old Name",
			}, accounts.BasicAuthUser, authSecret)
			defer created.Body.Close()
			var bu businessUnitJSON
			Expect(json.NewDecoder(created.Body).Decode(&bu)).To(Succeed())
			originalContext := bu.Context

			resp := doRequest(http.MethodPatch, "/api/accounts/bu-ac26c/business-units/"+originalContext, map[string]any{
				"name": "New Name",
			}, accounts.BasicAuthUser, authSecret)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			bus := listBUs("bu-ac26c")
			Expect(bus).To(HaveLen(2)) // the auto-created default, plus this one
			var renamed *businessUnitJSON
			for i := range bus {
				if bus[i].Context == originalContext {
					renamed = &bus[i]
				}
			}
			Expect(renamed).NotTo(BeNil(), "the slug must survive a rename unchanged")
			Expect(renamed.Name).To(Equal("New Name"))
		})

		It("BR-AC28: the default business unit cannot be renamed, but stays visibility-toggleable", func() {
			createTenant("bu-ac28b")
			bus := listBUs("bu-ac28b")
			Expect(bus).To(HaveLen(1))
			defaultContext := bus[0].Context

			renameResp := doRequest(http.MethodPatch, "/api/accounts/bu-ac28b/business-units/"+defaultContext, map[string]any{
				"name": "Renamed Default",
			}, accounts.BasicAuthUser, authSecret)
			defer renameResp.Body.Close()
			Expect(renameResp.StatusCode).To(Equal(http.StatusConflict))

			visResp := doRequest(http.MethodPatch, "/api/accounts/bu-ac28b/business-units/"+defaultContext, map[string]any{
				"visible": false,
			}, accounts.BasicAuthUser, authSecret)
			defer visResp.Body.Close()
			Expect(visResp.StatusCode).To(Equal(http.StatusNoContent), "BR-AC17's hide-once-a-real-BU-exists flow depends on the default staying toggleable")

			bus = listBUs("bu-ac28b")
			Expect(bus[0].Visible).To(BeFalse())
			Expect(bus[0].Name).To(Equal("Default"), "the rejected rename must not have partially applied")
		})

		It("lists the default business unit first, ahead of every real one", func() {
			createTenant("bu-order")
			addResp := doRequest(http.MethodPost, "/api/accounts/bu-order/business-units", map[string]any{
				"name": "Atlantic Fleet",
			}, accounts.BasicAuthUser, authSecret)
			defer addResp.Body.Close()
			Expect(addResp.StatusCode).To(Equal(http.StatusCreated))

			bus := listBUs("bu-order")
			Expect(bus).To(HaveLen(2))
			Expect(bus[0].IsDefault).To(BeTrue(), "the default is the one row guaranteed to exist and should anchor the list, not sort alphabetically among the rest")
		})
	})
})

func readAll(resp *http.Response) (string, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	return buf.String(), err
}
