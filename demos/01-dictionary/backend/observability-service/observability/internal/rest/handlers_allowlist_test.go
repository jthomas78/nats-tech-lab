package rest

// BR-040 — proves Mount registers exactly the admin/infra diagnostics
// allowlist documented in BUSINESS_RULES-SHIPPING.md's BR-040 entry for
// observability-service, and nothing else. Mount never invokes a handler
// (it only registers routes on the mux), so a zero-value Deps is enough to
// exercise it — no live NATS connection needed.

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	h := New(Deps{})
	mux := http.NewServeMux()

	routes := h.Mount(mux)

	g.Expect(routes).To(ConsistOf(
		"GET /healthz",
		"GET /api/nats/connections",
		"GET /api/nats/account-activity",
		"GET /api/nats/log",
		"GET /api/kv/buckets",
		"GET /api/kv/buckets/{account}/{bucket}/entries",
		"GET /api/jetstream/streams",
		"GET /api/jetstream/replay",
		"GET /api/nats/services",
	))
}
