// Package kvcache implements the Q5 versioned-read protocol
// (Dictionary-Service-Plan.md): Postgres stays the source of truth; every
// mutation bumps a per-{context,type} set version (BR-D04), refreshes the
// item's KV cache entry, and publishes a bounded change-event pointer so
// consumers know to re-pull a stale type. Nothing here is ever read back as
// truth — a cache rebuild from Postgres is always correct, same guarantee
// the shipping backend's own write-through KV cache already relies on.
package kvcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natstrace"
)

// ChangeStreamName is the bounded JetStream stream carrying change-event
// pointers (Q6 role 2) — never the source of truth.
const ChangeStreamName = "REFDATA"

// Domain identifies this service in the shared
// evt.{context}.{service}.{entity}.{id}.{event} subject taxonomy — a fixed
// literal, not a wildcard. Note {context} is the company/business-unit scope,
// NOT the tenant (tenant = NATS account) and NOT the region (separate regional
// deployment) — see ARCHITECTURE-COMMUNICATIONS.md § 2.3.
const Domain = "refdata"

// ChangeSubjectWildcard is the stream's subject filter, any context/type.
//
// The leading token is the fixed literal "evt", not a wildcard — a stream
// subject filter whose first token is an unbounded wildcard (e.g.
// "*.refdata.>") can textually overlap "$SYS.>"/"$JS.API.>" (a bare "*"
// accepts "$SYS" as its value), and JetStream refuses to create such a
// stream without NoAck — which would break the synchronous Publish/PubAck
// flow the projector relies on. Putting "evt" first avoids the overlap
// while leaving context/typeKey equally filterable.
const ChangeSubjectWildcard = "evt.*." + Domain + ".*.changed"

// ChangeSubject builds the subject a single type's change events publish to:
// evt.{context}.refdata.{typeKey}.changed.
func ChangeSubject(itemContext, typeKey string) string {
	return fmt.Sprintf("evt.%s.%s.%s.changed", itemContext, Domain, typeKey)
}

// CacheItem is the item payload stored in a KV cache entry — a projection of
// domain.DictionaryItem stripped of attrs (a Postgres/REST concern). No
// label/description here: BR-D30 guarantees the default locale's
// localization exists whenever an item has any localizations at all, so a
// reader resolves the default-locale label from Entry.Localizations
// directly instead of a duplicated, write-time-resolved item-level field.
type CacheItem struct {
	TypeKey string            `json:"typeKey"`
	Code    string            `json:"code"`
	Context string            `json:"context"`
	Status  domain.ItemStatus `json:"status"`
}

// Entry is the KV cache's assembled read view of one item — the item plus
// its localizations and outbound references, stamped with the type's set
// version at write time (Q5). Localizations use LocalizationValue (not the
// full domain.Localization) so the per-locale payload doesn't repeat
// typeKey/code/context that the parent Item already carries.
type Entry struct {
	Item          CacheItem                             `json:"item"`
	Localizations map[string]domain.LocalizationValue   `json:"localizations,omitempty"`
	References    map[string]domain.DictionaryReference `json:"references,omitempty"`
	Version       int                                   `json:"version"`
}

// MetaEntry is the `{namespace}{type}._meta` key — the whole set's current
// version and size, used for the versioned-read protocol's mismatch check.
// It shares its type's BR-D31 namespace, so a type's entries and its stamp
// live in one addressable subtree.
type MetaEntry struct {
	Version   int       `json:"version"`
	ItemCount int       `json:"itemCount"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ChangeEvent is the change-event pointer payload — never item state, only
// "this type changed, here is its new version" (Q6).
type ChangeEvent struct {
	TypeKey string `json:"typeKey"`
	Context string `json:"context"`
	Version int    `json:"version"`
}

// Publisher publishes raw bytes to a subject — satisfied by jstream.Publisher.
// PublishWithTrace (Phase 28d) additionally carries a traceparent header
// derived from an in-flight *natstrace.Span, so the change-event pointer a
// mutation causes rides the same trace as the rpc.*/REST request that caused
// it (BR-037, BR-D39) — nil-safe, matching jstream.Publisher's own
// nil-sp-means-plain-Publish behavior.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
	PublishWithTrace(ctx context.Context, sp *natstrace.Span, subject string, data []byte) error
}

// Projector rebuilds an item's KV cache entry and the type's _meta on every
// change, bumps the set version, and publishes the change-event pointer.
type Projector struct {
	kv         *kvstore.Store
	items      domain.ItemRepository
	locs       domain.LocalizationRepository
	refs       domain.ReferenceRepository
	versions   domain.VersionRepository
	namespaces *TypeNamespaces
	pub        Publisher
}

func NewProjector(kv *kvstore.Store, items domain.ItemRepository, locs domain.LocalizationRepository, refs domain.ReferenceRepository, versions domain.VersionRepository, namespaces *TypeNamespaces, pub Publisher) *Projector {
	return &Projector{kv: kv, items: items, locs: locs, refs: refs, versions: versions, namespaces: namespaces, pub: pub}
}

var _ domain.ChangeNotifier = (*Projector)(nil)

// NotifyItemChanged bumps typeKey's set version, rebuilds the item's cache
// entry (or removes it if the item was deleted) and the type's _meta, then
// publishes a change-event pointer. Called after every committed mutation.
func (p *Projector) NotifyItemChanged(ctx context.Context, itemContext, typeKey, code string) error {
	version, err := p.versions.Bump(ctx, itemContext, typeKey)
	if err != nil {
		return err
	}

	if err := p.rebuildEntry(ctx, itemContext, typeKey, code, version); err != nil {
		return err
	}
	if err := p.rebuildMeta(ctx, itemContext, typeKey, version); err != nil {
		return err
	}

	event, err := json.Marshal(ChangeEvent{TypeKey: typeKey, Context: itemContext, Version: version})
	if err != nil {
		return err
	}
	sp := natstrace.SpanFromContext(ctx)
	return p.pub.PublishWithTrace(ctx, sp, ChangeSubject(itemContext, typeKey), event)
}

// Backfill rebuilds an item's cache entry at the type's CURRENT version,
// without bumping it or publishing an event — the read-path repair for a
// cache miss or a cold start (Q5's "cache miss falls through... back-fills
// KV" — identical in spirit to the shipping backend's own KV-cache-then-Postgres miss path).
func (p *Projector) Backfill(ctx context.Context, itemContext, typeKey, code string) error {
	version, err := p.versions.Current(ctx, itemContext, typeKey)
	if err != nil {
		return err
	}
	if err := p.rebuildEntry(ctx, itemContext, typeKey, code, version); err != nil {
		return err
	}
	return p.rebuildMeta(ctx, itemContext, typeKey, version)
}

// ReadEntry is the read-side counterpart of rebuildEntry — it serves an
// item's cache entry directly from KV without touching Postgres, for the
// RPC handler's KV-first path (BR-D08). It returns (nil, nil) on a cache
// miss or a stale entry (stamped version doesn't match the type's current
// _meta version) so the caller can fall through to Postgres + Backfill,
// exactly as a direct KV miss would have before this was internalized.
func (p *Projector) ReadEntry(ctx context.Context, itemContext, typeKey, code string) (*Entry, error) {
	namespace, err := p.namespaces.For(ctx, typeKey)
	if err != nil {
		return nil, err
	}

	entryRaw, _, err := p.kv.Get(ctx, itemContext, ItemKey(namespace, typeKey, code))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entry Entry
	if err := json.Unmarshal(entryRaw, &entry); err != nil {
		return nil, err
	}

	metaRaw, _, err := p.kv.Get(ctx, itemContext, MetaKey(namespace, typeKey))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var meta MetaEntry
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return nil, err
	}

	if meta.Version != entry.Version {
		return nil, nil // stale — the set moved on since this entry was cached
	}
	return &entry, nil
}

// ReadMeta returns a type's cached _meta stamp, or (nil, nil) if the type
// has no cache entry yet. Owning this here keeps the BR-D31 namespace out of
// callers that only want to compare the cached version against Postgres's
// (the cache-status endpoint).
func (p *Projector) ReadMeta(ctx context.Context, itemContext, typeKey string) (*MetaEntry, error) {
	namespace, err := p.namespaces.For(ctx, typeKey)
	if err != nil {
		return nil, err
	}
	raw, _, err := p.kv.Get(ctx, itemContext, MetaKey(namespace, typeKey))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var meta MetaEntry
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ReadType returns every fresh cache entry for typeKey, or (nil, false) if
// the type's cache is missing, incomplete, or contains any stale entry — the
// RPC handler's KV-first path for a type-list request (BR-D08). A
// partial/stale cache falls through to Postgres wholesale rather than
// patching just the missing/stale entries, keeping the read path as simple
// as ReadEntry's whole-entry miss semantics.
func (p *Projector) ReadType(ctx context.Context, itemContext, typeKey string) ([]Entry, bool) {
	namespace, err := p.namespaces.For(ctx, typeKey)
	if err != nil {
		return nil, false
	}

	metaRaw, _, err := p.kv.Get(ctx, itemContext, MetaKey(namespace, typeKey))
	if err != nil {
		return nil, false
	}
	var meta MetaEntry
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return nil, false
	}

	keys, err := p.kv.Keys(ctx, itemContext)
	if err != nil {
		return nil, false
	}

	prefix := TypeKeyPrefix(namespace, typeKey)
	metaKey := MetaKey(namespace, typeKey)
	entries := make([]Entry, 0, meta.ItemCount)
	for _, k := range keys {
		if k == metaKey || !strings.HasPrefix(k, prefix) {
			continue
		}
		raw, _, err := p.kv.Get(ctx, itemContext, k)
		if err != nil {
			return nil, false
		}
		var entry Entry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, false
		}
		if entry.Version != meta.Version {
			return nil, false
		}
		entries = append(entries, entry)
	}
	if len(entries) != meta.ItemCount {
		return nil, false
	}
	return entries, true
}

func (p *Projector) rebuildEntry(ctx context.Context, itemContext, typeKey, code string, version int) error {
	namespace, err := p.namespaces.For(ctx, typeKey)
	if err != nil {
		return err
	}
	key := ItemKey(namespace, typeKey, code)

	item, err := p.items.Get(ctx, typeKey, itemContext, code)
	if errors.Is(err, domain.ErrItemNotFound) {
		return p.kv.Delete(ctx, itemContext, key)
	}
	if err != nil {
		return err
	}

	locList, err := p.locs.ListForItem(ctx, typeKey, itemContext, code)
	if err != nil {
		return err
	}
	locs := make(map[string]domain.LocalizationValue, len(locList))
	for _, loc := range locList {
		locs[loc.Locale] = loc.ToValue()
	}

	refList, err := p.refs.ListFrom(ctx, itemContext, typeKey, code)
	if err != nil {
		return err
	}
	refs := make(map[string]domain.DictionaryReference, len(refList))
	for _, ref := range refList {
		refs[ref.Relation] = ref
	}

	cacheItem := CacheItem{
		TypeKey: item.TypeKey, Code: item.Code, Context: item.Context,
		Status: item.Status,
	}

	entry, err := json.Marshal(Entry{Item: cacheItem, Localizations: locs, References: refs, Version: version})
	if err != nil {
		return err
	}
	_, err = p.kv.Put(ctx, itemContext, key, entry)
	return err
}

func (p *Projector) rebuildMeta(ctx context.Context, itemContext, typeKey string, version int) error {
	namespace, err := p.namespaces.For(ctx, typeKey)
	if err != nil {
		return err
	}
	items, err := p.items.List(ctx, typeKey, itemContext)
	if err != nil {
		return err
	}
	meta, err := json.Marshal(MetaEntry{Version: version, ItemCount: len(items), UpdatedAt: time.Now()})
	if err != nil {
		return err
	}
	_, err = p.kv.Put(ctx, itemContext, MetaKey(namespace, typeKey), meta)
	return err
}
