package rest

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// accountsBasicAuthUser must match accounts-service's own accounts.BasicAuthUser
// (accounts/middleware.go) — that service's entire /api/accounts/* surface is
// gated by HTTP Basic Auth with this fixed username and a shared secret, the
// same "spike-only shared secret" precedent as its own ACCOUNTS_AUTH_SECRET.
// Not imported directly: cross-module import would couple this service's
// build to accounts-service's go.mod for one string constant.
const accountsBasicAuthUser = "admin"

// AccountsClient resolves a NATS account public key to its friendly tenant
// name, replacing shipping-service's original tenantLabelsByAccount
// (dictionary/internal/rest/nats_ops.go) for the Connections/AccountActivity
// panels. That function matched /connz rows against the LocalAddr of
// connections shipping-service itself held — one per tenant, via
// TenantResources — which only worked because shipping-service was the one
// service with that fan-out. observability-service holds exactly one
// connection (PLATFORM), so there is nothing to match against; instead it
// asks accounts-service directly, which already has the authoritative
// name<->publicKey mapping in its own Postgres store and already serves it
// over GET /api/accounts (used by the Admin UI's Accounts panel). Mirrors
// accounts-service's own RefdataClient (accounts/refdata.go) — a small typed
// client for one cross-service HTTP call, not a shared library.
type AccountsClient struct {
	// BaseURL is accounts-service's own address (e.g.
	// http://accounts-service:8080). Labels returns nil unconditionally
	// when empty — this dependency is optional, and a caller must degrade
	// to "no labels" rather than fail the whole diagnostic response, the
	// same way a failed /varz probe never costs Connections its row list.
	BaseURL string
	// AuthSecret is accounts-service's ACCOUNTS_AUTH_SECRET — every
	// /api/accounts/* route is behind Basic Auth with accountsBasicAuthUser.
	AuthSecret string
	// HTTP is optional; http.DefaultClient is used when nil.
	HTTP *http.Client
	Log  *slog.Logger
}

// AccountsClientAccount is the subset of accounts-service's accountResponse
// (accounts/handler.go) this client actually reads.
type AccountsClientAccount struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
	Status    string `json:"status"` // accounts.StatusActive ("active") | accounts.StatusSuspended ("suspended")
}

// reservedAccountNames mirrors accounts-service's own
// reservedAccountNames (accounts/handler.go, BR-AC06) — used here to filter
// PLATFORM and SYS out of TenantNames. Phase 30i's live verification caught
// two wrong assumptions this originally encoded: accounts-service stores
// (and returns from GET /api/accounts) these rows with lowercase names
// ("platform", "sys" — confirmed against h.Store.Get(ctx, "platform") in
// accounts/handler.go), not the uppercase "PLATFORM" this file compared
// against; and SYS *does* get a Postgres row like every other account, it
// isn't naturally absent from the list the way the original comment
// assumed. A live account with an un-excluded name here means
// introspectableAccounts (kv.go) builds a bogus
// monitor.{platform,sys}.js-prefixed JetStream context that has no
// matching cross-account import — listKVBuckets/listStreams then abort
// with "no responders" on whichever of the two is enumerated first,
// discarding every account's results that would otherwise have succeeded
// (BR-AC32 only imports real tenant accounts' $JS.API subjects, never
// PLATFORM's or SYS's own). accounts-service's own reservation check
// (handler.go) already compares case-insensitively via
// strings.ToUpper(in.Name) against this same map — matched here for the
// same reason.
var reservedAccountNames = map[string]bool{"PLATFORM": true, "SYS": true}

// list fetches every account from accounts-service. Best-effort and
// nil-safe (works on a nil receiver, same convention natstrace.Span's
// methods use): an unreachable or erroring accounts-service, or an
// unconfigured BaseURL, returns nil rather than an error — every caller
// (Labels, TenantNames) must degrade to "nothing resolved", never fail the
// diagnostic response it's supporting, matching /varz's role as a
// secondary read for Connections.
func (c *AccountsClient) list(ctx context.Context) []AccountsClientAccount {
	if c == nil || c.BaseURL == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/accounts", nil)
	if err != nil {
		return nil
	}
	req.SetBasicAuth(accountsBasicAuthUser, c.AuthSecret)

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if c.Log != nil {
			c.Log.Warn("accounts list probe", "err", err)
		}
		return nil
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		if c.Log != nil {
			c.Log.Warn("accounts list probe", "status", resp.StatusCode)
		}
		return nil
	}

	var accs []AccountsClientAccount
	if err := json.NewDecoder(resp.Body).Decode(&accs); err != nil {
		if c.Log != nil {
			c.Log.Warn("accounts list decode", "err", err)
		}
		return nil
	}
	return accs
}

// Labels returns a publicKey -> name map for tagging /connz and /accstatz
// rows with a friendly tenant label (Phase 30d).
func (c *AccountsClient) Labels(ctx context.Context) map[string]string {
	accs := c.list(ctx)
	if accs == nil {
		return nil
	}
	out := make(map[string]string, len(accs))
	for _, a := range accs {
		out[a.PublicKey] = a.Name
	}
	return out
}

// TenantNames returns every tenant account's name (PLATFORM and SYS
// excluded) — Phase 30e's introspectableAccounts uses this to enumerate
// which monitor.{tenant}.js.> JetStream API prefixes to query, replacing
// shipping-service's original TenantResources map iteration.
func (c *AccountsClient) TenantNames(ctx context.Context) []string {
	accs := c.list(ctx)
	names := make([]string, 0, len(accs))
	for _, a := range accs {
		if reservedAccountNames[strings.ToUpper(a.Name)] {
			continue
		}
		names = append(names, a.Name)
	}
	return names
}

// TenantStatuses returns every tenant account's lifecycle status
// (accounts.StatusActive/StatusSuspended, PLATFORM and SYS excluded), keyed
// by name — introspectableAccounts (kv.go) uses this to tag each account
// group the Streams/KV Buckets panels report with its real active/suspended
// state, replacing an earlier client-side-only "is this the browser's
// currently connected tenant" indicator that had nothing to do with the
// account's own status.
func (c *AccountsClient) TenantStatuses(ctx context.Context) map[string]string {
	accs := c.list(ctx)
	statuses := make(map[string]string, len(accs))
	for _, a := range accs {
		if reservedAccountNames[strings.ToUpper(a.Name)] {
			continue
		}
		statuses[a.Name] = a.Status
	}
	return statuses
}
