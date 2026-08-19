// Package rest is observability-service's HTTP surface — the diagnostic
// endpoints lifted from shipping-service (Main-POC-Plan.md's Phase 30,
// dictionary/internal/rest/{nats_ops,nats_log,streams,kv,replay}.go) plus
// the health check Phase 30c stood up first.
package rest

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
)

// Deps is every dependency a handler needs.
type Deps struct {
	NC  *nats.Conn
	Log *slog.Logger
	// NatsMonitorURL is the NATS server's HTTP monitoring port (distinct
	// from NC's client port) — Connections and AccountActivity proxy
	// /connz, /varz, /accstatz on it. Empty disables both (they 502).
	NatsMonitorURL string
	// NatsLogPath is NATS's own log_file, tailed by the Log panel. Empty
	// disables it (503) — same "not configured outside Docker" shape as
	// shipping-service's original NatsLogPath.
	NatsLogPath string
	// Accounts resolves an account public key to its friendly tenant name
	// for the Connections/AccountActivity panels — see accounts_client.go's
	// doc comment for why this replaces shipping-service's original
	// LocalAddr-matching trick (tenantLabelsByAccount), which depended on
	// holding a live per-tenant connection, exactly the fan-out Phase 30
	// exists to eliminate. Nil-safe: Labels returns nil on a nil receiver.
	Accounts *AccountsClient
	// History is the Overview tab's trend-chart data source (BR-043) — a
	// 60-minute ring buffer of /accstatz samples, queried over a fixed
	// 5m/30m/1h window. Nil-safe: Query degrades to an empty accounts list
	// on a nil receiver, same convention Accounts uses.
	History *AccstatzHistory
}

// Handlers holds Deps and registers routes via Mount.
type Handlers struct {
	deps Deps
}

// New builds Handlers from deps.
func New(deps Deps) *Handlers {
	return &Handlers{deps: deps}
}

// Mount registers every route this service currently serves, returning the
// exact list of registered patterns in registration order. BR-040's
// allowlist test asserts this list against a hardcoded admin/infra allowlist
// so a future business route added here can't slip past the REST boundary
// unnoticed.
func (h *Handlers) Mount(mux *http.ServeMux) []string {
	var routes []string
	handle := func(pattern string, fn http.HandlerFunc) {
		routes = append(routes, pattern)
		mux.HandleFunc(pattern, fn)
	}

	handle("GET /healthz", h.healthz)
	handle("GET /api/nats/connections", h.listNatsConnections)
	handle("GET /api/nats/account-activity", h.listNatsAccountActivity)
	handle("GET /api/nats/account-activity/history", h.accountActivityHistory)
	handle("GET /api/nats/log", h.tailNatsLog)
	handle("GET /api/kv/buckets", h.listKVBuckets)
	handle("GET /api/kv/buckets/{account}/{bucket}/entries", h.kvBucketEntriesOnce)
	handle("GET /api/jetstream/streams", h.listStreams)
	handle("GET /api/jetstream/replay", h.jetstreamReplayOnce)
	handle("GET /api/nats/services", h.listNatsServices)

	return routes
}

// errorResponse is the JSON shape writeError produces — mirrors
// shipping-service's original.
type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// healthz reports the NATS connection's own status — the one dependency
// this service has at this stage. Returns 200 while connected or
// reconnecting (the client retries transparently), 503 only once the
// connection is fully closed.
func (h *Handlers) healthz(w http.ResponseWriter, r *http.Request) {
	if h.deps.NC == nil || h.deps.NC.IsClosed() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
