// Package accounts implements Phase 14b — a service that provisions NATS
// accounts dynamically via decentralized JWTs, replacing
// nats/bootstrap-operator.sh's one-shot nsc invocation (Phase 14a) with a
// live $SYS.REQ.CLAIMS.UPDATE/DELETE round trip callable at runtime.
//
// Routes (all gated by BasicAuth):
//
//	POST   /api/accounts                    create an account — mints an account + one user, returns the one-time .creds content
//	GET    /api/accounts                    list every known account (no creds, no signing key)
//	GET    /api/accounts/{name}             one account's details (no creds, no signing key)
//	POST   /api/accounts/{name}/suspend     suspend an account — revokes its resolver JWT via $SYS.REQ.CLAIMS.DELETE
//	POST   /api/accounts/{name}/reactivate  reactivate a suspended account — re-mints its resolver JWT via $SYS.REQ.CLAIMS.UPDATE and, when possible, a fresh one-time .creds
//	POST   /api/accounts/{name}/jslimits    update an account's JetStream resource limits — re-mints its resolver JWT with the new limits
package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// accountResponse omits SigningKeySeed — never serialized to a listing or
// detail response; only CreateAccount's one-time reply includes secrets
// (the .creds content), and even that never includes the account's own
// signing key seed, only the minted user's.
type accountResponse struct {
	Name           string `json:"name"`
	PublicKey      string `json:"publicKey"`
	Status         string `json:"status"`
	JSMaxMem       int64  `json:"jsMaxMem"`
	JSMaxFile      int64  `json:"jsMaxFile"`
	JSMaxStreams   int64  `json:"jsMaxStreams"`
	JSMaxConsumers int64  `json:"jsMaxConsumers"`
	CreatedAt      string `json:"createdAt"`
}

func toResponse(a Account) accountResponse {
	return accountResponse{
		Name:           a.Name,
		PublicKey:      a.PublicKey,
		Status:         a.Status,
		JSMaxMem:       a.JSMaxMem,
		JSMaxFile:      a.JSMaxFile,
		JSMaxStreams:   a.JSMaxStreams,
		JSMaxConsumers: a.JSMaxConsumers,
		CreatedAt:      a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

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

// Handlers wires the Store, Provisioner, and shared creds-output directory
// (the volume shipping-service also mounts, see docker-compose.yml) into
// the HTTP layer.
type Handlers struct {
	Store       *Store
	Provisioner *Provisioner
	CredsDir    string // shared nats-creds volume; new <name>.creds files are written here
	Log         *slog.Logger
	// NotifyNC is a DEFAULT-account connection (Phase 16h/BR-AC08) used only
	// to publish notify.accounts.account.created after a successful create —
	// nil-safe (publish is skipped) so this service still runs if that
	// connection isn't configured. Deliberately a separate connection from
	// Provisioner's sysNC: $SYS.REQ.CLAIMS.* and this notify event are
	// unrelated concerns, and shipping-service's subscriber (composition.go)
	// listens on its own DEFAULT-account connection — core NATS pub/sub
	// never crosses an account boundary, so publishing from sysNC would
	// never reach it.
	NotifyNC *nats.Conn
	// AuditLog is the append-only Postgres audit trail (BR-AC11) — nil-safe
	// (recordAudit skips silently), same contract as NotifyNC, so this
	// service still runs (just without an audit trail) if unset.
	AuditLog *AuditLog
	// UsageFetcher joins live /jsz stats with Postgres limits for the usage
	// endpoint — nil-safe (GET /api/accounts/usage returns 503 when unset).
	UsageFetcher *UsageFetcher
}

func NewHandlers(store *Store, provisioner *Provisioner, credsDir string, log *slog.Logger, notifyNC *nats.Conn, auditLog *AuditLog) *Handlers {
	return &Handlers{Store: store, Provisioner: provisioner, CredsDir: credsDir, Log: log, NotifyNC: notifyNC, AuditLog: auditLog}
}

// auditActor extracts a best-effort actor identity for an audit row
// (BR-AC11): the basic-auth username (always "admin" today, see
// BasicAuthUser — this service has one shared secret) overridden by an
// optional caller-supplied X-Actor header, plus the request's source
// address. Neither is authenticated identity; both are placeholders until
// WorkOS-backed human auth (see accounts_service_plan.md) provides a real
// principal — at that point this becomes strictly better data behind the
// same column, not a schema change.
func auditActor(r *http.Request) (actor, sourceIP string) {
	actor = "admin"
	if user, _, ok := r.BasicAuth(); ok && user != "" {
		actor = user
	}
	if xa := r.Header.Get("X-Actor"); xa != "" {
		actor = xa
	}
	return actor, r.RemoteAddr
}

// recordAudit is the shared best-effort call behind every audit write in
// this file (BR-AC11): a failed audit insert is logged but never
// propagated to the caller — the lifecycle action it's describing has
// already succeeded or failed on its own terms by the time this runs.
func (h *Handlers) recordAudit(ctx context.Context, entry AuditEntry) {
	if h.AuditLog == nil {
		return
	}
	if err := h.AuditLog.Record(ctx, entry); err != nil {
		h.Log.Error("record audit event", "account", entry.Account, "action", entry.Action, "err", err)
	}
}

// publishAccountCreated fire-and-forget notifies shipping-service (or any
// other interested service) that a new tenant now exists, so it can
// provision that tenant's resources immediately instead of waiting for a
// human to switch to it first (BR-AC08; shipping-service's
// Handlers.EnsureTenantByName, BR-030, is the consumer). Context-free
// subject — accounts-service has no {context} of its own, matching this
// service's other subjects (ARCHITECTURE-COMMUNICATIONS.md § "Context-free
// services"). Best-effort: a delivery failure here doesn't fail the create
// request — the account is already fully minted and persisted by the time
// this is called; EnsureAllTenants' startup scan or a later Admin UI
// SwitchTenant remain fallback paths if this specific event is ever missed.
func (h *Handlers) publishAccountCreated(name string) {
	h.publishAccountEvent("notify.accounts.account.created", "account-created", name)
}

// publishAccountEvent is the shared mechanics behind the three lifecycle
// publishers above and below — same payload shape, same nil-safe best-effort
// contract, same context-free subject family. Each caller keeps its own named
// method because the *reason* each event exists differs (and is documented on
// each); only the plumbing is shared.
func (h *Handlers) publishAccountEvent(subject, label, name string) {
	if h.NotifyNC == nil {
		return
	}
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		h.Log.Error("marshal "+label+" event", "name", name, "err", err)
		return
	}
	if err := h.NotifyNC.Publish(subject, payload); err != nil {
		h.Log.Error("publish "+label+" event", "name", name, "err", err)
	}
}

// publishAccountSuspended is the mirror of publishAccountCreated (BR-AC09):
// fire-and-forget notifies shipping-service that a tenant it may be holding
// a live connection for has just been revoked, so it can tear that
// connection down explicitly instead of letting nats.go's default reconnect
// logic retry forever against a .creds file suspendAccount has already
// deleted (shipping-service's Handlers.TeardownTenantByName, BR-031, is the
// consumer — see ARCHITECTURE-ACCOUNTS.md § 2t-a for the runtime behavior
// this closes). Same context-free subject family and same best-effort
// contract as publishAccountCreated: a delivery failure here doesn't fail
// the suspend request, since $SYS.REQ.CLAIMS.DELETE has already revoked the
// account at the resolver by the time this is called — that revoke is the
// actual security boundary, this is only what makes shipping-service notice
// promptly rather than eventually.
func (h *Handlers) publishAccountSuspended(name string) {
	h.publishAccountEvent("notify.accounts.account.suspended", "account-suspended", name)
}

// publishAccountReactivated completes the lifecycle triple (BR-AC10). Without
// it, BR-AC09's teardown is a one-way door: shipping-service drops a suspended
// tenant's resources and — since `EnsureAllTenants` only runs at startup and
// Sea Freight Flow never calls SwitchTenant (Phase 15d) — nothing ever rebuilds
// them when the tenant comes back, leaving a reactivated tenant unusable until
// a restart or an operator manually switching the Admin UI to it. That is the
// same gap BR-030 closed for newly-minted tenants, in a third position.
//
// Published after the *whole* reactivation commits (resolver JWT re-pushed,
// signing key persisted, fresh .creds written, status back to active) — the
// creds file in particular must already exist, since shipping-service's
// consumer resolves the tenant by scanning that directory (BR-032).
func (h *Handlers) publishAccountReactivated(name string) {
	h.publishAccountEvent("notify.accounts.account.reactivated", "account-reactivated", name)
}

// publishAccountJSLimitsUpdated notifies interested services that an
// account's JetStream resource limits have been changed (BR-AC12). Same
// context-free subject family and best-effort contract as the other three
// lifecycle publishers.
func (h *Handlers) publishAccountJSLimitsUpdated(name string) {
	h.publishAccountEvent("notify.accounts.account.jslimits_updated", "account-jslimits-updated", name)
}

func (h *Handlers) Mount(mux *http.ServeMux, authSecret string) {
	mux.Handle("POST /api/accounts", BasicAuth(authSecret, http.HandlerFunc(h.createAccount)))
	mux.Handle("GET /api/accounts", BasicAuth(authSecret, http.HandlerFunc(h.listAccounts)))
	mux.Handle("GET /api/accounts/usage", BasicAuth(authSecret, http.HandlerFunc(h.listJSUsage)))
	mux.Handle("GET /api/accounts/{name}", BasicAuth(authSecret, http.HandlerFunc(h.getAccount)))
	mux.Handle("POST /api/accounts/{name}/suspend", BasicAuth(authSecret, http.HandlerFunc(h.suspendAccount)))
	mux.Handle("POST /api/accounts/{name}/reactivate", BasicAuth(authSecret, http.HandlerFunc(h.reactivateAccount)))
	mux.Handle("POST /api/accounts/{name}/jslimits", BasicAuth(authSecret, http.HandlerFunc(h.updateJSLimits)))
}

func (h *Handlers) listJSUsage(w http.ResponseWriter, r *http.Request) {
	if h.UsageFetcher == nil {
		writeError(w, http.StatusServiceUnavailable, "JetStream usage monitoring is not configured (NATS_MONITOR_URL not set)")
		return
	}
	usage, err := h.UsageFetcher.FetchAll(r.Context())
	if err != nil {
		h.Log.Error("fetch js usage", "err", err)
		writeError(w, http.StatusBadGateway, "failed to fetch JetStream usage from NATS monitoring endpoint")
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

type createAccountRequest struct {
	Name           string `json:"name"`
	JSMaxMem       int64  `json:"jsMaxMem"`
	JSMaxFile      int64  `json:"jsMaxFile"`
	JSMaxStreams   int64  `json:"jsMaxStreams"`
	JSMaxConsumers int64  `json:"jsMaxConsumers"`
}

type createAccountResponse struct {
	Account accountResponse `json:"account"`
	Creds   string          `json:"creds"` // one-time: the newly-minted user's .creds file content
}

// defaultJSLimits mirror nats/bootstrap-operator.sh's ACME/GLOBEX tier
// (256M/1G/10/20) — used whenever a create request omits a limit (zero
// value), so a request that only sets `name` still gets a usable account
// rather than a JetStream-disabled one (all-zero limits).
var defaultJSLimits = JSLimits{MaxMem: 256 << 20, MaxFile: 1 << 30, MaxStreams: 10, MaxConsumers: 20}

// reservedAccountNames implements BR-AC06 (see BUSINESS_RULES-ACCOUNTS.md):
// DEFAULT and SYS are never mintable through this API, checked
// case-insensitively. DEFAULT is shipping-service's permanent connection and
// SYS is this service's own $SYS.REQ.CLAIMS.* credential — neither is a
// tenant, and shipping-service's discoverTenants (dictionary/internal/rest/
// tenant.go) relies on both being excludable from the switchable tenant
// list. That exclusion is itself just a case-sensitive filename match
// against the shared creds directory, so it only reliably holds if this
// service refuses to ever produce a same-named-but-differently-cased
// account (e.g. "Default", "sys") in the first place — see that file's
// nonTenantCredsFiles doc comment for the matching case-insensitive check
// on the other side.
var reservedAccountNames = map[string]bool{"DEFAULT": true, "SYS": true}

// reservedNamePrefix implements BR-AC07 (see BUSINESS_RULES-ACCOUNTS.md):
// account names beginning with "_" are reserved for platform/system use
// across the whole taxonomy — not this service's own concept, but the
// enforcement point for it that also happens to matter here, since in the
// common case (no company-group split — see Main-POC-Plan.md Phase 16
// decision 11) a tenant's own name doubles as its company `{context}` value
// (ARCHITECTURE-COMMUNICATIONS.md § 2.3). A tenant literally named e.g.
// "_ops" would let that reuse silently claim the reserved `_`-prefixed
// context namespace (`_platform`, Phase 16d) the moment its account name is
// used as a context. refdata-service's own `ValidateContextName` (BR-D33)
// is the primary enforcement point for context values specifically, since a
// context can be registered independently of any account — this check
// closes the same gap one level up, at the point a tenant identity is
// minted in the first place.
const reservedNamePrefix = "_"

func (h *Handlers) createAccount(w http.ResponseWriter, r *http.Request) {
	var in createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if reservedAccountNames[strings.ToUpper(in.Name)] {
		writeError(w, http.StatusConflict, "account name is reserved")
		return
	}
	if strings.HasPrefix(in.Name, reservedNamePrefix) {
		writeError(w, http.StatusBadRequest, "account name may not start with '_' — that prefix is reserved for platform/system use")
		return
	}

	if _, err := h.Store.Get(r.Context(), in.Name); err == nil {
		writeError(w, http.StatusConflict, "account name already exists")
		return
	} else if !errors.Is(err, ErrNotFound) {
		h.Log.Error("check existing account", "name", in.Name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	limits := JSLimits{in.JSMaxMem, in.JSMaxFile, in.JSMaxStreams, in.JSMaxConsumers}
	if limits == (JSLimits{}) {
		limits = defaultJSLimits
	}

	actor, sourceIP := auditActor(r)

	minted, err := h.Provisioner.CreateAccount(r.Context(), limits)
	if err != nil {
		h.Log.Error("mint account", "name", in.Name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: in.Name, Action: AuditActionCreated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "mint account", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "failed to mint account")
		return
	}

	credsBytes, err := h.Provisioner.CreateUser(minted.PublicKey, minted.SigningKeySeed, in.Name)
	if err != nil {
		h.Log.Error("mint user", "name", in.Name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: in.Name, Action: AuditActionCreated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "mint user", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "failed to mint user")
		return
	}

	if h.CredsDir != "" {
		credsPath := filepath.Join(h.CredsDir, in.Name+".creds")
		if err := os.WriteFile(credsPath, credsBytes, 0o600); err != nil {
			h.Log.Error("write creds file", "path", credsPath, "err", err)
			h.recordAudit(r.Context(), AuditEntry{Account: in.Name, Action: AuditActionCreated, Actor: actor, SourceIP: sourceIP,
				Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "write creds file", "error": err.Error()}})
			writeError(w, http.StatusInternalServerError, "account minted but failed to write creds file")
			return
		}
	}

	acc := Account{
		Name:           in.Name,
		PublicKey:      minted.PublicKey,
		SigningKeySeed: minted.SigningKeySeed,
		Status:         StatusActive,
		JSMaxMem:       limits.MaxMem,
		JSMaxFile:      limits.MaxFile,
		JSMaxStreams:   limits.MaxStreams,
		JSMaxConsumers: limits.MaxConsumers,
	}
	if err := h.Store.Insert(r.Context(), acc); err != nil {
		h.Log.Error("persist account", "name", in.Name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: in.Name, Action: AuditActionCreated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "persist account", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "account minted but failed to persist — resolver and Postgres are now inconsistent")
		return
	}
	h.recordAudit(r.Context(), AuditEntry{Account: in.Name, Action: AuditActionCreated, Actor: actor, SourceIP: sourceIP,
		Outcome: AuditOutcomeSuccess, Metadata: map[string]any{"publicKey": minted.PublicKey}})
	// Published only once the account is fully committed (resolver JWT,
	// creds file, and this Postgres row) — a subscriber reacting to it
	// should never see a half-provisioned tenant.
	h.publishAccountCreated(in.Name)

	stored, err := h.Store.Get(r.Context(), in.Name)
	if err != nil {
		h.Log.Error("reload account after insert", "name", in.Name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, createAccountResponse{
		Account: toResponse(stored),
		Creds:   string(credsBytes),
	})
}

func (h *Handlers) listAccounts(w http.ResponseWriter, r *http.Request) {
	accs, err := h.Store.List(r.Context())
	if err != nil {
		h.Log.Error("list accounts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]accountResponse, 0, len(accs))
	for _, a := range accs {
		out = append(out, toResponse(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) getAccount(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	acc, err := h.Store.Get(r.Context(), name)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		h.Log.Error("get account", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(acc))
}

func (h *Handlers) suspendAccount(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	acc, err := h.Store.Get(r.Context(), name)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		h.Log.Error("get account", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	actor, sourceIP := auditActor(r)

	if err := h.Provisioner.DeleteAccount(r.Context(), acc.PublicKey); err != nil {
		h.Log.Error("revoke account", "name", name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionSuspended, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "revoke account", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "failed to revoke account")
		return
	}
	if err := h.Store.SetStatus(r.Context(), name, StatusSuspended); err != nil {
		h.Log.Error("mark account suspended", "name", name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionSuspended, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "mark suspended", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "account revoked but failed to update status")
		return
	}
	h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionSuspended, Actor: actor, SourceIP: sourceIP, Outcome: AuditOutcomeSuccess})
	h.publishAccountSuspended(name)

	if h.CredsDir != "" {
		// Best-effort: remove the shared .creds file so shipping-service's
		// directory scan (composition.go) stops offering a now-revoked
		// tenant in the switch dropdown. Not fatal if it fails (e.g. seeded
		// accounts have no file here) — the account is already revoked
		// server-side, which is the actual security boundary.
		_ = os.Remove(filepath.Join(h.CredsDir, name+".creds"))
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": StatusSuspended})
}

type reactivateAccountResponse struct {
	Account accountResponse `json:"account"`
	Creds   string          `json:"creds"` // one-time: the newly-minted user's .creds file content (see BR-AC04)
}

// reactivateAccount implements BR-AC04 (see BUSINESS_RULES-ACCOUNTS.md): only
// a suspended account may be reactivated, back to its original public key
// and JetStream limits, always ending with a fresh, working user .creds —
// including for a seeded pre-existing account with no signing key on record
// (see Account.SigningKeySeed's doc comment), which establishes one now
// rather than leaving the account permanently unable to produce creds again
// (a real gap this closes: an account can otherwise end up "active" with no
// way to ever get a usable credential).
func (h *Handlers) reactivateAccount(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	acc, err := h.Store.Get(r.Context(), name)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		h.Log.Error("get account", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if acc.Status != StatusSuspended {
		writeError(w, http.StatusConflict, "account is not suspended")
		return
	}

	signingKeySeed := acc.SigningKeySeed
	if signingKeySeed == "" {
		signingKP, err := nkeys.CreateAccount()
		if err != nil {
			h.Log.Error("generate signing key for reactivation", "name", name, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to reactivate account")
			return
		}
		seed, err := signingKP.Seed()
		if err != nil {
			h.Log.Error("read generated signing key seed", "name", name, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to reactivate account")
			return
		}
		signingKeySeed = string(seed)
	}

	actor, sourceIP := auditActor(r)

	limits := JSLimits{MaxMem: acc.JSMaxMem, MaxFile: acc.JSMaxFile, MaxStreams: acc.JSMaxStreams, MaxConsumers: acc.JSMaxConsumers}
	if err := h.Provisioner.ReactivateAccount(r.Context(), acc.PublicKey, signingKeySeed, limits); err != nil {
		h.Log.Error("reactivate account", "name", name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionReactivated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "reactivate account", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "failed to reactivate account")
		return
	}

	if signingKeySeed != acc.SigningKeySeed {
		// Persist the newly-established signing key immediately: if the
		// creds-minting step below fails, the account is still left able to
		// mint creds on a retry (this call is otherwise idempotent — the
		// account is already active at the resolver at this point regardless).
		if err := h.Store.SetSigningKeySeed(r.Context(), name, signingKeySeed); err != nil {
			h.Log.Error("persist established signing key", "name", name, "err", err)
			h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionReactivated, Actor: actor, SourceIP: sourceIP,
				Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "persist signing key", "error": err.Error()}})
			writeError(w, http.StatusInternalServerError, "account reactivated but failed to persist its new signing key")
			return
		}
	}

	credsBytes, err := h.Provisioner.CreateUser(acc.PublicKey, signingKeySeed, acc.Name)
	if err != nil {
		h.Log.Error("mint user after reactivate", "name", name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionReactivated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "mint user", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "account reactivated but failed to mint new creds")
		return
	}
	if h.CredsDir != "" {
		credsPath := filepath.Join(h.CredsDir, acc.Name+".creds")
		if err := os.WriteFile(credsPath, credsBytes, 0o600); err != nil {
			h.Log.Error("write creds file", "path", credsPath, "err", err)
			h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionReactivated, Actor: actor, SourceIP: sourceIP,
				Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "write creds file", "error": err.Error()}})
			writeError(w, http.StatusInternalServerError, "account reactivated but failed to write creds file")
			return
		}
	}

	if err := h.Store.SetStatus(r.Context(), name, StatusActive); err != nil {
		h.Log.Error("mark account active", "name", name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionReactivated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "mark active", "error": err.Error()}})
		writeError(w, http.StatusInternalServerError, "account reactivated but failed to update status")
		return
	}
	h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionReactivated, Actor: actor, SourceIP: sourceIP, Outcome: AuditOutcomeSuccess})
	h.publishAccountReactivated(name)

	stored, err := h.Store.Get(r.Context(), name)
	if err != nil {
		h.Log.Error("reload account after reactivate", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, reactivateAccountResponse{
		Account: toResponse(stored),
		Creds:   string(credsBytes),
	})
}

type updateJSLimitsRequest struct {
	JSMaxMem       int64 `json:"jsMaxMem"`
	JSMaxFile      int64 `json:"jsMaxFile"`
	JSMaxStreams   int64 `json:"jsMaxStreams"`
	JSMaxConsumers int64 `json:"jsMaxConsumers"`
}

// updateJSLimits implements BR-AC12: re-mint the account JWT with new
// JetStream limits and push it to the resolver, then persist the new limits
// to Postgres. No status gate — works whether active or suspended.
func (h *Handlers) updateJSLimits(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	acc, err := h.Store.Get(r.Context(), name)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		h.Log.Error("get account", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var in updateJSLimitsRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.JSMaxMem < 0 || in.JSMaxFile < 0 || in.JSMaxStreams < 0 || in.JSMaxConsumers < 0 {
		writeError(w, http.StatusBadRequest, "JetStream limits must not be negative")
		return
	}

	signingKeySeed := acc.SigningKeySeed
	if signingKeySeed == "" {
		signingKP, err := nkeys.CreateAccount()
		if err != nil {
			h.Log.Error("generate signing key for jslimits update", "name", name, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to update limits")
			return
		}
		seed, err := signingKP.Seed()
		if err != nil {
			h.Log.Error("read generated signing key seed", "name", name, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to update limits")
			return
		}
		signingKeySeed = string(seed)
	}

	actor, sourceIP := auditActor(r)
	previous := map[string]any{
		"jsMaxMem": acc.JSMaxMem, "jsMaxFile": acc.JSMaxFile,
		"jsMaxStreams": acc.JSMaxStreams, "jsMaxConsumers": acc.JSMaxConsumers,
	}

	newLimits := JSLimits{MaxMem: in.JSMaxMem, MaxFile: in.JSMaxFile, MaxStreams: in.JSMaxStreams, MaxConsumers: in.JSMaxConsumers}
	if err := h.Provisioner.UpdateAccountLimits(r.Context(), acc.PublicKey, signingKeySeed, newLimits); err != nil {
		h.Log.Error("update account limits", "name", name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionJSLimitsUpdated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "update js limits", "error": err.Error(), "previous": previous}})
		writeError(w, http.StatusInternalServerError, "failed to update account limits")
		return
	}

	if signingKeySeed != acc.SigningKeySeed {
		if err := h.Store.SetSigningKeySeed(r.Context(), name, signingKeySeed); err != nil {
			h.Log.Error("persist established signing key", "name", name, "err", err)
			h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionJSLimitsUpdated, Actor: actor, SourceIP: sourceIP,
				Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "persist signing key", "error": err.Error(), "previous": previous}})
			writeError(w, http.StatusInternalServerError, "limits pushed to resolver but failed to persist signing key")
			return
		}
	}

	if err := h.Store.SetJSLimits(r.Context(), name, newLimits); err != nil {
		h.Log.Error("persist js limits", "name", name, "err", err)
		h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionJSLimitsUpdated, Actor: actor, SourceIP: sourceIP,
			Outcome: AuditOutcomeFailed, Metadata: map[string]any{"step": "persist limits", "error": err.Error(), "previous": previous}})
		writeError(w, http.StatusInternalServerError, "limits pushed to resolver but failed to persist — resolver and Postgres are now inconsistent")
		return
	}

	requested := map[string]any{
		"jsMaxMem": in.JSMaxMem, "jsMaxFile": in.JSMaxFile,
		"jsMaxStreams": in.JSMaxStreams, "jsMaxConsumers": in.JSMaxConsumers,
	}
	h.recordAudit(r.Context(), AuditEntry{Account: name, Action: AuditActionJSLimitsUpdated, Actor: actor, SourceIP: sourceIP,
		Outcome: AuditOutcomeSuccess, Metadata: map[string]any{"previous": previous, "requested": requested}})
	h.publishAccountJSLimitsUpdated(name)

	stored, err := h.Store.Get(r.Context(), name)
	if err != nil {
		h.Log.Error("reload account after jslimits update", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, toResponse(stored))
}
