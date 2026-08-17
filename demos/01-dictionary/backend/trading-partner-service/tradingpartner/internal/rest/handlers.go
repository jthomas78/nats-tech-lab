// Package rest exposes this service's remaining infra surface over HTTP.
//
// Phase 26h moved every business operation (TradingPartner registration/
// lifecycle, ComplianceDocument review, FleetAsset registration) onto
// api.*.trading-partner.* (internal/browserrpc), served as a dual transport
// alongside the equivalent REST routes. Phase 33.5 retired the REST half:
// this service had no BasicAuth-gated admin/operator routes distinct from
// its business CRUD, so all fourteen /api/trading-partners/* routes were
// deleted outright rather than reclassified. REST now serves infra health
// only.
package rest

import "net/http"

// Mount registers the routes this service still serves over REST.
func Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
