// Hybrid KV materialization for corpus versions (§5, Phase 12.5): eager
// materialization into a per-version bucket on publish/rollback, TTL for
// superseded versions, and rewrite-on-read to keep a pinned consumer's
// version warm. Deliberately separate from the plain Q5 cache above — that
// protocol serves the unversioned working-table state; this one serves
// immutable, versioned corpus snapshots, and old/new versions coexist
// indefinitely by design (version pinning).
package kvcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
	"github.com/nats-io/nats.go/jetstream"
)

// SupersededVersionTTL is how long a corpus version's KV bucket survives
// once it is no longer the context's latest published version, absent any
// read that pins/refreshes it. 30 days is a POC-scale default, not a
// modeled retention decision — see the design doc's open question on KV
// bucket cleanup (deferred to the pin registry).
const SupersededVersionTTL = 30 * 24 * time.Hour

// RetainedSupersededVersions is how many superseded versions of a context
// keep their KV bucket at all. Anything older is discarded outright on the
// next publish/rollback — see VersionMaterializer.Discard.
//
// This exists because SupersededVersionTTL alone does not bound anything
// that matters. A KV bucket is a JetStream stream, and TTL expires *keys*,
// never the bucket — so before this, every publish permanently consumed one
// stream slot from the account's MaxStreams, and the count only ever went
// up. On this lab's platform account (MaxStreams 20) that took a couple of
// hours of ordinary restarts to exhaust, after which every subsequent
// publish failed with err_code=10027 and left a version marked `published`
// in Postgres with nothing behind it in KV.
//
// 2 is a POC-scale default chosen to leave an obvious rollback target plus
// one, not a modeled retention decision — the same caveat SupersededVersionTTL
// carries. It bounds a context at RetainedSupersededVersions+1 streams.
const RetainedSupersededVersions = 2

// VersionedEntry is one materialized item within a specific published
// corpus version's KV bucket (refdata-{context}-v{N}). Like Entry,
// localizations use the slim LocalizationValue and the item uses CacheItem
// — no label/description on the item; BR-D30 guarantees the default
// locale's localization is present in Localizations whenever any exist.
type VersionedEntry struct {
	Item          CacheItem                           `json:"item"`
	Localizations map[string]domain.LocalizationValue `json:"localizations,omitempty"`
	SourceContext string                              `json:"sourceContext"`
	IsOverride    bool                                `json:"isOverride"`
	Version       int                                 `json:"version"`
}

// VersionedMeta is the "_meta" key inside a version's own bucket.
type VersionedMeta struct {
	Version   int `json:"version"`
	ItemCount int `json:"itemCount"`
}

// VersionMaterializer writes a published corpus version's full flattened
// content into its own versioned KV bucket, and manages that bucket's TTL
// as the version transitions between active and superseded.
type VersionMaterializer struct {
	kv         *kvstore.Store
	namespaces *TypeNamespaces
}

func NewVersionMaterializer(kv *kvstore.Store, namespaces *TypeNamespaces) *VersionMaterializer {
	return &VersionMaterializer{kv: kv, namespaces: namespaces}
}

// Materialize eagerly writes every item (with its localizations folded in)
// into the refdata-{contextKey}-v{version} bucket, plus a "_meta" summary.
// ttl is 0 for the version currently being made active, and
// SupersededVersionTTL for a version being marked superseded.
func (m *VersionMaterializer) Materialize(ctx context.Context, contextKey string, version int, items []domain.CorpusItem, locs []domain.CorpusLocalization, ttl time.Duration) error {
	byItem := map[string]map[string]domain.LocalizationValue{}
	for _, loc := range locs {
		key := loc.TypeKey + "\x00" + loc.Code
		if byItem[key] == nil {
			byItem[key] = map[string]domain.LocalizationValue{}
		}
		byItem[key][loc.Locale] = loc.Localization.ToValue()
	}

	bucket, err := m.kv.VersionedBucket(ctx, contextKey, version, ttl)
	if err != nil {
		return err
	}
	for _, item := range items {
		ci := CacheItem{
			TypeKey: item.TypeKey, Code: item.Code, Context: item.Context,
			Status: item.Status,
		}
		entry := VersionedEntry{
			Item:          ci,
			Localizations: byItem[item.TypeKey+"\x00"+item.Code],
			SourceContext: item.SourceContext,
			IsOverride:    item.IsOverride,
			Version:       version,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		namespace, err := m.namespaces.For(ctx, item.TypeKey)
		if err != nil {
			return err
		}
		if _, err := bucket.Put(ctx, ItemKey(namespace, item.TypeKey, item.Code), data); err != nil {
			return err
		}
	}
	meta, err := json.Marshal(VersionedMeta{Version: version, ItemCount: len(items)})
	if err != nil {
		return err
	}
	_, err = bucket.Put(ctx, "_meta", meta)
	return err
}

// Supersede lowers a no-longer-active version's bucket to the superseded
// TTL. CreateOrUpdateKeyValue re-applying a bucket config does not touch
// existing keys' content, only the bucket's own TTL setting for keys
// written from this point on — a version that already has no pending reads
// still ages out eventually via the bucket's own default TTL semantics.
func (m *VersionMaterializer) Supersede(ctx context.Context, contextKey string, version int) error {
	_, err := m.kv.VersionedBucket(ctx, contextKey, version, SupersededVersionTTL)
	return err
}

// Discard deletes a superseded version's bucket entirely, giving its stream
// slot back. Called for versions that have fallen outside
// RetainedSupersededVersions.
//
// The cost is that a consumer pinned to a discarded version now gets
// ErrVersionedKeyNotFound rather than a stale-but-readable answer. That is
// the deliberate trade: an unbounded bucket count does not merely leak, it
// eventually takes the *whole account* down — a publish that cannot create
// its bucket fails for every context, not just old ones. Postgres still
// holds every version's corpus rows, so a discarded version is
// re-materializable (ItemsAtVersion + Materialize) if the pin registry this
// was deferred to ever lands.
func (m *VersionMaterializer) Discard(ctx context.Context, contextKey string, version int) error {
	return m.kv.DeleteVersionedBucket(ctx, contextKey, version)
}

// VersionNotifier implements domain.CorpusNotifier: on every publish or
// rollback it materializes the newly-active version and marks every other
// non-draft version for that context as superseded.
type VersionNotifier struct {
	materializer *VersionMaterializer
	corpus       domain.CorpusRepository
}

func NewVersionNotifier(kv *kvstore.Store, corpus domain.CorpusRepository, namespaces *TypeNamespaces) *VersionNotifier {
	return &VersionNotifier{materializer: NewVersionMaterializer(kv, namespaces), corpus: corpus}
}

var _ domain.CorpusNotifier = (*VersionNotifier)(nil)

func (n *VersionNotifier) NotifyPublished(ctx context.Context, contextKey string, version int) error {
	return n.materializeAndSupersede(ctx, contextKey, version)
}

func (n *VersionNotifier) NotifyRolledBack(ctx context.Context, contextKey string, version int) error {
	return n.materializeAndSupersede(ctx, contextKey, version)
}

func (n *VersionNotifier) materializeAndSupersede(ctx context.Context, contextKey string, version int) error {
	items, err := n.corpus.ItemsAtVersion(ctx, contextKey, version)
	if err != nil {
		return err
	}
	locs, err := n.corpus.LocalizationsAtVersion(ctx, contextKey, version)
	if err != nil {
		return err
	}
	if err := n.materializer.Materialize(ctx, contextKey, version, items, locs, 0); err != nil {
		return err
	}

	versions, err := n.corpus.Versions(ctx, contextKey)
	if err != nil {
		return err
	}
	// Newest first, so "the N most recent superseded versions" is a prefix.
	// Versions is documented as ordering DESC, but the port is an interface
	// and retention correctness should not rest on a repository's ORDER BY.
	superseded := make([]int, 0, len(versions))
	for _, v := range versions {
		if v.Version == version || v.Status == domain.CorpusDraft {
			continue
		}
		superseded = append(superseded, v.Version)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(superseded)))

	for i, v := range superseded {
		if i < RetainedSupersededVersions {
			if err := n.materializer.Supersede(ctx, contextKey, v); err != nil {
				return err
			}
			continue
		}
		if err := n.materializer.Discard(ctx, contextKey, v); err != nil {
			return err
		}
	}
	return nil
}

// VersionReader serves the versioned-read REST surface (§7 "Versioned
// Read"). Every read rewrites the key it just fetched back to the bucket —
// rewrite-on-read — which resets that key's TTL clock for a version a
// consumer is still actively pinned to. A no-op cost-wise for the active
// version's bucket, since that bucket carries no TTL at all.
type VersionReader struct {
	kv         *kvstore.Store
	namespaces *TypeNamespaces
}

func NewVersionReader(kv *kvstore.Store, namespaces *TypeNamespaces) *VersionReader {
	return &VersionReader{kv: kv, namespaces: namespaces}
}

var ErrVersionedKeyNotFound = errors.New("no such item at this corpus version")

func (r *VersionReader) Get(ctx context.Context, contextKey string, version int, typeKey, code string) (VersionedEntry, error) {
	bucket, err := r.kv.VersionedBucketHandle(ctx, contextKey, version)
	if err != nil {
		return VersionedEntry{}, mapVersionedNotFound(err)
	}
	namespace, err := r.namespaces.For(ctx, typeKey)
	if err != nil {
		return VersionedEntry{}, err
	}
	key := ItemKey(namespace, typeKey, code)
	msg, err := bucket.Get(ctx, key)
	if err != nil {
		return VersionedEntry{}, mapVersionedNotFound(err)
	}
	var entry VersionedEntry
	if err := json.Unmarshal(msg.Value(), &entry); err != nil {
		return VersionedEntry{}, err
	}
	if _, err := bucket.Put(ctx, key, msg.Value()); err != nil {
		return entry, err
	}
	return entry, nil
}

func (r *VersionReader) List(ctx context.Context, contextKey string, version int, typeKey string) ([]VersionedEntry, error) {
	bucket, err := r.kv.VersionedBucketHandle(ctx, contextKey, version)
	if err != nil {
		return nil, mapVersionedNotFound(err)
	}
	keys, err := bucket.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return []VersionedEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	namespace, err := r.namespaces.For(ctx, typeKey)
	if err != nil {
		return nil, err
	}
	prefix := TypeKeyPrefix(namespace, typeKey)
	entries := []VersionedEntry{}
	for _, key := range keys {
		if key == "_meta" || !strings.HasPrefix(key, prefix) {
			continue
		}
		msg, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}
		var entry VersionedEntry
		if err := json.Unmarshal(msg.Value(), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
		_, _ = bucket.Put(ctx, key, msg.Value()) // rewrite-on-read
	}
	return entries, nil
}

func mapVersionedNotFound(err error) error {
	if errors.Is(err, jetstream.ErrBucketNotFound) || errors.Is(err, jetstream.ErrKeyNotFound) {
		return fmt.Errorf("%w: %v", ErrVersionedKeyNotFound, err)
	}
	return err
}
