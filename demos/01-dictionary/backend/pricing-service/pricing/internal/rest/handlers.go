// Package rest exposes pricing-service's infra-only HTTP surface.
//
// Phase 33.4 deleted every /api/pricing/* business route (FeeScale,
// RateSheet, FixedRate, diesel price index/overlay — all 34 routes) once
// the api.* frontend-to-service adapter (internal/browserrpc) reached full
// parity with them. Business reads and writes are reachable only over
// api.*/rpc.* now; REST reduces to /healthz. See
// BUSINESS_RULES-PRICING.md's BR-P26 for the transport-contract rule.
package rest

import "net/http"

// Mount wires the infra-only routes onto mux and returns the exact list of
// "METHOD /pattern" routes it registered, so a test can assert this stays
// in sync with the admin/infra allowlist (BUSINESS_RULES-PRICING.md BR-P27).
func Mount(mux *http.ServeMux) []string {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return []string{"GET /healthz"}
}
