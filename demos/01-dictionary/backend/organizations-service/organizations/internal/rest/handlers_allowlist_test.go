package rest

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
)

// TestMountRoutesMatchAdminAllowlist proves BR-TP17/BR-040: this service's
// Mount registers exactly the admin/infra allowlist and nothing else — an
// unexpectedly added or removed route fails this test, not just review.
//
// Phase 38c-ii widened the allowlist by exactly two byte-transfer routes
// (BR-TP40). They are listed literally here rather than spread via
// rest.DocumentFileRoutes, so that adding a third route to that slice still
// fails this test — deferring to the same constant the production code uses
// would let the allowlist grow silently, which is the one thing this test
// exists to prevent.
func TestMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	files := &commands.DocumentFileHandler{}

	g.Expect(Mount(http.NewServeMux(), files, nil)).To(ConsistOf(
		"GET /healthz",
		"POST /files/documents",
		"GET /files/documents",
	))
}

// TestMountWithoutFileHandlerServesHealthOnly pins the nil-files shape: no
// object store wired means no byte routes at all, not routes that fail at
// request time.
func TestMountWithoutFileHandlerServesHealthOnly(t *testing.T) {
	g := NewWithT(t)

	g.Expect(Mount(http.NewServeMux(), nil, nil)).To(ConsistOf("GET /healthz"))
}
