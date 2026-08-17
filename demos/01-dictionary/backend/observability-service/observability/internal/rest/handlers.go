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
}

// Handlers holds Deps and registers routes via Mount.
type Handlers struct {
	deps Deps
}

// New builds Handlers from deps.
func New(deps Deps) *Handlers {
	return &Handlers{deps: deps}
}

// Mount registers every route this service currently serves.
func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /api/nats/connections", h.listNatsConnections)
	mux.HandleFunc("GET /api/nats/account-activity", h.listNatsAccountActivity)
	mux.HandleFunc("GET /api/nats/log", h.tailNatsLog)
	mux.HandleFunc("GET /api/kv/buckets", h.listKVBuckets)
	mux.HandleFunc("GET /api/kv/buckets/{account}/{bucket}/entries", h.kvBucketEntriesOnce)
	mux.HandleFunc("GET /api/jetstream/streams", h.listStreams)
	mux.HandleFunc("GET /api/jetstream/replay", h.jetstreamReplayOnce)
	mux.HandleFunc("GET /api/nats/services", h.listNatsServices)
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
