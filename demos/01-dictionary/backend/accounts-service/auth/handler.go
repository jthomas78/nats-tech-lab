// Package auth implements Phase 15c (folded into accounts-service in Phase
// 19 — see Main-POC-Plan.md): minting short-lived, permission-restricted
// NATS user JWTs so a browser can connect directly to NATS over WebSocket
// (api.*/notify.* only — see token.go) instead of going through REST + SSE
// for every read and write. Reads accounts-service's own Store directly
// (Phase 19 replaced the cross-service Postgres read this package used to
// do as a separate binary — see accounts.Store.ListActiveTenantNames' doc
// comment).
//
// Routes (no auth gate — see connectInfo's doc comment for why):
//
//	GET   /api/auth/connectInfo?tenant={name}   mint a browser NATS user JWT for tenant
//	GET   /api/auth/adminConnectInfo            mint a browser NATS user JWT under PLATFORM (Phase 23, BR-AC18)
//	GET   /api/auth/refdataAdminConnectInfo     mint a refdata-admin-UI NATS user JWT under PLATFORM (Phase 32)
//	GET   /api/auth/tenants                     list switchable tenant names
//	POST  /api/auth/login                       placeholder for the future WorkOS flow (BR-UA01) — 501
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// Handlers wires accounts-service's own Store and the NATS WebSocket URL
// the browser should dial into the HTTP layer.
type Handlers struct {
	Store *accounts.Store
	WSUrl string // e.g. "ws://localhost:9222" — returned verbatim in connectInfo
	Log   *slog.Logger
}

func NewHandlers(store *accounts.Store, wsURL string, log *slog.Logger) *Handlers {
	return &Handlers{Store: store, WSUrl: wsURL, Log: log}
}

// tokenTTL resolves the currently-configured browser/admin JWT TTL (BR-AC20)
// from the system-config row, read fresh on every mint so a change from the
// Admin UI takes effect on the next connect without a restart. A read failure
// falls back to the default rather than failing the mint — issuing a
// short-lived credential on the default TTL is strictly safer than refusing to
// connect the browser at all.
func (h *Handlers) tokenTTL(ctx context.Context) time.Duration {
	cfg, err := h.Store.GetTokenTTLConfig(ctx)
	if err != nil {
		h.Log.Warn("read token TTL config; using default", "err", err)
		return accounts.DefaultTokenTTLConfig().TTL()
	}
	return cfg.TTL()
}

// platformAccountName is accounts-service's own lowercase row name for the
// seeded PLATFORM account (cmd/main.go's seedPreexistingAccounts) — the
// same identity ensureSigningKey establishes a signing key for at startup.
const platformAccountName = "platform"

// Mount registers this service's deliberately ungated /api/auth/* routes and
// returns the exact list of "METHOD /pattern" strings registered, in
// registration order (BR-040/BR-AC33) — so a test can assert this list
// ConsistOf a hardcoded allowlist and catch a future route sneaking onto this
// mux, not just a code-review miss.
func (h *Handlers) Mount(mux *http.ServeMux) []string {
	routes := []string{
		"GET /api/auth/connectInfo",
		"GET /api/auth/adminConnectInfo",
		"GET /api/auth/refdataAdminConnectInfo",
		"GET /api/auth/tenants",
		"POST /api/auth/login",
	}
	mux.HandleFunc(routes[0], h.connectInfo)
	mux.HandleFunc(routes[1], h.adminConnectInfo)
	mux.HandleFunc(routes[2], h.refdataAdminConnectInfo)
	mux.HandleFunc(routes[3], h.tenants)
	mux.HandleFunc(routes[4], h.login)
	return routes
}

// connectInfo mints and returns a fresh browser NATS credential for the
// requested tenant. Deliberately ungated: this endpoint IS how the browser
// obtains its first credential (there is nothing to authenticate against
// yet), matching BR-UA01's JIT-provisioning intent minus the WorkOS login
// step it's a placeholder for — see login below and Main-POC-Plan.md
// Phase 15c's "known POC trade-offs" for why this is acceptable for a local
// lab stack and what production would add in front of it.
//
// @Summary      Mint a browser NATS credential for a tenant
// @Description  Deliberately ungated — this endpoint IS how the browser obtains its first credential. Requires the tenant to be active and to have a signing key on record.
// @Tags         auth
// @Produce      json
// @Param        tenant  query     string  true  "Tenant account name"
// @Success      200     {object}  auth.ConnectInfo
// @Failure      400     {object}  errorResponse  "Missing tenant"
// @Failure      403     {object}  errorResponse  "Tenant is not active"
// @Failure      404     {object}  errorResponse  "Unknown tenant"
// @Failure      409     {object}  errorResponse  "Tenant has no signing key on record"
// @Failure      500     {object}  errorResponse
// @Router       /api/auth/connectInfo [get]
func (h *Handlers) connectInfo(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant is required")
		return
	}

	acc, err := h.Store.Get(r.Context(), tenant)
	if errors.Is(err, accounts.ErrNotFound) {
		writeError(w, http.StatusNotFound, "unknown tenant")
		return
	}
	if err != nil {
		h.Log.Error("look up tenant", "tenant", tenant, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if acc.Status != accounts.StatusActive {
		writeError(w, http.StatusForbidden, "tenant is not active")
		return
	}
	if acc.SigningKeySeed == "" {
		// A seeded pre-existing account (PLATFORM/ACME/GLOBEX) that
		// accounts-service has never minted a signing key for — see
		// accounts/store.go's Account.SigningKeySeed doc comment. Without a
		// signing key this service cannot sign a user JWT for it.
		writeError(w, http.StatusConflict, "tenant has no signing key on record")
		return
	}

	info, err := MintBrowserToken(acc.PublicKey, acc.SigningKeySeed, tenant, h.WSUrl, h.tokenTTL(r.Context()))
	if err != nil {
		h.Log.Error("mint browser token", "tenant", tenant, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to mint browser credential")
		return
	}

	writeJSON(w, http.StatusOK, info)
}

// adminConnectInfo mints and returns a fresh browser NATS credential for the
// Admin UI's own PLATFORM-account connection (Phase 23, BR-AC18). PLATFORM
// is not a tenant — deliberately not routed through connectInfo's
// Status-gated tenant lookup; it looks up the fixed "platform" row directly
// and mints via MintAdminToken's own restricted PLATFORM permission profile
// instead of MintBrowserToken's tenant-shaped one.
//
// @Summary      Mint a browser NATS credential for the Admin UI
// @Description  Mints a PLATFORM credential for centralized notifications and exact read-only refdata requests (BR-AC18).
// @Tags         auth
// @Produce      json
// @Success      200  {object}  auth.ConnectInfo
// @Failure      404  {object}  errorResponse  "PLATFORM account not seeded"
// @Failure      409  {object}  errorResponse  "PLATFORM account has no signing key on record"
// @Failure      500  {object}  errorResponse
// @Router       /api/auth/adminConnectInfo [get]
func (h *Handlers) adminConnectInfo(w http.ResponseWriter, r *http.Request) {
	acc, err := h.Store.Get(r.Context(), platformAccountName)
	if errors.Is(err, accounts.ErrNotFound) {
		writeError(w, http.StatusNotFound, "platform account not seeded")
		return
	}
	if err != nil {
		h.Log.Error("look up platform account", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if acc.SigningKeySeed == "" {
		// cmd/main.go's ensureSigningKey establishes this at every startup —
		// an empty seed here means that step hasn't run yet or failed.
		writeError(w, http.StatusConflict, "platform account has no signing key on record")
		return
	}

	info, err := MintAdminToken(acc.PublicKey, acc.SigningKeySeed, h.WSUrl, h.tokenTTL(r.Context()))
	if err != nil {
		h.Log.Error("mint admin token", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to mint admin credential")
		return
	}

	writeJSON(w, http.StatusOK, info)
}

// refdataAdminConnectInfo mints and returns a fresh NATS credential for the
// refdata admin UI's (frontend/refdata) own PLATFORM-account connection
// (Phase 32) — sibling to adminConnectInfo above, same fixed "platform" row
// lookup, but MintRefdataAdminToken's full refdata-scoped permission profile
// instead of MintAdminToken's three exact read subjects (see
// MintRefdataAdminToken's doc comment for why this needs its own PLATFORM
// credential rather than reusing either MintAdminToken or MintBrowserToken).
//
// @Summary      Mint a browser NATS credential for the refdata admin UI
// @Description  Mints a publish-capable, refdata-scoped PLATFORM-account credential for the refdata admin UI's own connection (Phase 32).
// @Tags         auth
// @Produce      json
// @Success      200  {object}  auth.ConnectInfo
// @Failure      404  {object}  errorResponse  "PLATFORM account not seeded"
// @Failure      409  {object}  errorResponse  "PLATFORM account has no signing key on record"
// @Failure      500  {object}  errorResponse
// @Router       /api/auth/refdataAdminConnectInfo [get]
func (h *Handlers) refdataAdminConnectInfo(w http.ResponseWriter, r *http.Request) {
	acc, err := h.Store.Get(r.Context(), platformAccountName)
	if errors.Is(err, accounts.ErrNotFound) {
		writeError(w, http.StatusNotFound, "platform account not seeded")
		return
	}
	if err != nil {
		h.Log.Error("look up platform account", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if acc.SigningKeySeed == "" {
		writeError(w, http.StatusConflict, "platform account has no signing key on record")
		return
	}

	info, err := MintRefdataAdminToken(acc.PublicKey, acc.SigningKeySeed, h.WSUrl, h.tokenTTL(r.Context()))
	if err != nil {
		h.Log.Error("mint refdata admin token", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to mint refdata admin credential")
		return
	}

	writeJSON(w, http.StatusOK, info)
}

type tenantsResponse struct {
	Tenants []string `json:"tenants"`
}

// @Summary      List switchable tenants
// @Description  Every active tenant account name, for the Admin UI's tenant switcher.
// @Tags         auth
// @Produce      json
// @Success      200  {object}  tenantsResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/auth/tenants [get]
func (h *Handlers) tenants(w http.ResponseWriter, r *http.Request) {
	names, err := h.Store.ListActiveTenantNames(r.Context())
	if err != nil {
		h.Log.Error("list tenants", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tenantsResponse{Tenants: names})
}

// login is a placeholder for BR-UA01's WorkOS-first JIT-provisioning flow —
// out of scope for Phase 15c, which only needs connectInfo to let an
// already-known tenant's browser connect directly to NATS.
//
// @Summary      Log in (not yet implemented)
// @Description  Placeholder for the future WorkOS-backed login flow (BR-UA01).
// @Tags         auth
// @Produce      json
// @Failure      501  {object}  errorResponse
// @Router       /api/auth/login [post]
func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "login is not yet implemented (BR-UA01)")
}
