package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// opString renders a jetstream.KeyValueOp for the wire (kvChange.Op /
// the old watchEvent.Op) — PUT/DEL/PURGE, matching NATS KV's own three
// operation kinds.
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

// kvBucket is one registered KV bucket's run-time status — the bucket rail's
// row shape. Values counts every message including historical revisions;
// ttlSeconds is 0 when keys never expire. Account is the NATS account this
// bucket lives in (a known tenant name, or "platform") — bucket names are
// only unique WITHIN an account (every tenant provisions its own
// dict-a/container/meta), so this is what lets the rail group buckets
// correctly instead of colliding same-named rows from different accounts.
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
	Buckets []kvBucket `json:"buckets"`
}

// listKVBuckets godoc
//
// @Summary      List KV buckets
// @Description  Every Key-Value bucket registered across every NATS account this backend holds a connection to — every known tenant (dict-a, dict-b, container, meta) plus the PLATFORM account (refdata-service's versioned caches) — each tagged with its account. Deliberately NOT scoped to the topbar's currently-selected tenant: a tenant switch reconnects Deps.JS for command/query traffic, but this is a read-only cross-account diagnostic view.
// @Tags         kv
// @Produce      json
// @Success      200  {object}  kvBucketsResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/kv/buckets [get]
func (h *Handlers) listKVBuckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	deps := h.deps()

	buckets := []kvBucket{}
	for _, acct := range h.introspectableAccounts() {
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
			deps.Log.Error("list kv buckets", "account", acct.name, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	// Stable order (account, then bucket name) so the rail's groups and rows
	// don't reshuffle between polls.
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Account != buckets[j].Account {
			return buckets[i].Account < buckets[j].Account
		}
		return buckets[i].Bucket < buckets[j].Bucket
	})
	writeJSON(w, http.StatusOK, kvBucketsResponse{Buckets: buckets})
}

// platformAccount is the name the cross-account introspection endpoints use
// for the PLATFORM account in place of a tenant name. PLATFORM has no tenant
// identity of its own, and context values starting with "_" are reserved
// (BR-D33/BR-AC07), so this can never collide with a real tenant.
const platformAccount = "platform"

// introspectAccount pairs one NATS account this backend holds a connection to
// with the JetStream context that can read it.
type introspectAccount struct {
	name string
	js   jetstream.JetStream
}

// introspectableAccounts lists every NATS account the read-only cross-account
// introspection endpoints report — listKVBuckets and listStreams both walk
// this: every known tenant (each one's persistent bundle in TenantResources)
// plus PLATFORM.
//
// Deliberately NOT scoped to Deps.Tenant/Deps.JS: those mirror whichever
// single tenant is currently active for command/query traffic, which is the
// wrong axis entirely for a diagnostic view whose whole job is showing what
// exists across the deployment.
//
// PLATFORM comes from PlatformFullJS (not PlatformJS — see its Deps doc
// comment) because listing streams needs $JS.API.STREAM.LIST, which
// shipping-admin is deliberately denied. It's nil when unconfigured (local dev
// outside Docker) or the connection failed at Startup, in which case PLATFORM
// is skipped entirely rather than erroring.
func (h *Handlers) introspectableAccounts() []introspectAccount {
	deps := h.deps()
	accounts := make([]introspectAccount, 0, len(deps.TenantResources)+1)
	for name, res := range deps.TenantResources {
		// Nil-guarded the same way tenantLabelsByAccount guards this map: a
		// diagnostic endpoint should degrade to "that account isn't listed"
		// rather than panic on a half-built bundle.
		if res == nil || res.js == nil {
			continue
		}
		accounts = append(accounts, introspectAccount{name: name, js: res.js})
	}
	if deps.PlatformFullJS != nil {
		accounts = append(accounts, introspectAccount{name: platformAccount, js: deps.PlatformFullJS})
	}
	return accounts
}

// jsForAccount resolves the JetStream context for one of the accounts
// introspectableAccounts reports — either a known tenant (its persistent
// bundle in TenantResources) or "platform" (the unrestricted PLATFORM-account
// connection, Deps.PlatformFullJS — not Deps.PlatformJS, see its doc
// comment). Bucket and stream names both collide across accounts (every
// tenant provisions its own dict-a/container/meta and its own SHIPPING), so
// any endpoint keyed by a bare bucket or stream name needs this to know which
// account's store to open — unlike h.deps().JS, this is never the "currently
// active tenant" mirror field.
func (h *Handlers) jsForAccount(account string) (jetstream.JetStream, bool) {
	deps := h.deps()
	if account == platformAccount {
		return deps.PlatformFullJS, deps.PlatformFullJS != nil
	}
	res, ok := deps.TenantResources[account]
	if !ok {
		return nil, false
	}
	return res.js, true
}

// kvChange is one KV entry as returned by kvBucketEntriesOnce's bootstrap
// snapshot (Phase 23) — live changes after this snapshot arrive over
// notify.{context}.kv.{bucket}.{key}.changed instead, which carries just the
// raw value (see internal/kvstore.Store.EnableNotify), not this envelope.
type kvChange struct {
	Key      string          `json:"key"`
	Op       string          `json:"op"` // PUT, DEL, PURGE
	Revision uint64          `json:"revision"`
	Created  time.Time       `json:"created,omitempty"`
	Value    json.RawMessage `json:"value,omitempty" swaggertype:"object"`
}

// kvBucketEntriesOnce godoc
//
// @Summary      One-shot KV bucket entries (Phase 23)
// @Description  Returns every current entry in one KV bucket as a single JSON array, snapshotted at request time — the bootstrap half of watchKVBucket's old SSE stream (Live=false entries only). Live changes after this snapshot arrive via notify.{context}.kv.{bucket}.{key}.changed (internal/kvstore.Store.EnableNotify) instead of holding a connection open.
// @Tags         kv
// @Produce      json
// @Param        account  path  string  true  "NATS account (a known tenant name, or \"platform\")"
// @Param        bucket   path  string  true  "KV bucket name (e.g. dict-a, dict-b, container, meta)"
// @Success      200  {array}   kvChange
// @Failure      400  {object}  errorResponse  "Unknown account or bucket"
// @Failure      500  {object}  errorResponse
// @Router       /api/kv/buckets/{account}/{bucket}/entries [get]
func (h *Handlers) kvBucketEntriesOnce(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	js, ok := h.jsForAccount(r.PathValue("account"))
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
		h.deps().Log.Error("open kv bucket", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		h.deps().Log.Error("kv watch all", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = watcher.Stop() }()

	entries := []kvChange{}
	for entry := range watcher.Updates() {
		if entry == nil {
			break // WatchAll's INIT_DONE marker — snapshot complete
		}
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
	}

	writeJSON(w, http.StatusOK, entries)
}
