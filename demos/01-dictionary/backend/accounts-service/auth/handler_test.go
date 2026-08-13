package auth_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(GinkgoWriter, nil))
}

// signingSeedFor generates a fresh, valid account signing key seed — tests
// need a real one so MintBrowserToken (called by connectInfo) can encode a
// JWT with it, unlike a plain-string fixture that's never actually signed
// with.
func signingSeedFor() string {
	kp, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	seed, err := kp.Seed()
	Expect(err).NotTo(HaveOccurred())
	return string(seed)
}

var _ = Describe("Handlers", func() {
	var handlers *auth.Handlers
	var mux *http.ServeMux
	var store *accounts.Store

	BeforeEach(func() {
		if testUnavailable != "" {
			Skip("docker unavailable for Postgres integration test: " + testUnavailable)
		}
		store = accounts.NewStore(testDB)
		handlers = auth.NewHandlers(store, "ws://localhost:9222", discardLogger())
		mux = http.NewServeMux()
		handlers.Mount(mux)
	})

	seedAccount := func(name, publicKey, signingKeySeed, status string) error {
		return store.Insert(context.Background(), accounts.Account{
			Name: name, PublicKey: publicKey, SigningKeySeed: signingKeySeed, Status: status,
		})
	}

	Describe("GET /api/auth/connectInfo", func() {
		It("mints connect info for an active tenant with a signing key on record", func() {
			name := uniqueName("acme")
			Expect(seedAccount(name, "A"+name, signingSeedFor(), "active")).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/api/auth/connectInfo?tenant="+name, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			var info auth.ConnectInfo
			Expect(json.Unmarshal(rec.Body.Bytes(), &info)).To(Succeed())
			Expect(info.Tenant).To(Equal(name))
			Expect(info.WSUrl).To(Equal("ws://localhost:9222"))
			Expect(info.JWT).NotTo(BeEmpty())
			Expect(info.NKeySeed).NotTo(BeEmpty())
		})

		It("BR-AC20: stamps the minted JWT's expiry from the configured TTL", func() {
			// Reset to default first, then set a known non-default value so
			// this spec is independent of order and proves the configured
			// value (not a constant) flows into the mint.
			Expect(store.SetTokenTTLConfig(context.Background(), accounts.DefaultTokenTTLConfig())).To(Succeed())
			Expect(store.SetTokenTTLConfig(context.Background(), accounts.TokenTTLConfig{
				ValueMinutes: 25, MinMinutes: 20, MaxMinutes: 30,
			})).To(Succeed())

			name := uniqueName("acme")
			Expect(seedAccount(name, "A"+name, signingSeedFor(), "active")).To(Succeed())

			before := time.Now()
			req := httptest.NewRequest(http.MethodGet, "/api/auth/connectInfo?tenant="+name, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusOK))

			var info auth.ConnectInfo
			Expect(json.Unmarshal(rec.Body.Bytes(), &info)).To(Succeed())
			claims, err := jwt.DecodeUserClaims(info.JWT)
			Expect(err).NotTo(HaveOccurred())
			expiry := time.Unix(claims.Expires, 0)
			Expect(expiry).To(BeTemporally(">", before.Add(24*time.Minute)))
			Expect(expiry).To(BeTemporally("<", before.Add(26*time.Minute)))

			// Leave the singleton row back at the default for other specs.
			Expect(store.SetTokenTTLConfig(context.Background(), accounts.DefaultTokenTTLConfig())).To(Succeed())
		})

		It("returns 400 when tenant is missing", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/connectInfo", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 404 for an unknown tenant", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/connectInfo?tenant="+uniqueName("ghost"), nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 403 for a suspended tenant", func() {
			name := uniqueName("suspended")
			Expect(seedAccount(name, "A"+name, signingSeedFor(), "suspended")).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/api/auth/connectInfo?tenant="+name, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusForbidden))
		})

		It("returns 409 for an active tenant with no signing key on record", func() {
			name := uniqueName("nokey")
			Expect(seedAccount(name, "A"+name, "", "active")).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/api/auth/connectInfo?tenant="+name, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusConflict))
		})
	})

	Describe("GET /api/auth/adminConnectInfo", func() {
		// adminConnectInfo always looks up the fixed "platform" row (it is
		// not parameterized like connectInfo's tenant), so each test needs a
		// clean slate rather than uniqueName's per-tenant isolation.
		BeforeEach(func() {
			if testUnavailable != "" {
				return
			}
			_, err := testDB.ExecContext(context.Background(), `DELETE FROM accounts.accounts WHERE name = 'platform'`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("mints connect info for the seeded platform account with a signing key on record", func() {
			Expect(seedAccount("platform", "APLATFORM", signingSeedFor(), "active")).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/api/auth/adminConnectInfo", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			var info auth.ConnectInfo
			Expect(json.Unmarshal(rec.Body.Bytes(), &info)).To(Succeed())
			Expect(info.Tenant).To(Equal("platform"))
			Expect(info.WSUrl).To(Equal("ws://localhost:9222"))
			Expect(info.JWT).NotTo(BeEmpty())
			Expect(info.NKeySeed).NotTo(BeEmpty())
		})

		It("returns 404 when the platform account has not been seeded", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/adminConnectInfo", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 409 when the platform account has no signing key on record", func() {
			Expect(seedAccount("platform", "APLATFORM", "", "active")).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/api/auth/adminConnectInfo", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusConflict))
		})
	})

	Describe("GET /api/auth/tenants", func() {
		It("lists active tenant names", func() {
			name := uniqueName("visible")
			Expect(seedAccount(name, "A"+name, signingSeedFor(), "active")).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/api/auth/tenants", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			var body struct {
				Tenants []string `json:"tenants"`
			}
			Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
			Expect(body.Tenants).To(ContainElement(name))
		})
	})

	Describe("POST /api/auth/login", func() {
		It("returns 501 Not Implemented", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusNotImplemented))
		})
	})
})
