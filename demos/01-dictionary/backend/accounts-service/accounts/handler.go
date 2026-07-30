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
package accounts

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
}

func NewHandlers(store *Store, provisioner *Provisioner, credsDir string, log *slog.Logger) *Handlers {
	return &Handlers{Store: store, Provisioner: provisioner, CredsDir: credsDir, Log: log}
}

func (h *Handlers) Mount(mux *http.ServeMux, authSecret string) {
	mux.Handle("POST /api/accounts", BasicAuth(authSecret, http.HandlerFunc(h.createAccount)))
	mux.Handle("GET /api/accounts", BasicAuth(authSecret, http.HandlerFunc(h.listAccounts)))
	mux.Handle("GET /api/accounts/{name}", BasicAuth(authSecret, http.HandlerFunc(h.getAccount)))
	mux.Handle("POST /api/accounts/{name}/suspend", BasicAuth(authSecret, http.HandlerFunc(h.suspendAccount)))
	mux.Handle("POST /api/accounts/{name}/reactivate", BasicAuth(authSecret, http.HandlerFunc(h.reactivateAccount)))
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

	minted, err := h.Provisioner.CreateAccount(r.Context(), limits)
	if err != nil {
		h.Log.Error("mint account", "name", in.Name, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to mint account")
		return
	}

	credsBytes, err := h.Provisioner.CreateUser(minted.PublicKey, minted.SigningKeySeed, in.Name)
	if err != nil {
		h.Log.Error("mint user", "name", in.Name, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to mint user")
		return
	}

	if h.CredsDir != "" {
		credsPath := filepath.Join(h.CredsDir, in.Name+".creds")
		if err := os.WriteFile(credsPath, credsBytes, 0o600); err != nil {
			h.Log.Error("write creds file", "path", credsPath, "err", err)
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
		writeError(w, http.StatusInternalServerError, "account minted but failed to persist — resolver and Postgres are now inconsistent")
		return
	}

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

	if err := h.Provisioner.DeleteAccount(r.Context(), acc.PublicKey); err != nil {
		h.Log.Error("revoke account", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to revoke account")
		return
	}
	if err := h.Store.SetStatus(r.Context(), name, StatusSuspended); err != nil {
		h.Log.Error("mark account suspended", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "account revoked but failed to update status")
		return
	}

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

	limits := JSLimits{MaxMem: acc.JSMaxMem, MaxFile: acc.JSMaxFile, MaxStreams: acc.JSMaxStreams, MaxConsumers: acc.JSMaxConsumers}
	if err := h.Provisioner.ReactivateAccount(r.Context(), acc.PublicKey, signingKeySeed, limits); err != nil {
		h.Log.Error("reactivate account", "name", name, "err", err)
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
			writeError(w, http.StatusInternalServerError, "account reactivated but failed to persist its new signing key")
			return
		}
	}

	credsBytes, err := h.Provisioner.CreateUser(acc.PublicKey, signingKeySeed, acc.Name)
	if err != nil {
		h.Log.Error("mint user after reactivate", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "account reactivated but failed to mint new creds")
		return
	}
	if h.CredsDir != "" {
		credsPath := filepath.Join(h.CredsDir, acc.Name+".creds")
		if err := os.WriteFile(credsPath, credsBytes, 0o600); err != nil {
			h.Log.Error("write creds file", "path", credsPath, "err", err)
			writeError(w, http.StatusInternalServerError, "account reactivated but failed to write creds file")
			return
		}
	}

	if err := h.Store.SetStatus(r.Context(), name, StatusActive); err != nil {
		h.Log.Error("mark account active", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "account reactivated but failed to update status")
		return
	}

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
