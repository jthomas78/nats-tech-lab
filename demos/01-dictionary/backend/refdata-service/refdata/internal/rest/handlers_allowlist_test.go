package rest

import (
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
)

// TestMountRoutesMatchAdminAllowlist is BR-D44's mirror of BR-040
// (BUSINESS_RULES-SHIPPING.md): Mount's returned route list must exactly
// match this hardcoded admin/infra allowlist — not a subset check, so the
// test catches both an unexpectedly *added* route (a future business route
// slipping onto this mux) and an unexpectedly *removed* one (signalling the
// allowlist itself has gone stale and needs a deliberate edit).
func TestMountRoutesMatchAdminAllowlist(t *testing.T) {
	g := NewWithT(t)

	h := NewHandlers(Deps{})
	mux := http.NewServeMux()
	routes := h.Mount(mux)

	g.Expect(routes).To(ConsistOf(
		"POST /api/refdata/admin/contexts",
		"GET /api/refdata/admin/contexts",
		"GET /api/refdata/admin/contexts/{context}/detail",
		"PATCH /api/refdata/admin/contexts/{context}/visible",
		"POST /api/refdata/admin/corpus/{context}/draft",
		"GET /api/refdata/admin/corpus/{context}/draft",
		"PUT /api/refdata/admin/corpus/{context}/draft/items",
		"PUT /api/refdata/admin/corpus/{context}/draft/localizations",
		"POST /api/refdata/admin/corpus/{context}/publish",
		"POST /api/refdata/admin/corpus/{context}/rollback/{version}",
		"GET /api/refdata/admin/corpus/{context}/versions",
		"GET /api/refdata/admin/corpus/{context}/versions/{version}",
		"GET /api/refdata/admin/corpus/{context}/diff/{from}/{to}",
		"POST /api/refdata/admin/types",
		"POST /api/refdata/admin/locales",
		"POST /api/refdata/admin/items",
		// BR-D46-BR-D48 (Phase 38d-ii): region registration is one admin
		// route, not a business route — it stays off the browser api.*
		// surface per BR-D41.
		"POST /api/refdata/admin/regions",
		"POST /api/refdata/admin/items/{type}/{context}/{code}/deprecate",
		"POST /api/refdata/admin/items/{type}/{context}/{code}/reactivate",
		"PATCH /api/refdata/admin/items/{type}/{context}/{code}/attrs",
		"DELETE /api/refdata/admin/items/{type}/{context}/{code}",
		"POST /api/refdata/admin/references",
		"POST /api/refdata/admin/localizations",
		"POST /api/refdata/admin/{type}/{code}/translate",
		"/swagger/",
	))
}
