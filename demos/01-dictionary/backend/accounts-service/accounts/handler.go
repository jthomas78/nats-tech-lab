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
//	GET    /api/accounts/topology           every declared export/import edge across all accounts, read from resolver JWTs — see topology.go
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
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
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
	// NotifyNC is a PLATFORM-account connection (Phase 16h/BR-AC08) used only
	// to publish notify.accounts.account.created after a successful create —
	// nil-safe (publish is skipped) so this service still runs if that
	// connection isn't configured. Deliberately a separate connection from
	// Provisioner's sysNC: $SYS.REQ.CLAIMS.* and this notify event are
	// unrelated concerns, and shipping-service's subscriber (composition.go)
	// listens on its own PLATFORM-account connection — core NATS pub/sub
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
	// RefdataURL is the base URL of refdata-service (e.g. http://refdata-service:8080).
	// Used by the BU endpoints to register/hide contexts in refdata. Best-effort:
	// empty string skips the refdata call with a warning, but the BU row is still
	// persisted locally so the lookup stays consistent.
	RefdataURL string
}

func NewHandlers(store *Store, provisioner *Provisioner, credsDir string, log *slog.Logger, notifyNC *nats.Conn, auditLog *AuditLog) *Handlers {
	return &Handlers{Store: store, Provisioner: provisioner, CredsDir: credsDir, Log: log, NotifyNC: notifyNC, AuditLog: auditLog}
}

// Refdata builds the writer-side refdata-service client from RefdataURL.
// Cheap enough to build per-call (no connection state) and keeps RefdataURL as
// the one field composition.go/cmd/main.go need to set.
func (h *Handlers) Refdata() *RefdataClient {
	return &RefdataClient{BaseURL: h.RefdataURL, Log: h.Log}
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

// crossAccountOpts supplies the PLATFORM identity only for tenant claims.
// Active-account limit updates preserve the resolver JWT directly; a
// suspended account has no JWT to inspect, so reactivation uses this fallback
// to restore its imports. PLATFORM itself must never import itself.
func (h *Handlers) crossAccountOpts(ctx context.Context, accountPub, accountName string) CrossAccountOpts {
	platform, err := h.Store.Get(ctx, "platform")
	if err != nil {
		h.Log.Warn("get PLATFORM account for claim preservation", "err", err)
		return CrossAccountOpts{}
	}
	if platform.PublicKey == accountPub {
		return CrossAccountOpts{}
	}
	return CrossAccountOpts{PlatformPublicKey: platform.PublicKey, TenantName: accountName}
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
func (h *Handlers) publishAccountCreated(ctx context.Context, name string) {
	h.publishAccountEvent(ctx, "created", "account-created", name)
}

// publishAccountEvent is the shared mechanics behind the four lifecycle
// publishers above and below — same payload shape, same nil-safe best-effort
// contract, same context-free subject family. Each caller keeps its own named
// method because the *reason* each event exists differs (and is documented on
// each); only the plumbing is shared.
//
// Phase 28e (BR-037) traced this publish as its own outbound span on
// obs.trace.*. Phase 43a (BR-AC34) moved it to obs.pubsub.*, the fire-and-
// forget channel it always belonged on — see the comment on the emit below
// for why the intermediate span went with it. Nil-safe throughout: no
// NotifyNC means neither the notify nor its observation happens.
// lifecycleSubject builds notify.accounts.account.{action} and the tokens it
// is observed under.
//
// This is the notify.* family's irregular member: four tokens where its
// siblings have five to seven, and no {context} at all, because this service
// administers the tenant axis itself rather than operating inside one. That
// puts it below natstrace.ObservePublish's five-token floor, so a subject-
// derived attribution would not merely be wrong here — it would skip these
// four events silently. The tokens are named instead, with the context this
// service does belong to.
//
// The shape is deliberately not regularised: the subject is pinned in JWT
// permission grants (auth/token.go), in shipping-service's subscribers and in
// bootstrap-operator.sh's minted credentials, and Provisioner.CreateUser
// mints unrestricted users, so a Go-side regeneration would silently drop the
// scoped grants (see Phase 60).
func lifecycleSubject(action string) natsnotify.Subject {
	return natsnotify.Subject{
		Name: "notify.accounts.account." + action,
		Tokens: natsnotify.Tokens{
			Context: "_platform",
			Service: "accounts",
			Entity:  "account",
			Action:  action,
		},
	}
}

func (h *Handlers) publishAccountEvent(ctx context.Context, action, label, name string) {
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
	// Phase 43a (BR-AC34): these four publishes moved from obs.trace.* to
	// obs.pubsub.*. They are fire-and-forget notifications, and obs.trace.* is
	// the request/reply channel — carrying them there was the anomaly, and one
	// event on two channels would defeat BR-047's per-channel dedup. The
	// notify continues the causing request's span, which is what BR-AC34 asked
	// to keep; Phase 43d moved that continuation into the seam, where it is
	// the same code path every other notify.* publisher now takes.
	natsnotify.New(h.NotifyNC, h.Log, natsnotify.WithObservation(h.NotifyNC)).
		Publish(ctx, lifecycleSubject(action), payload)
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
func (h *Handlers) publishAccountSuspended(ctx context.Context, name string) {
	h.publishAccountEvent(ctx, "suspended", "account-suspended", name)
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
func (h *Handlers) publishAccountReactivated(ctx context.Context, name string) {
	h.publishAccountEvent(ctx, "reactivated", "account-reactivated", name)
}

// publishAccountJSLimitsUpdated notifies interested services that an
// account's JetStream resource limits have been changed (BR-AC12). Same
// context-free subject family and best-effort contract as the other three
// lifecycle publishers.
func (h *Handlers) publishAccountJSLimitsUpdated(ctx context.Context, name string) {
	h.publishAccountEvent(ctx, "jslimits_updated", "account-jslimits-updated", name)
}

// Mount registers this service's BasicAuth-gated /api/accounts* routes and
// returns the exact list of "METHOD /pattern" strings registered, in
// registration order (BR-040/BR-AC33) — so a test can assert this list
// ConsistOf a hardcoded allowlist and catch a future business route sneaking
// onto this mux, not just a code-review miss.
func (h *Handlers) Mount(mux *http.ServeMux, authSecret string) []string {
	var routes []string
	handle := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, BasicAuth(authSecret, fn))
		routes = append(routes, pattern)
	}

	handle("POST /api/accounts", h.createAccount)
	handle("GET /api/accounts", h.listAccounts)
	handle("GET /api/accounts/usage", h.listJSUsage)
	handle("GET /api/accounts/topology", h.listTopology)
	handle("GET /api/accounts/{name}", h.getAccount)
	handle("POST /api/accounts/{name}/suspend", h.suspendAccount)
	handle("POST /api/accounts/{name}/reactivate", h.reactivateAccount)
	handle("POST /api/accounts/{name}/jslimits", h.updateJSLimits)

	// BR-AC20: platform-global system config (not account-scoped — sits under
	// the /api/accounts prefix alongside the other collection-level endpoints
	// /usage and /topology so it reuses the existing /api/platform/accounts
	// proxy rewrite; the {name} route above never matches the literal
	// "system-config" segment).
	handle("GET /api/accounts/system-config", h.getSystemConfig)
	handle("PUT /api/accounts/system-config", h.updateSystemConfig)
	// Phase 22: business unit management (BR-AC15/BR-AC16/BR-AC17)
	handle("GET /api/accounts/{name}/business-units", h.listBusinessUnits)
	handle("POST /api/accounts/{name}/business-units", h.createBusinessUnit)
	handle("PATCH /api/accounts/{name}/business-units/{buContext}", h.updateBusinessUnit)

	return routes
}

// @Summary      List JetStream usage
// @Description  Live per-account JetStream resource usage (streams/consumers/mem/file) joined with each account's configured limits, read from the NATS server's /jsz monitoring endpoint. 503 if NATS_MONITOR_URL is not configured.
// @Tags         accounts
// @Produce      json
// @Success      200  {array}   JSUsage
// @Failure      502  {object}  errorResponse  "Failed to fetch usage from NATS monitoring endpoint"
// @Failure      503  {object}  errorResponse  "JetStream usage monitoring not configured"
// @Router       /api/accounts/usage [get]
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
// PLATFORM and SYS are never mintable through this API, checked
// case-insensitively. PLATFORM is shipping-service's permanent connection and
// SYS is this service's own $SYS.REQ.CLAIMS.* credential — neither is a
// tenant, and shipping-service's discoverTenants (dictionary/internal/rest/
// tenant.go) relies on both being excludable from the switchable tenant
// list. That exclusion is itself just a case-sensitive filename match
// against the shared creds directory, so it only reliably holds if this
// service refuses to ever produce a same-named-but-differently-cased
// account (e.g. "Platform", "sys") in the first place — see that file's
// nonTenantCredsFiles doc comment for the matching case-insensitive check
// on the other side.
var reservedAccountNames = map[string]bool{"PLATFORM": true, "SYS": true}

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

// @Summary      Create an account
// @Description  Mints a new NATS account plus one user, returns the one-time .creds content. JetStream limits default to the standard tenant tier when omitted.
// @Tags         accounts
// @Accept       json
// @Produce      json
// @Param        body  body      createAccountRequest  true  "Account name and optional JetStream limits"
// @Success      201   {object}  createAccountResponse
// @Failure      400   {object}  errorResponse  "Missing name"
// @Failure      409   {object}  errorResponse  "Reserved or already-existing account name"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts [post]
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

	platform, err := h.Store.Get(r.Context(), "platform")
	if err != nil && !errors.Is(err, ErrNotFound) {
		h.Log.Error("get PLATFORM account for tenant imports", "err", err)
		writeError(w, http.StatusInternalServerError, "platform account is unavailable")
		return
	}
	platformPublicKey := ""
	if err == nil {
		platformPublicKey = platform.PublicKey
	}

	minted, err := h.Provisioner.CreateAccount(r.Context(), limits, in.Name, platformPublicKey)
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
	h.publishAccountCreated(r.Context(), in.Name)

	stored, err := h.Store.Get(r.Context(), in.Name)
	if err != nil {
		h.Log.Error("reload account after insert", "name", in.Name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// BR-AC16/BR-AC28: auto-create the account's own {tenant}-default BU row
	// using the stored UUID. Best-effort: a failure here is logged but does
	// not fail the request — the account is already fully minted and
	// persisted.
	defaultSlug := DefaultContext(in.Name)
	if err := h.Store.InsertBusinessUnit(r.Context(), NewBusinessUnit{
		AccountID: stored.ID,
		Name:      DefaultBUName,
		Context:   defaultSlug,
		Visible:   true,
		IsDefault: true,
	}); err != nil && !errors.Is(err, ErrBUDuplicate) {
		h.Log.Warn("auto-create default business unit", "account", in.Name, "err", err)
	}
	// BR-AC29: provisioning the refdata-service side (register context, add
	// locales, draft+publish so it inherits the platform template) can poll
	// for up to 30s waiting on _default_bu's corpus — run it off the request
	// path so a cold refdata-service startup never turns into a slow tenant
	// registration. Detached from r.Context(), which is canceled the moment
	// this handler returns.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := h.Refdata().ProvisionDefaultContext(ctx, in.Name, defaultSlug); err != nil {
			h.Log.Warn("provision default BU context in refdata", "account", in.Name, "context", defaultSlug, "err", err)
		}
	}()

	writeJSON(w, http.StatusCreated, createAccountResponse{
		Account: toResponse(stored),
		Creds:   string(credsBytes),
	})
}

// @Summary      List accounts
// @Description  Every known account, without creds or signing key material.
// @Tags         accounts
// @Produce      json
// @Success      200  {array}   accountResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/accounts [get]
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

// @Summary      Get an account
// @Description  One account's details, without creds or signing key material.
// @Tags         accounts
// @Produce      json
// @Param        name  path      string  true  "Account name"
// @Success      200   {object}  accountResponse
// @Failure      404   {object}  errorResponse  "Account not found"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts/{name} [get]
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

// BR-AC13 (see BUSINESS_RULES-ACCOUNTS.md): the PLATFORM account is mandatory
// on every deployment and can never be suspended — unlike suspending a
// tenant account, which only takes that one tenant offline, suspending
// PLATFORM would revoke the resolver JWT that shipping-service's and
// refdata-service's permanent connections (nats.Connect with platform.creds)
// depend on, severing cross-tenant infrastructure (refdata rpc.*, the
// REFDATA change stream, the obs.rpc.> observability bridge) for every
// tenant at once. Reuses reservedAccountNames (BR-AC06) rather than a
// PLATFORM-only literal, since SYS is equally infrastructure-critical
// (though it never has a Postgres row to suspend in the first place — see
// seedPreexistingAccounts).
//
// @Summary      Suspend an account
// @Description  Revokes the account's resolver JWT via $SYS.REQ.CLAIMS.DELETE and marks it suspended. PLATFORM/SYS can never be suspended.
// @Tags         accounts
// @Produce      json
// @Param        name  path      string  true  "Account name"
// @Success      200   {object}  map[string]string
// @Failure      404   {object}  errorResponse  "Account not found"
// @Failure      409   {object}  errorResponse  "Account is reserved"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts/{name}/suspend [post]
func (h *Handlers) suspendAccount(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if reservedAccountNames[strings.ToUpper(name)] {
		writeError(w, http.StatusConflict, "account is reserved and can never be suspended")
		return
	}
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
	h.publishAccountSuspended(r.Context(), name)

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
//
// @Summary      Reactivate a suspended account
// @Description  Re-mints the account's resolver JWT via $SYS.REQ.CLAIMS.UPDATE with its original public key and JetStream limits, and mints a fresh one-time .creds.
// @Tags         accounts
// @Produce      json
// @Param        name  path      string  true  "Account name"
// @Success      200   {object}  reactivateAccountResponse
// @Failure      404   {object}  errorResponse  "Account not found"
// @Failure      409   {object}  errorResponse  "Account is not suspended"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts/{name}/reactivate [post]
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
	if err := h.Provisioner.ReactivateAccount(r.Context(), acc.PublicKey, signingKeySeed, limits, h.crossAccountOpts(r.Context(), acc.PublicKey, acc.Name), nil); err != nil {
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
	h.publishAccountReactivated(r.Context(), name)

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
// @Summary      Update an account's JetStream limits
// @Description  Re-mints the account's resolver JWT with new JetStream resource limits and persists them to Postgres. Works whether the account is active or suspended.
// @Tags         accounts
// @Accept       json
// @Produce      json
// @Param        name  path      string                  true  "Account name"
// @Param        body  body      updateJSLimitsRequest   true  "New JetStream limits"
// @Success      200   {object}  accountResponse
// @Failure      400   {object}  errorResponse  "Invalid body or negative limit"
// @Failure      404   {object}  errorResponse  "Account not found"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts/{name}/jslimits [post]
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
	if err := h.Provisioner.UpdateAccountLimits(r.Context(), acc.PublicKey, signingKeySeed, newLimits, h.crossAccountOpts(r.Context(), acc.PublicKey, acc.Name)); err != nil {
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
	h.publishAccountJSLimitsUpdated(r.Context(), name)

	stored, err := h.Store.Get(r.Context(), name)
	if err != nil {
		h.Log.Error("reload account after jslimits update", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, toResponse(stored))
}

// systemConfigResponse is the shape the Admin UI's System Settings screen
// reads and writes. envelopeMin/Max expose the hard BR-UA03 bounds so the UI
// can constrain both editors without hardcoding them; they are read-only.
type systemConfigResponse struct {
	TokenTTLMinutes    int    `json:"tokenTtlMinutes"`
	TokenTTLMinMinutes int    `json:"tokenTtlMinMinutes"`
	TokenTTLMaxMinutes int    `json:"tokenTtlMaxMinutes"`
	EnvelopeMinMinutes int    `json:"envelopeMinMinutes"`
	EnvelopeMaxMinutes int    `json:"envelopeMaxMinutes"`
	UpdatedAt          string `json:"updatedAt,omitempty"`
}

func systemConfigToResponse(c TokenTTLConfig) systemConfigResponse {
	resp := systemConfigResponse{
		TokenTTLMinutes:    c.ValueMinutes,
		TokenTTLMinMinutes: c.MinMinutes,
		TokenTTLMaxMinutes: c.MaxMinutes,
		EnvelopeMinMinutes: MinTTLMinutes,
		EnvelopeMaxMinutes: MaxTTLMinutes,
	}
	if !c.UpdatedAt.IsZero() {
		resp.UpdatedAt = c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

type updateSystemConfigRequest struct {
	TokenTTLMinutes    int `json:"tokenTtlMinutes"`
	TokenTTLMinMinutes int `json:"tokenTtlMinMinutes"`
	TokenTTLMaxMinutes int `json:"tokenTtlMaxMinutes"`
}

// getSystemConfig returns the current browser/admin JWT expiry policy
// (BR-AC20).
// @Summary      Get system config
// @Description  The current browser/admin JWT expiry policy (BR-AC20), including the hard BR-UA03 min/max bounds.
// @Tags         system
// @Produce      json
// @Success      200  {object}  systemConfigResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/accounts/system-config [get]
func (h *Handlers) getSystemConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Store.GetTokenTTLConfig(r.Context())
	if err != nil {
		h.Log.Error("get system config", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, systemConfigToResponse(cfg))
}

// updateSystemConfig validates (BR-AC21) and persists a new expiry policy.
// Unlike updateJSLimits it does not touch NATS: the TTL affects only *future*
// mints (auth.MintBrowserToken/MintAdminToken read it per connect), so there
// is no resolver push and no notify — the next browser (re)connect picks it up.
// @Summary      Update system config
// @Description  Validates (BR-AC21) and persists a new browser/admin JWT expiry policy. Affects only future token mints — no resolver push, no notify.
// @Tags         system
// @Accept       json
// @Produce      json
// @Param        body  body      updateSystemConfigRequest  true  "New token TTL policy"
// @Success      200   {object}  systemConfigResponse
// @Failure      400   {object}  errorResponse  "Invalid body or out-of-bounds policy"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts/system-config [put]
func (h *Handlers) updateSystemConfig(w http.ResponseWriter, r *http.Request) {
	var in updateSystemConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := TokenTTLConfig{
		ValueMinutes: in.TokenTTLMinutes,
		MinMinutes:   in.TokenTTLMinMinutes,
		MaxMinutes:   in.TokenTTLMaxMinutes,
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.Store.SetTokenTTLConfig(r.Context(), cfg); err != nil {
		h.Log.Error("persist system config", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to persist system config")
		return
	}

	stored, err := h.Store.GetTokenTTLConfig(r.Context())
	if err != nil {
		h.Log.Error("reload system config", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, systemConfigToResponse(stored))
}

// --- Phase 22: Business Unit endpoints (BR-AC15/BR-AC16/BR-AC17) ---

type businessUnitResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Context   string `json:"context"`
	Visible   bool   `json:"visible"`
	IsDefault bool   `json:"isDefault"`
	CreatedAt string `json:"createdAt"`
}

func toBUResponse(bu BusinessUnit) businessUnitResponse {
	return businessUnitResponse{
		ID:        bu.ID,
		Name:      bu.Name,
		Context:   bu.Context,
		Visible:   bu.Visible,
		IsDefault: bu.IsDefault,
		CreatedAt: bu.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// @Summary      List an account's business units
// @Description  Every business unit registered under one account (Phase 22, BR-AC15).
// @Tags         business-units
// @Produce      json
// @Param        name  path      string  true  "Account name"
// @Success      200   {array}   businessUnitResponse
// @Failure      404   {object}  errorResponse  "Account not found"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts/{name}/business-units [get]
func (h *Handlers) listBusinessUnits(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := h.Store.Get(r.Context(), name); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "account not found")
		return
	} else if err != nil {
		h.Log.Error("get account for BU list", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	bus, err := h.Store.ListBusinessUnits(r.Context(), name)
	if err != nil {
		h.Log.Error("list business units", "account", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]businessUnitResponse, 0, len(bus))
	for _, bu := range bus {
		out = append(out, toBUResponse(bu))
	}
	writeJSON(w, http.StatusOK, out)
}

// createBURequest carries the display name and, optionally, the context slug.
// When Context is omitted the server derives it (BR-AC26) — the Admin UI sends
// the derived value explicitly so the operator can see and adjust it before
// committing to something immutable.
type createBURequest struct {
	Name    string `json:"name"`
	Context string `json:"context"`
}

// @Summary      Create a business unit
// @Description  Registers a new business unit under an account (Phase 22, BR-AC16), deriving its context slug from the name when one isn't supplied, and best-effort registers it in refdata-service.
// @Tags         business-units
// @Accept       json
// @Produce      json
// @Param        name  path      string            true  "Account name"
// @Param        body  body      createBURequest   true  "Business unit name and optional context slug"
// @Success      201   {object}  businessUnitResponse
// @Failure      400   {object}  errorResponse  "Missing name or invalid context slug"
// @Failure      404   {object}  errorResponse  "Account not found"
// @Failure      409   {object}  errorResponse  "Duplicate name or context"
// @Failure      500   {object}  errorResponse
// @Router       /api/accounts/{name}/business-units [post]
func (h *Handlers) createBusinessUnit(w http.ResponseWriter, r *http.Request) {
	accountName := r.PathValue("name")
	acc, err := h.Store.Get(r.Context(), accountName)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "account not found")
		return
	} else if err != nil {
		h.Log.Error("get account for BU create", "name", accountName, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var in createBURequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	in.Name = strings.TrimSpace(in.Name)

	slug := in.Context
	if slug == "" {
		slug = DeriveContext(accountName, in.Name)
	}
	// BR-AC27: validated here, at the point of write, rather than left to the
	// best-effort call into refdata-service below — a slug that fails there
	// fails silently, leaving a business unit that can never resolve.
	if err := ValidateContext(slug); err != nil {
		writeError(w, http.StatusBadRequest,
			"context must be lowercase letters, digits and hyphens, start and end alphanumeric, and be at most "+
				strconv.Itoa(MaxContextLen)+" characters")
		return
	}

	if err := h.Store.InsertBusinessUnit(r.Context(), NewBusinessUnit{
		AccountID: acc.ID,
		Name:      in.Name,
		Context:   slug,
		Visible:   true,
	}); err != nil {
		if errors.Is(err, ErrBUDuplicate) {
			writeError(w, http.StatusConflict, "a business unit with that name or context already exists")
			return
		}
		h.Log.Error("insert business unit", "account", accountName, "context", slug, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := h.Refdata().RegisterContext(r.Context(), ContextRegistration{
		Context: slug,
		Parent:  DefaultBUTemplateContext,
		Name:    in.Name,
		Tenant:  accountName,
	}); err != nil {
		h.Log.Warn("register BU context in refdata", "account", accountName, "context", slug, "err", err)
	}

	bu, err := h.Store.GetBusinessUnit(r.Context(), accountName, slug)
	if err != nil {
		h.Log.Error("reload BU after insert", "account", accountName, "context", slug, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toBUResponse(bu))
}

// updateBURequest carries the two mutable fields. Both are pointers so the
// handler can tell "not supplied" from "set to the zero value" — without that,
// a rename-only request would read as a request to hide the unit.
//
// There is deliberately no Context field: the slug has no rename path anywhere
// (BR-AC26).
type updateBURequest struct {
	Visible *bool   `json:"visible"`
	Name    *string `json:"name"`
}

// @Summary      Update a business unit
// @Description  Renames and/or toggles visibility of one business unit (Phase 22, BR-AC17). The default business unit can never be renamed. No response body on success.
// @Tags         business-units
// @Accept       json
// @Param        name       path  string           true  "Account name"
// @Param        buContext  path  string           true  "Business unit context slug"
// @Param        body       body  updateBURequest  true  "Fields to update — visible, name, or both"
// @Success      204  "No content"
// @Failure      400  {object}  errorResponse  "Invalid body or nothing to update"
// @Failure      404  {object}  errorResponse  "Business unit not found"
// @Failure      409  {object}  errorResponse  "Renaming the default business unit, or duplicate name"
// @Failure      500  {object}  errorResponse
// @Router       /api/accounts/{name}/business-units/{buContext} [patch]
func (h *Handlers) updateBusinessUnit(w http.ResponseWriter, r *http.Request) {
	accountName := r.PathValue("name")
	buContext := r.PathValue("buContext")

	var in updateBURequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Visible == nil && in.Name == nil {
		writeError(w, http.StatusBadRequest, "nothing to update — supply visible, name, or both")
		return
	}

	bu, err := h.Store.GetBusinessUnit(r.Context(), accountName, buContext)
	if errors.Is(err, ErrBUNotFound) {
		writeError(w, http.StatusNotFound, "business unit not found")
		return
	} else if err != nil {
		h.Log.Error("get BU for update", "account", accountName, "context", buContext, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// BR-AC28: the default's identity is fixed. Visibility stays toggleable —
	// BR-AC17's hide-once-a-real-BU-exists flow depends on exactly that.
	if in.Name != nil && bu.IsDefault {
		writeError(w, http.StatusConflict, "the default business unit cannot be renamed")
		return
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name may not be empty")
			return
		}
		if err := h.Store.RenameBusinessUnit(r.Context(), accountName, buContext, name); err != nil {
			if errors.Is(err, ErrBUDuplicate) {
				writeError(w, http.StatusConflict, "a business unit with that name already exists")
				return
			}
			h.Log.Error("rename BU", "account", accountName, "context", buContext, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// refdata-service's Register upserts, so re-registering the same slug
		// with the new display name is its rename path.
		if err := h.Refdata().RegisterContext(r.Context(), ContextRegistration{
			Context: buContext,
			Parent:  DefaultBUTemplateContext,
			Name:    name,
			Tenant:  accountName,
		}); err != nil {
			h.Log.Warn("rename BU context in refdata", "context", buContext, "err", err)
		}
	}

	if in.Visible != nil {
		if err := h.Store.SetBusinessUnitVisible(r.Context(), accountName, buContext, *in.Visible); err != nil {
			if errors.Is(err, ErrBUNotFound) {
				writeError(w, http.StatusNotFound, "business unit not found")
				return
			}
			h.Log.Error("set BU visible", "account", accountName, "context", buContext, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := h.Refdata().SetContextVisible(r.Context(), buContext, *in.Visible); err != nil {
			h.Log.Warn("set context visible in refdata", "context", buContext, "visible", *in.Visible, "err", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
