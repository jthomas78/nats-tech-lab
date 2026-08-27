// Package kvstore wraps NATS KV with the lab's tenant-scoped bucket
// convention: one bucket per application role per tenant, named by the
// prefix alone (e.g. "ships", "container", "meta"). Context
// (business-unit scope) is folded into the KV key as a prefix:
// {context}.{entityType}.{id}. The NATS account boundary enforces tenant
// isolation; {context} scopes within a tenant. Before this design, the
// bucket was per-(role, context) pair, e.g. "ships-acme-northdiv", which
// consumed one stream per context per role and exhausted js_max_streams as
// business-unit count grew.
package kvstore

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/notify"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
)

// Store manages a single KV bucket per Store instance. All operations accept
// a kvContext string that is prepended to the key internally, so multiple
// business-unit contexts share one bucket without colliding.
type Store struct {
	js     jetstream.JetStream
	prefix string // also the bucket name

	notifier *natsnotify.Notifier // optional (nil until EnableNotify) — see Put's notify publish

	mu sync.Mutex
	kv jetstream.KeyValue // lazily initialised on first use
}

func New(js jetstream.JetStream, prefix string) *Store {
	return &Store{js: js, prefix: prefix}
}

// EnableNotify turns on Put's best-effort
// notify.{context}.kv.{bucket}.{key}.changed publish (Main-POC-Plan.md Phase
// 23) so the Admin UI's KV inspector can watch bucket changes directly over
// NATS instead of the SSE watchKVBucket handler it's replacing. Not part of
// New's constructor signature deliberately — most of this package's ~20
// existing call sites (mainly tests) have no use for it, and requiring nc at
// construction would force every one of them to pass nil, the same
// mechanical churn EnableNotify avoids. Unset (the default) means Put never
// publishes, matching today's behavior exactly.
func (s *Store) EnableNotify(nc *nats.Conn, log *slog.Logger) {
	// Observation goes out on the same connection the notify does, which is
	// what BR-D45 requires: the envelope has to sit inside the publishing
	// tenant's account for BR-AC34's import remap to name the right tenant.
	s.notifier = natsnotify.New(nc, log, natsnotify.WithObservation(nc))
}

// Ensure creates the bucket now rather than on first Put/Get/Keys.
//
// Every other operation on this Store provisions lazily, which is fine for
// correctness but leaves a bucket that has simply never been written
// indistinguishable, from outside, from one that does not exist: the Admin
// UI's cross-account reader (observability-service's
// GET /api/kv/buckets/{account}/{bucket}/entries) answers ErrBucketNotFound
// with a 400, so a tenant with no ships yet made the Admin UI log a red
// "unknown bucket: ships" on every page load. Which of a tenant's three
// buckets happened to exist was decided by whichever query ran first, not by
// anything about the tenant — "container" and "meta" were routinely present
// and "ships" was not.
//
// Calling this at tenant-provisioning time makes the three buckets an
// invariant of a provisioned tenant, alongside the SHIPPING stream that is
// already created eagerly beside them. It is deliberately NOT folded into
// New: most of this package's call sites are tests that want the lazy
// behaviour and have no ctx to give.
func (s *Store) Ensure(ctx context.Context) error {
	_, err := s.bucket(ctx)
	return err
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
	rev, err := kv.Put(ctx, internalKey(kvContext, key), value)
	if err == nil {
		s.publishNotify(ctx, kvContext, key, value)
	}
	return rev, err
}

// publishNotify fires notify.{context}.kv.{bucket}.{key}.changed after a
// successful Put or Delete.
//
// value is nil for a Delete (the wire message carries zero bytes) — the KV
// inspector distinguishes PUT from DEL by whether the notify payload is
// empty, since this repo's own KV values are always non-empty JSON. That is
// the only thing this method decides; the publish, the gate, the traceparent
// on ctx and the BR-045 observation all belong to shared/natsnotify, and the
// subject to internal/notify.
func (s *Store) publishNotify(ctx context.Context, kvContext, key string, value []byte) {
	s.notifier.Publish(ctx, notify.KVChanged(kvContext, s.prefix, key), value)
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
	if err := kv.Delete(ctx, internalKey(kvContext, key)); err != nil {
		return err
	}
	s.publishNotify(ctx, kvContext, key, nil)
	return nil
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
	kvContext string
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
