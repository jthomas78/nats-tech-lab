// Package kvstore wraps NATS KV with the lab's context-scoped bucket
// convention: one bucket per application context, named {prefix}-{context}.
// Same wrapper shape as the shipping backend's internal/kvstore — duplicated
// rather than shared, since these are separate Go modules/services by design
// (Phase 11, Dictionary-Service-Plan.md).
package kvstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

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

// Keys lists every key currently in the context's bucket — used by the
// RPC handler's KV-first type-list path (BR-D08) to enumerate a type's
// cached items without a Postgres round-trip.
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

func (s *Store) Delete(ctx context.Context, kvContext, key string) error {
	kv, err := s.Bucket(ctx, kvContext)
	if err != nil {
		return err
	}
	return kv.Delete(ctx, key)
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

// versionedBucketName is the corpus-versioned bucket naming convention —
// {prefix}-{context}-v{version} — distinct from the unversioned
// {prefix}-{context} bucket the plain Q5 cache (Bucket/Put/Get above) uses.
func (s *Store) versionedBucketName(kvContext string, version int) string {
	return fmt.Sprintf("%s-%s-v%d", s.prefix, kvContext, version)
}

// VersionedBucket creates (or updates the TTL of) the KV bucket for one
// published corpus version. Not cached in s.buckets the way Bucket is —
// versioned buckets are written rarely (on publish/rollback/supersede), not
// on every request, so the map's memoization isn't worth the bookkeeping of
// evicting entries for versions nobody reads anymore.
func (s *Store) VersionedBucket(ctx context.Context, kvContext string, version int, ttl time.Duration) (jetstream.KeyValue, error) {
	name := s.versionedBucketName(kvContext, version)
	kv, err := s.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, TTL: ttl})
	if err != nil {
		return nil, fmt.Errorf("create versioned kv bucket %s: %w", name, err)
	}
	return kv, nil
}

// VersionedBucketHandle gets a handle to an existing versioned bucket
// without creating it or touching its TTL config — the read path's entry
// point, so a plain GET can never accidentally reconfigure the bucket.
func (s *Store) VersionedBucketHandle(ctx context.Context, kvContext string, version int) (jetstream.KeyValue, error) {
	return s.js.KeyValue(ctx, s.versionedBucketName(kvContext, version))
}
