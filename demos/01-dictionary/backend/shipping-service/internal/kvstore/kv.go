// Package kvstore wraps NATS KV with the lab's tenant-scoped bucket
// convention: one bucket per application role per tenant, named by the
// prefix alone (e.g. "dict-a", "dict-b", "container", "meta"). Context
// (business-unit scope) is folded into the KV key as a prefix:
// {context}.{entityType}.{id}. The NATS account boundary enforces tenant
// isolation; {context} scopes within a tenant. Before this design, the
// bucket was per-(role, context) pair, e.g. "dict-a-acme-northdiv", which
// consumed one stream per context per role and exhausted js_max_streams as
// business-unit count grew.
package kvstore

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)


// Store manages a single KV bucket per Store instance. All operations accept
// a kvContext string that is prepended to the key internally, so multiple
// business-unit contexts share one bucket without colliding.
type Store struct {
	js     jetstream.JetStream
	prefix string // also the bucket name

	mu sync.Mutex
	kv jetstream.KeyValue // lazily initialised on first use
}

func New(js jetstream.JetStream, prefix string) *Store {
	return &Store{js: js, prefix: prefix}
}

// bucket returns the KV bucket, creating it on first call.
func (s *Store) bucket(ctx context.Context) (jetstream.KeyValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kv != nil {
		return s.kv, nil
	}
	kv, err := s.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: s.prefix})
	if err != nil {
		return nil, fmt.Errorf("create kv bucket %s: %w", s.prefix, err)
	}
	s.kv = kv
	return kv, nil
}

// internalKey builds the full KV key stored in the bucket: {kvContext}.{key}.
func internalKey(kvContext, key string) string { return kvContext + "." + key }

// stripPrefix removes the {kvContext}. prefix from a full internal key.
func stripPrefix(kvContext, full string) string {
	return strings.TrimPrefix(full, kvContext+".")
}

// Put writes a key and returns the new KV revision.
func (s *Store) Put(ctx context.Context, kvContext, key string, value []byte) (uint64, error) {
	kv, err := s.bucket(ctx)
	if err != nil {
		return 0, err
	}
	return kv.Put(ctx, internalKey(kvContext, key), value)
}

// Get reads a key, returning the value and its revision.
// jetstream.ErrKeyNotFound is returned unchanged so callers can branch on it.
func (s *Store) Get(ctx context.Context, kvContext, key string) ([]byte, uint64, error) {
	kv, err := s.bucket(ctx)
	if err != nil {
		return nil, 0, err
	}
	entry, err := kv.Get(ctx, internalKey(kvContext, key))
	if err != nil {
		return nil, 0, err
	}
	return entry.Value(), entry.Revision(), nil
}

// Delete removes a key (used to evict Shape B cache entries).
func (s *Store) Delete(ctx context.Context, kvContext, key string) error {
	kv, err := s.bucket(ctx)
	if err != nil {
		return err
	}
	return kv.Delete(ctx, internalKey(kvContext, key))
}

// Keys lists all keys for the given context, with the context prefix stripped
// so callers receive the same bare keys (e.g. "ship.SHIP1") as before.
func (s *Store) Keys(ctx context.Context, kvContext string) ([]string, error) {
	kv, err := s.bucket(ctx)
	if err != nil {
		return nil, err
	}
	lister, err := kv.ListKeysFiltered(ctx, kvContext+".>")
	if err != nil {
		return nil, err
	}
	var keys []string
	for k := range lister.Keys() {
		keys = append(keys, stripPrefix(kvContext, k))
	}
	return keys, nil
}

// Watch watches every key for the given context. The returned watcher
// first replays current values, then a nil marker, then live updates.
// Entry keys have the context prefix stripped so callers see bare keys.
func (s *Store) Watch(ctx context.Context, kvContext string) (jetstream.KeyWatcher, error) {
	kv, err := s.bucket(ctx)
	if err != nil {
		return nil, err
	}
	w, err := kv.Watch(ctx, kvContext+".>")
	if err != nil {
		return nil, err
	}
	return &contextFilteredWatcher{inner: w, kvContext: kvContext}, nil
}

// contextFilteredWatcher wraps a KeyWatcher and strips the {context}. prefix
// from entry keys before forwarding them, keeping the caller interface stable.
type contextFilteredWatcher struct {
	inner     jetstream.KeyWatcher
	kvContext  string
	ch        chan jetstream.KeyValueEntry
	once      sync.Once
}

func (w *contextFilteredWatcher) init() {
	w.ch = make(chan jetstream.KeyValueEntry, 64)
	go func() {
		defer close(w.ch)
		for entry := range w.inner.Updates() {
			if entry == nil {
				w.ch <- nil
				continue
			}
			w.ch <- &prefixStrippedEntry{KeyValueEntry: entry, kvContext: w.kvContext}
		}
	}()
}

func (w *contextFilteredWatcher) Updates() <-chan jetstream.KeyValueEntry {
	w.once.Do(w.init)
	return w.ch
}

func (w *contextFilteredWatcher) Stop() error { return w.inner.Stop() }

// prefixStrippedEntry wraps a KeyValueEntry, overriding Key() to return the
// bare user key without the {context}. prefix.
type prefixStrippedEntry struct {
	jetstream.KeyValueEntry
	kvContext string
}

func (e *prefixStrippedEntry) Key() string {
	return stripPrefix(e.kvContext, e.KeyValueEntry.Key())
}
