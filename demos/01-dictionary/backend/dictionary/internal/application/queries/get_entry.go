// Package queries holds the read-side use cases for both shapes.
//
// Shape A treats NATS KV as the read model itself: reads never touch
// Postgres. Shape B treats KV as a cache in front of the canonical Postgres
// projection: a hit is served from KV, a miss falls through to Postgres and
// backfills the cache.
package queries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

// ShapeA reads directly from the KV read model.
type ShapeA struct {
	kv *kvstore.Store
}

func NewShapeA(kv *kvstore.Store) *ShapeA {
	return &ShapeA{kv: kv}
}

// GetEntry returns the entry and its KV revision.
func (q *ShapeA) GetEntry(ctx context.Context, kvContext, entityType, id string) (domain.DictionaryEntry, uint64, error) {
	key := entityType + "." + id
	value, revision, err := q.kv.Get(ctx, kvContext, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.DictionaryEntry{}, 0, domain.ErrNotFound
	}
	if err != nil {
		return domain.DictionaryEntry{}, 0, err
	}
	var entry domain.DictionaryEntry
	if err := json.Unmarshal(value, &entry); err != nil {
		return domain.DictionaryEntry{}, 0, fmt.Errorf("unmarshal kv value %s: %w", key, err)
	}
	return entry, revision, nil
}

// ListEntries returns every entry in the context's bucket.
func (q *ShapeA) ListEntries(ctx context.Context, kvContext string) ([]domain.DictionaryEntry, error) {
	keys, err := q.kv.Keys(ctx, kvContext)
	if err != nil {
		return nil, err
	}
	entries := make([]domain.DictionaryEntry, 0, len(keys))
	for _, key := range keys {
		value, _, err := q.kv.Get(ctx, kvContext, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			continue // deleted between list and get
		}
		if err != nil {
			return nil, err
		}
		var entry domain.DictionaryEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil, fmt.Errorf("unmarshal kv value %s: %w", key, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ShapeB reads from the KV cache and falls through to Postgres on a miss.
type ShapeB struct {
	kv   *kvstore.Store
	repo domain.Repository
}

func NewShapeB(kv *kvstore.Store, repo domain.Repository) *ShapeB {
	return &ShapeB{kv: kv, repo: repo}
}

// GetEntry returns the entry and whether it was served from the cache.
// On a miss the entry is fetched from Postgres and written back to KV.
func (q *ShapeB) GetEntry(ctx context.Context, kvContext, entityType, id string) (domain.DictionaryEntry, bool, error) {
	key := entityType + "." + id
	value, _, err := q.kv.Get(ctx, kvContext, key)
	if err == nil {
		var entry domain.DictionaryEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return domain.DictionaryEntry{}, false, fmt.Errorf("unmarshal kv value %s: %w", key, err)
		}
		return entry, true, nil
	}
	if !errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.DictionaryEntry{}, false, err
	}

	// Cache miss: fall through to the canonical projection.
	entry, err := q.repo.Find(ctx, kvContext, entityType, id)
	if err != nil {
		return domain.DictionaryEntry{}, false, err
	}

	// Backfill the cache so the next read is a hit. A failure here is
	// logged by the caller, not fatal: the read itself succeeded.
	if data, err := json.Marshal(entry); err == nil {
		_, _ = q.kv.Put(ctx, kvContext, key, data)
	}
	return entry, false, nil
}

// ListEntries returns the canonical projection rows for a context.
func (q *ShapeB) ListEntries(ctx context.Context, kvContext string) ([]domain.DictionaryEntry, error) {
	return q.repo.List(ctx, kvContext)
}

// EvictCacheEntry removes a key from the Shape B cache so the demo can show
// the miss → Postgres → backfill path explicitly.
func (q *ShapeB) EvictCacheEntry(ctx context.Context, kvContext, entityType, id string) error {
	err := q.kv.Delete(ctx, kvContext, entityType+"."+id)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.ErrNotFound
	}
	return err
}
