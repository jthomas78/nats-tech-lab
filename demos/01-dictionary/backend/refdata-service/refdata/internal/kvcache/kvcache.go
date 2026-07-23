// Package kvcache implements the Q5 versioned-read protocol
// (Dictionary-Service-Plan.md): Postgres stays the source of truth; every
// mutation bumps a per-{context,type} set version (BR-D04), refreshes the
// item's KV cache entry, and publishes a bounded change-event pointer so
// consumers know to re-pull a stale type. Nothing here is ever read back as
// truth — a cache rebuild from Postgres is always correct, same guarantee
// Shape B's write-through cache already relies on.
package kvcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
)

// ChangeStreamName is the bounded JetStream stream carrying change-event
// pointers (Q6 role 2) — never the source of truth.
const ChangeStreamName = "REFDATA"

// Domain identifies this service in the shared evt.<tenant>.<domain>...
// subject taxonomy — a fixed literal, not a wildcard.
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

// Entry is the KV cache's assembled read view of one item — the item plus
// its localizations and outbound references, stamped with the type's set
// version at write time (Q5).
type Entry struct {
	Item          domain.DictionaryItem                 `json:"item"`
	Localizations map[string]domain.Localization        `json:"localizations,omitempty"`
	References    map[string]domain.DictionaryReference `json:"references,omitempty"`
	Version       int                                   `json:"version"`
}

// MetaEntry is the `{type}._meta` key — the whole set's current version and
// size, used for the versioned-read protocol's mismatch check.
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
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// Projector rebuilds an item's KV cache entry and the type's _meta on every
// change, bumps the set version, and publishes the change-event pointer.
type Projector struct {
	kv       *kvstore.Store
	items    domain.ItemRepository
	locs     domain.LocalizationRepository
	refs     domain.ReferenceRepository
	versions domain.VersionRepository
	pub      Publisher
}

func NewProjector(kv *kvstore.Store, items domain.ItemRepository, locs domain.LocalizationRepository, refs domain.ReferenceRepository, versions domain.VersionRepository, pub Publisher) *Projector {
	return &Projector{kv: kv, items: items, locs: locs, refs: refs, versions: versions, pub: pub}
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
	return p.pub.Publish(ctx, ChangeSubject(itemContext, typeKey), event)
}

// Backfill rebuilds an item's cache entry at the type's CURRENT version,
// without bumping it or publishing an event — the read-path repair for a
// cache miss or a cold start (Q5's "cache miss falls through... back-fills
// KV" — identical in spirit to Shape B's miss path).
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

func (p *Projector) rebuildEntry(ctx context.Context, itemContext, typeKey, code string, version int) error {
	key := typeKey + "." + code

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
	locs := make(map[string]domain.Localization, len(locList))
	for _, loc := range locList {
		locs[loc.Locale] = loc
	}

	refList, err := p.refs.ListFrom(ctx, itemContext, typeKey, code)
	if err != nil {
		return err
	}
	refs := make(map[string]domain.DictionaryReference, len(refList))
	for _, ref := range refList {
		refs[ref.Relation] = ref
	}

	entry, err := json.Marshal(Entry{Item: item, Localizations: locs, References: refs, Version: version})
	if err != nil {
		return err
	}
	_, err = p.kv.Put(ctx, itemContext, key, entry)
	return err
}

func (p *Projector) rebuildMeta(ctx context.Context, itemContext, typeKey string, version int) error {
	items, err := p.items.List(ctx, typeKey, itemContext)
	if err != nil {
		return err
	}
	meta, err := json.Marshal(MetaEntry{Version: version, ItemCount: len(items), UpdatedAt: time.Now()})
	if err != nil {
		return err
	}
	_, err = p.kv.Put(ctx, itemContext, typeKey+"._meta", meta)
	return err
}
