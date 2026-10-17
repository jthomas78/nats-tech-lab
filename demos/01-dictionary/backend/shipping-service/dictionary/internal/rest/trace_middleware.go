package rest

import (
	"net/http"
)

// httpTraceMiddleware gives every REST request one OTel SERVER span
// (BR-073), so an outbound rpc.* call a handler makes — for example
// internal/refdataconsumer's — is a child of it rather than a new root. That
// is the gap Phase 28m closed with a legacy-tracer span; Phase 54d moved it to
// otelnats.HTTPMiddlewareFor.
//
// The receiving account is the active tenant, read from h.deps() on every
// request, so it follows SwitchTenant like every other per-request field on
// Deps. Deps.Tracing is read once, when Mount wraps the route. With no
// Provider the route is served untraced: tracing must never block the
// business path it describes.
func (h *Handlers) httpTraceMiddleware(next http.HandlerFunc) http.HandlerFunc {
	tracing := h.deps().Tracing
	if tracing == nil {
		return next
	}
	traced := tracing.HTTPMiddlewareFor(func(*http.Request) string { return h.deps().Tenant }, next)
	return traced.ServeHTTP
}
