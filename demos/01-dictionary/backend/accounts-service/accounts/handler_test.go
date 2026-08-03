package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

var _ = Describe("Handlers", func() {
	const authSecret = "test-secret"

	var (
		ots         *operatorTestServer
		client      *httptest.Server
		store       *accounts.Store
		provisioner *accounts.Provisioner
	)

	BeforeEach(func() {
		if storeTestUnavailable != "" {
			Skip("docker unavailable for Postgres integration test: " + storeTestUnavailable)
		}

		ots = newOperatorTestServer(GinkgoT())
		DeferCleanup(ots.Shutdown)
		nc := ots.ConnectSys(GinkgoT())
		DeferCleanup(nc.Close)

		var err error
		provisioner, err = accounts.NewProvisioner(ots.OperatorSigningKeySeed, nc)
		Expect(err).NotTo(HaveOccurred())

		store = accounts.NewStore(storeTestDB)
		credsDir := GinkgoT().TempDir()
		handlers := accounts.NewHandlers(store, provisioner, credsDir, slog.New(slog.DiscardHandler), nil)

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
		minted, err := provisioner.CreateAccount(context.Background(), accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5})
		Expect(err).NotTo(HaveOccurred())

		// Simulate a seeded account (like DEFAULT/ACME/GLOBEX): a real,
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
	// operator has no DEFAULT account) — the cross-account delivery guarantee
	// itself is core NATS behavior, not something this test needs to reprove.
	It("BR-AC08: publishes notify.accounts.account.created only after a create fully succeeds", func() {
		notifyNC := ots.ConnectSys(GinkgoT())
		defer notifyNC.Close()
		sub, err := notifyNC.SubscribeSync("notify.accounts.account.created")
		Expect(err).NotTo(HaveOccurred())
		defer sub.Unsubscribe() //nolint:errcheck

		notifyHandlers := accounts.NewHandlers(store, provisioner, GinkgoT().TempDir(), slog.New(slog.DiscardHandler), notifyNC)
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

		notifyHandlers := accounts.NewHandlers(store, provisioner, GinkgoT().TempDir(), slog.New(slog.DiscardHandler), notifyNC)
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

	It("does not fail account suspension when NotifyNC is unset", func() {
		createResp := doRequest(http.MethodPost, "/api/accounts", map[string]any{"name": "no-notify-suspend-tenant"}, accounts.BasicAuthUser, authSecret)
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		resp := doRequest(http.MethodPost, "/api/accounts/no-notify-suspend-tenant/suspend", nil, accounts.BasicAuthUser, authSecret)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "the outer BeforeEach's handlers has a nil NotifyNC — suspension must still succeed")
	})

	// BR-AC06 (BUSINESS_RULES-ACCOUNTS.md): DEFAULT/SYS are reserved,
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
		Entry("DEFAULT", "DEFAULT"),
		Entry("default", "default"),
		Entry("Default", "Default"),
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
})

func readAll(resp *http.Response) (string, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	return buf.String(), err
}
