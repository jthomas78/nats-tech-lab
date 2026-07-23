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

// VersionedEntry is one materialized item within a specific published
// corpus version's KV bucket (refdata-{context}-v{N}).
type VersionedEntry struct {
	Item          domain.DictionaryItem          `json:"item"`
	Localizations map[string]domain.Localization `json:"localizations,omitempty"`
	SourceContext string                         `json:"sourceContext"`
	IsOverride    bool                           `json:"isOverride"`
	Version       int                            `json:"version"`
}

// VersionedMeta is the "_meta" key inside a version's own bucket.
type VersionedMeta struct {
	Version   int `json:"version"`
	ItemCount int `json:"itemCount"`
}

// VersionMaterializer writes a published corpus version's full flattened
// content into its own versioned KV bucket, and manages that bucket's TTL
// as the version transitions between active and superseded.
type VersionMaterializer struct{ kv *kvstore.Store }

func NewVersionMaterializer(kv *kvstore.Store) *VersionMaterializer {
	return &VersionMaterializer{kv: kv}
}

// Materialize eagerly writes every item (with its localizations folded in)
// into the refdata-{contextKey}-v{version} bucket, plus a "_meta" summary.
// ttl is 0 for the version currently being made active, and
// SupersededVersionTTL for a version being marked superseded.
func (m *VersionMaterializer) Materialize(ctx context.Context, contextKey string, version int, items []domain.CorpusItem, locs []domain.CorpusLocalization, ttl time.Duration) error {
	byItem := map[string]map[string]domain.Localization{}
	for _, loc := range locs {
		key := loc.TypeKey + "\x00" + loc.Code
		if byItem[key] == nil {
			byItem[key] = map[string]domain.Localization{}
		}
		byItem[key][loc.Locale] = loc.Localization
	}

	bucket, err := m.kv.VersionedBucket(ctx, contextKey, version, ttl)
	if err != nil {
		return err
	}
	for _, item := range items {
		entry := VersionedEntry{
			Item:          item.DictionaryItem,
			Localizations: byItem[item.TypeKey+"\x00"+item.Code],
			SourceContext: item.SourceContext,
			IsOverride:    item.IsOverride,
			Version:       version,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := bucket.Put(ctx, item.TypeKey+"."+item.Code, data); err != nil {
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

// VersionNotifier implements domain.CorpusNotifier: on every publish or
// rollback it materializes the newly-active version and marks every other
// non-draft version for that context as superseded.
type VersionNotifier struct {
	materializer *VersionMaterializer
	corpus       domain.CorpusRepository
}

func NewVersionNotifier(kv *kvstore.Store, corpus domain.CorpusRepository) *VersionNotifier {
	return &VersionNotifier{materializer: NewVersionMaterializer(kv), corpus: corpus}
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
	for _, v := range versions {
		if v.Version == version || v.Status == domain.CorpusDraft {
			continue
		}
		if err := n.materializer.Supersede(ctx, contextKey, v.Version); err != nil {
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
type VersionReader struct{ kv *kvstore.Store }

func NewVersionReader(kv *kvstore.Store) *VersionReader { return &VersionReader{kv: kv} }

var ErrVersionedKeyNotFound = errors.New("no such item at this corpus version")

func (r *VersionReader) Get(ctx context.Context, contextKey string, version int, typeKey, code string) (VersionedEntry, error) {
	bucket, err := r.kv.VersionedBucketHandle(ctx, contextKey, version)
	if err != nil {
		return VersionedEntry{}, mapVersionedNotFound(err)
	}
	msg, err := bucket.Get(ctx, typeKey+"."+code)
	if err != nil {
		return VersionedEntry{}, mapVersionedNotFound(err)
	}
	var entry VersionedEntry
	if err := json.Unmarshal(msg.Value(), &entry); err != nil {
		return VersionedEntry{}, err
	}
	if _, err := bucket.Put(ctx, typeKey+"."+code, msg.Value()); err != nil {
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
	prefix := typeKey + "."
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
