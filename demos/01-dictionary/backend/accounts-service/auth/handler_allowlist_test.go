package auth_test

// BR-040/BR-AC33: this service's Handlers.Mount registers a fixed,
// deliberately ungated set of /api/auth/* routes onto one mux — a separate
// package and a separate sub-allowlist from accounts.Handlers.Mount, since
// the two live in different packages and mount independently onto the same
// mux (see cmd/main.go). Mount returns the exact "METHOD /pattern" strings
// it registers so this test can assert the returned list ConsistOf a
// hardcoded allowlist — an exact match, not a subset check, so both an
// unexpectedly added route and an unexpectedly removed one fail the test.
// Deliberately a bare *testing.T + gomega test, not a Ginkgo spec: Mount
// only registers routes and never invokes a handler, so no Postgres fixture
// (suite_test.go's BeforeSuite/testDB) is needed — constructing a
// zero-value *auth.Handlers is enough to exercise it.

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
)

func TestAuthMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	h := &auth.Handlers{}
	mux := http.NewServeMux()
	routes := h.Mount(mux)

	g.Expect(routes).To(ConsistOf(
		"GET /api/auth/connectInfo",
		"GET /api/auth/adminConnectInfo",
		"GET /api/auth/refdataAdminConnectInfo",
		"GET /api/auth/shellConnectInfo",
		"GET /api/auth/tenants",
		"POST /api/auth/login",
	))
}
