package rest

// BR-040 (Phase 34): every service's registered REST route set is asserted
// to exactly match a hardcoded admin/infra/bootstrap allowlist, so a
// business route added later to Mount fails this test, not just a code
// review. The allowlist below is data, not prose, and must be updated
// deliberately if this service's route set ever legitimately changes.

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	h := newTraceMiddlewareTestHandlers(t, nil)
	mux := http.NewServeMux()

	routes := h.Mount(mux)

	g.Expect(routes).To(ConsistOf(
		"GET /api/admin/ports/{context}",
		"GET /api/tenant",
		"POST /api/tenant/switch",
		"GET /healthz",
		"/swagger/",
	))
}
