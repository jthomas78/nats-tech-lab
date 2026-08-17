package rest

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
)

// TestMountRoutesMatchAdminAllowlist proves BR-TP17/BR-040: this service's
// Mount registers exactly the admin/infra allowlist and nothing else — an
// unexpectedly added or removed route fails this test, not just review.
func TestMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	g.Expect(Mount(http.NewServeMux())).To(ConsistOf("GET /healthz"))
}
