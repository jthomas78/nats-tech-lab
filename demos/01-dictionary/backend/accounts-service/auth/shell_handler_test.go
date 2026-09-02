package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/nats-io/jwt/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
)

var _ = Describe("BR-AS27 — shell credential HTTP bootstrap", func() {
	var store *accounts.Store
	var mux *http.ServeMux
	BeforeEach(func() {
		if testUnavailable != "" {
			Skip(testUnavailable)
		}
		_, err := testDB.ExecContext(context.Background(), `DELETE FROM accounts.accounts WHERE name = 'platform'`)
		Expect(err).NotTo(HaveOccurred())
		store = accounts.NewStore(testDB)
		mux = http.NewServeMux()
		auth.NewHandlers(store, "/nats", discardLogger()).Mount(mux)
	})
	seed := func(key string) {
		Expect(store.Insert(context.Background(), accounts.Account{Name: "platform", PublicKey: "APLATFORM", SigningKeySeed: key, Status: "active"})).To(Succeed())
	}
	read := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/shellConnectInfo", nil))
		return rec
	}
	It("mints the shell profile with the configured TTL and no operator subjects", func() {
		seed(signingSeedFor())
		Expect(store.SetTokenTTLConfig(context.Background(), accounts.TokenTTLConfig{ValueMinutes: 25, MinMinutes: 20, MaxMinutes: 30})).To(Succeed())
		DeferCleanup(func() {
			Expect(store.SetTokenTTLConfig(context.Background(), accounts.DefaultTokenTTLConfig())).To(Succeed())
		})
		rec := read()
		Expect(rec.Code).To(Equal(http.StatusOK))
		var info auth.ConnectInfo
		Expect(json.Unmarshal(rec.Body.Bytes(), &info)).To(Succeed())
		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.WSUrl).To(Equal("/nats"))
		Expect(info.Tenant).To(Equal("platform"))
		Expect(claims.Name).To(Equal("lab-shell"))
		Expect(claims.Permissions.Pub.Allow).To(ConsistOf("api._platform.mfe-registry.frontend-plugins.read.v1", "api._platform.mfe-registry.frontend-plugins.health.v1", "_INBOX.>"))
		Expect(time.Until(time.Unix(claims.Expires, 0))).To(BeNumerically(">", 24*time.Minute))
		Expect(time.Until(time.Unix(claims.Expires, 0))).To(BeNumerically("<", 26*time.Minute))
	})
	It("reports an unseeded PLATFORM account", func() { Expect(read().Code).To(Equal(http.StatusNotFound)) })
	It("reports a missing signing key", func() { seed(""); Expect(read().Code).To(Equal(http.StatusConflict)) })
	It("does not expose a signing failure's secret", func() {
		seed("secret-invalid-seed")
		rec := read()
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(rec.Body.String()).NotTo(ContainSubstring("secret-invalid-seed"))
	})
})
