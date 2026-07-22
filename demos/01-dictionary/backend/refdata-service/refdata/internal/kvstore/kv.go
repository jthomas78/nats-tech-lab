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
