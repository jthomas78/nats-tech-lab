// Package rest exposes this service's remaining infra surface over HTTP.
//
// Phase 26h moved every business operation (Organization registration/
// lifecycle, ComplianceDocument review, FleetAsset registration) onto
// api.*.organizations.* (internal/browserrpc), served as a dual transport
// alongside the equivalent REST routes. Phase 33.5 retired the REST half:
// this service had no BasicAuth-gated admin/operator routes distinct from
// its business CRUD, so all fourteen /api/organizationss/* routes were
// deleted outright rather than reclassified. REST served infra health only
// until Phase 38c-ii, which added two byte-transfer routes for compliance
// document upload/download — see document_files.go for why bytes cannot ride
// the NATS command surface, and why those two routes do not reopen the
// business surface Phase 33.5 closed.
package rest

import (
	"log/slog"
	"net/http"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
)

// Mount registers every route this service serves over HTTP. It returns the
// exact "METHOD /pattern" list it registered so BR-TP17's allowlist test can
// assert the mux surface never grows a business route.
//
// files may be nil, which registers infra health alone — the shape the
// service had between Phase 33.5 and 38c-ii, kept reachable so a deployment
// without an object store still serves /healthz rather than failing to boot.
// The allowlist test pins the full surface, so a nil here cannot quietly
// shrink what the test believes it is checking.
func Mount(mux *http.ServeMux, files *commands.DocumentFileHandler, log *slog.Logger) []string {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	routes := []string{"GET /healthz"}
	if files != nil {
		routes = append(routes, MountDocumentFiles(mux, files, log)...)
	}
	return routes
}
