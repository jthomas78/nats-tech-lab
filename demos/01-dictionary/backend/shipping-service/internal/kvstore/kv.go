// Package kvstore wraps NATS KV with the lab's context-scoped bucket
// convention: one bucket per application context (tenant/region/locale),
// named {prefix}-{context}, e.g. dict-a-en-GB. There are no global,
// unscoped lookups.
package kvstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

// versionedBucketName mirrors refdata-service's Phase 12.5 versioned bucket
// naming convention: {prefix}-{context}-v{version}. This service never
// creates these buckets — refdata-service owns and eagerly materializes
// them on publish/rollback — so this is read-only naming knowledge, the
// same kind of cross-service convention refdataconsumer already relies on
// for the unversioned {prefix}-{context} bucket.
func versionedBucketName(prefix, kvContext string, version int) string {
	return fmt.Sprintf("%s-%s-v%d", prefix, kvContext, version)
}

// Store manages a family of context-scoped KV buckets sharing one prefix.
type Store struct {
	js     jetstream.JetStream
	prefix string

	mu      sync.Mutex
	buckets map[string]jetstream.KeyValue
}

func New(js jetstream.JetStream, prefix string) *Store {
	return &Store{
		js:      js,
		prefix:  prefix,
		buckets: make(map[string]jetstream.KeyValue),
	}
}

// Bucket returns the KV bucket for the given application context, creating
// it on first use.
func (s *Store) Bucket(ctx context.Context, kvContext string) (jetstream.KeyValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if kv, ok := s.buckets[kvContext]; ok {
		return kv, nil
	}
	name := s.prefix + "-" + kvContext
	kv, err := s.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name})
	if err != nil {
		return nil, fmt.Errorf("create kv bucket %s: %w", name, err)
	}
	s.buckets[kvContext] = kv
	return kv, nil
}

// Put writes a key and returns the new KV revision.
func (s *Store) Put(ctx context.Context, kvContext, key string, value []byte) (uint64, error) {
	kv, err := s.Bucket(ctx, kvContext)
	if err != nil {
		return 0, err
	}
	return kv.Put(ctx, key, value)
}

// Get reads a key, returning the value and its revision.
// jetstream.ErrKeyNotFound is returned unchanged so callers can branch on it.
func (s *Store) Get(ctx context.Context, kvContext, key string) ([]byte, uint64, error) {
	kv, err := s.Bucket(ctx, kvContext)
	if err != nil {
		return nil, 0, err
	}
	entry, err := kv.Get(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	return entry.Value(), entry.Revision(), nil
}

// Delete removes a key (used to evict Shape B cache entries).
func (s *Store) Delete(ctx context.Context, kvContext, key string) error {
	kv, err := s.Bucket(ctx, kvContext)
	if err != nil {
		return err
	}
	return kv.Delete(ctx, key)
}

// Keys lists all keys in the context's bucket.
func (s *Store) Keys(ctx context.Context, kvContext string) ([]string, error) {
	kv, err := s.Bucket(ctx, kvContext)
	if err != nil {
		return nil, err
	}
	lister, err := kv.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	var keys []string
	for k := range lister.Keys() {
		keys = append(keys, k)
	}
	return keys, nil
}

// Watch watches every key in the context's bucket. The returned watcher
// first replays current values, then a nil marker, then live updates.
func (s *Store) Watch(ctx context.Context, kvContext string) (jetstream.KeyWatcher, error) {
	kv, err := s.Bucket(ctx, kvContext)
	if err != nil {
		return nil, err
	}
	return kv.WatchAll(ctx)
}

// PutVersioned writes a key into a specific corpus version's bucket,
// creating the bucket if needed. Production code never calls this —
// refdata-service alone owns and materializes versioned buckets — it exists
// so tests can seed a versioned cache entry without duplicating the bucket
// naming convention.
func (s *Store) PutVersioned(ctx context.Context, kvContext string, version int, key string, value []byte) (uint64, error) {
	kv, err := s.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: versionedBucketName(s.prefix, kvContext, version)})
	if err != nil {
		return 0, err
	}
	return kv.Put(ctx, key, value)
}

// GetVersioned reads a key from a specific corpus version's bucket
// (refdata-{context}-v{N}, Phase 12.5) — the version-pinning read path. It
// gets the bucket handle without creating or reconfiguring it (a pinned
// reader must never touch that bucket's TTL, which refdata-service alone
// manages), then rewrites the same value back on a hit. That rewrite is
// "rewrite-on-read": a Put resets the key's own TTL clock, keeping a
// version this consumer is still pinned to from expiring out from under it,
// mirroring refdata-service's own versioned-read behavior exactly.
func (s *Store) GetVersioned(ctx context.Context, kvContext string, version int, key string) ([]byte, uint64, error) {
	kv, err := s.js.KeyValue(ctx, versionedBucketName(s.prefix, kvContext, version))
	if err != nil {
		return nil, 0, err
	}
	entry, err := kv.Get(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if _, err := kv.Put(ctx, key, entry.Value()); err != nil {
		return entry.Value(), entry.Revision(), err
	}
	return entry.Value(), entry.Revision(), nil
}
