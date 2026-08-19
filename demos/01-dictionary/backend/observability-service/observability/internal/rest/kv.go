package rest

// KV Buckets panel — lifted from shipping-service's
// dictionary/internal/rest/kv.go (Phase 30e). introspectableAccounts and
// jsForAccount are rewritten: the originals iterated shipping-service's own
// TenantResources map (one live connection per tenant) plus a second,
// broader-access PlatformFullJS connection. This service holds exactly one
// connection; cross-account reach instead comes from constructing a
// jetstream.JetStream per account with BR-AC32's monitor.{tenant}.js
// API-prefix remap (jetstream.NewWithAPIPrefix) — the standard client
// library transparently honors a custom prefix for every operation these
// handlers need, including the legacy KV-watch path (jetstream/kv.go's
// legacyJetStream() propagates it too), so no hand-rolled wire protocol is
// needed. Tenant names come from accountsClient.TenantNames instead of a
// map iteration.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// platformAccount is the name the cross-account introspection endpoints use
// for the PLATFORM account in place of a tenant name — matches
// shipping-service's original convention. Context values starting with "_"
// are reserved (BR-D33/BR-AC07) so this can never collide with a real
// tenant.
const platformAccount = "platform"

// jsAPIPrefixTmpl mirrors accounts/provisioner.go's jsAPILocalSubjectTmpl
// minus the trailing subject suffix — jetstream.NewWithAPIPrefix appends
// its own "." separator, so this is just "monitor.{tenant}.js", matching
// BR-AC32's local remap exactly.
const jsAPIPrefixTmpl = "monitor.%s.js"

// platformAccountStatus is the status introspectableAccounts reports for
// PLATFORM — it never goes through accounts-service's suspend/reactivate
// lifecycle (BR-AC accounts.md), so it is always "active" rather than
// resolved from a lookup the way tenant accounts' status is.
const platformAccountStatus = "active"

// introspectAccount pairs one NATS account this backend can introspect with
// the JetStream context that reads it and its accounts-service lifecycle
// status (active/suspended) — Phase 30's Streams/KV Buckets panels tag each
// account group with this so the UI can reflect the account's real state
// rather than an unrelated "is this the browser's connected tenant" signal.
type introspectAccount struct {
	name   string
	status string
	js     jetstream.JetStream
}

// introspectableAccounts lists every account listKVBuckets/listStreams
// report: PLATFORM (this service's own connection, native $JS.API prefix)
// plus every tenant accounts-service knows about, each via its
// BR-AC32-remapped prefix. Deliberately NOT scoped to any "currently
// active" tenant — there is no such concept here, unlike shipping-service's
// per-session tenant switch — this is a cross-account diagnostic view whose
// whole job is showing what exists across the deployment.
func (h *Handlers) introspectableAccounts(ctx context.Context) []introspectAccount {
	accounts := []introspectAccount{}
	if h.deps.NC != nil {
		if js, err := jetstream.New(h.deps.NC); err == nil {
			accounts = append(accounts, introspectAccount{name: platformAccount, status: platformAccountStatus, js: js})
		}
	}
	statuses := h.deps.Accounts.TenantStatuses(ctx)
	for _, name := range h.deps.Accounts.TenantNames(ctx) {
		js, err := jetstream.NewWithAPIPrefix(h.deps.NC, fmt.Sprintf(jsAPIPrefixTmpl, name))
		if err != nil {
			continue
		}
		accounts = append(accounts, introspectAccount{name: name, status: statuses[name], js: js})
	}
	return accounts
}

// jsForAccount resolves the JetStream context for one named account (a
// {account} path/query param) — either "platform" or a tenant name
// accounts-service currently recognizes. Bucket and stream names both
// collide across accounts (every tenant provisions its own ships/
// container/meta and its own SHIPPING), so any endpoint keyed by a bare
// bucket or stream name needs this to know which account's store to open.
func (h *Handlers) jsForAccount(ctx context.Context, account string) (jetstream.JetStream, bool) {
	if account == platformAccount {
		if h.deps.NC == nil {
			return nil, false
		}
		js, err := jetstream.New(h.deps.NC)
		return js, err == nil
	}
	for _, name := range h.deps.Accounts.TenantNames(ctx) {
		if name != account {
			continue
		}
		js, err := jetstream.NewWithAPIPrefix(h.deps.NC, fmt.Sprintf(jsAPIPrefixTmpl, account))
		return js, err == nil
	}
	return nil, false
}

// ─── KV Buckets ─────────────────────────────────────────────────────────────

type kvBucket struct {
	Bucket       string `json:"bucket"`
	Account      string `json:"account"`
	Values       uint64 `json:"values"`
	History      int64  `json:"history"`
	Bytes        uint64 `json:"bytes"`
	TTLSeconds   int64  `json:"ttlSeconds"`
	BackingStore string `json:"backingStore"`
}

type kvBucketsResponse struct {
	Accounts []accountStatusEntry `json:"accounts"`
	Buckets  []kvBucket           `json:"buckets"`
}

// listKVBuckets godoc
//
// @Summary      List KV buckets
// @Description  Every Key-Value bucket registered across every NATS account this backend can introspect — PLATFORM plus every tenant accounts-service currently knows about — each tagged with its account. Accounts is the authoritative account list (every account, including ones whose buckets couldn't be listed, e.g. a suspended tenant) — Buckets may have zero rows for an account present in Accounts.
// @Tags         kv
// @Produce      json
// @Success      200  {object}  kvBucketsResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/kv/buckets [get]
func (h *Handlers) listKVBuckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	deps := h.deps

	accounts := []accountStatusEntry{}
	buckets := []kvBucket{}
	for _, acct := range h.introspectableAccounts(ctx) {
		accounts = append(accounts, accountStatusEntry{Name: acct.name, Status: acct.status})

		lister := acct.js.KeyValueStores(ctx)
		for status := range lister.Status() {
			buckets = append(buckets, kvBucket{
				Bucket:       status.Bucket(),
				Account:      acct.name,
				Values:       status.Values(),
				History:      status.History(),
				Bytes:        status.Bytes(),
				TTLSeconds:   int64(status.TTL() / time.Second),
				BackingStore: status.BackingStore(),
			})
		}
		if err := lister.Error(); err != nil {
			// See listStreams' matching comment (streams.go): a suspended
			// tenant's cross-account $JS.API access reliably fails "no
			// responders" and must not blank out every other account's
			// already-gathered buckets.
			deps.Log.Warn("list kv buckets: account unreachable, skipping", "account", acct.name, "err", err)
			continue
		}
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Account != buckets[j].Account {
			return buckets[i].Account < buckets[j].Account
		}
		return buckets[i].Bucket < buckets[j].Bucket
	})
	writeJSON(w, http.StatusOK, kvBucketsResponse{Accounts: accounts, Buckets: buckets})
}

// kvChange is one KV entry as returned by kvBucketEntriesOnce's bootstrap
// snapshot.
type kvChange struct {
	Key      string          `json:"key"`
	Op       string          `json:"op"` // PUT, DEL, PURGE
	Revision uint64          `json:"revision"`
	Created  time.Time       `json:"created,omitempty"`
	Value    json.RawMessage `json:"value,omitempty" swaggertype:"object"`
}

// opString renders a jetstream.KeyValueOp for the wire.
func opString(op jetstream.KeyValueOp) string {
	switch op {
	case jetstream.KeyValueDelete:
		return "DEL"
	case jetstream.KeyValuePurge:
		return "PURGE"
	default:
		return "PUT"
	}
}

// kvBucketEntriesOnce godoc
//
// @Summary      One-shot KV bucket entries
// @Description  Returns every current entry in one KV bucket as a single JSON array, snapshotted at request time.
// @Tags         kv
// @Produce      json
// @Param        account  path  string  true  "NATS account (a known tenant name, or \"platform\")"
// @Param        bucket   path  string  true  "KV bucket name (e.g. ships, container, meta)"
// @Success      200  {array}   kvChange
// @Failure      400  {object}  errorResponse  "Unknown account or bucket"
// @Failure      500  {object}  errorResponse
// @Router       /api/kv/buckets/{account}/{bucket}/entries [get]
func (h *Handlers) kvBucketEntriesOnce(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	js, ok := h.jsForAccount(ctx, r.PathValue("account"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown account: "+r.PathValue("account"))
		return
	}

	kv, err := js.KeyValue(ctx, r.PathValue("bucket"))
	if err != nil {
		if errors.Is(err, jetstream.ErrBucketNotFound) {
			writeError(w, http.StatusBadRequest, "unknown bucket: "+r.PathValue("bucket"))
			return
		}
		h.deps.Log.Error("open kv bucket", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		h.deps.Log.Error("kv watch all", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = watcher.Stop() }()

	// Cross-account KV watch via monitor.{account}.js delivers individual entries
	// but the INIT_DONE nil never arrives — the "no more messages" 404 status is
	// not routed back through the export chain. Idle timer is the fallback: if no
	// new entry arrives for snapshotIdleTimeout after the last one, the snapshot is
	// assumed complete.
	const snapshotIdleTimeout = 750 * time.Millisecond
	idle := time.NewTimer(snapshotIdleTimeout)
	defer idle.Stop()

	entries := []kvChange{}
outer:
	for {
		select {
		case entry, ok := <-watcher.Updates():
			if !ok || entry == nil {
				break outer
			}
			if !idle.Stop() {
				select { case <-idle.C: default: }
			}
			idle.Reset(snapshotIdleTimeout)
			change := kvChange{
				Key:      entry.Key(),
				Op:       opString(entry.Operation()),
				Revision: entry.Revision(),
				Created:  entry.Created(),
			}
			if entry.Operation() == jetstream.KeyValuePut {
				change.Value = entry.Value()
			}
			entries = append(entries, change)
		case <-idle.C:
			break outer
		case <-ctx.Done():
			break outer
		}
	}

	writeJSON(w, http.StatusOK, entries)
}
