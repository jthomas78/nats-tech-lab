package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"

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
		handlers := accounts.NewHandlers(store, provisioner, credsDir, slog.New(slog.DiscardHandler))

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
})

func readAll(resp *http.Response) (string, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	return buf.String(), err
}
