package rest

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
)

// TestMountRoutesMatchAdminAllowlist asserts Mount's returned route list is
// exactly the admin/infra allowlist — no more, no less. BR-P27
// (BUSINESS_RULES-PRICING.md) mirrors BR-040 (BUSINESS_RULES-SHIPPING.md):
// a business route added later to Mount without updating this allowlist
// fails this test, not just code review.
func TestMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	g.Expect(Mount(http.NewServeMux())).To(ConsistOf("GET /healthz"))
}
