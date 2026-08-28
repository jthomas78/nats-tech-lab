package accounts_test

// BR-040/BR-AC33: this service's Handlers.Mount registers a fixed set of
// BasicAuth-gated /api/accounts* routes onto one mux. Mount returns the
// exact "METHOD /pattern" strings it registers so this test can assert the
// returned list ConsistOf a hardcoded allowlist — an exact match, not a
// subset check, so both an unexpectedly added route and an unexpectedly
// removed one fail the test. Deliberately a bare *testing.T + gomega test,
// not a Ginkgo spec: Mount only registers routes and never invokes a
// handler, so no database/Store/Provisioner fixture (suite_test.go's
// BeforeSuite) is needed — constructing a zero-value *accounts.Handlers is
// enough to exercise it.

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

func TestAccountsMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	h := &accounts.Handlers{}
	mux := http.NewServeMux()
	routes := h.Mount(mux, "test-secret")

	g.Expect(routes).To(ConsistOf(
		"POST /api/accounts",
		"GET /api/accounts",
		"GET /api/accounts/usage",
		"GET /api/accounts/topology",
		"GET /api/accounts/{name}",
		"POST /api/accounts/{name}/suspend",
		"POST /api/accounts/{name}/reactivate",
		"POST /api/accounts/{name}/jslimits",
		"GET /api/accounts/system-config",
		"PUT /api/accounts/system-config",
		"GET /api/accounts/frontend-plugins",
		"GET /api/accounts/{name}/business-units",
		"POST /api/accounts/{name}/business-units",
		"PATCH /api/accounts/{name}/business-units/{buContext}",
	))
}
