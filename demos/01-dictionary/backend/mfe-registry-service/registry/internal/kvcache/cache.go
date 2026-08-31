// Package kvcache is the registry's read cache: one NATS KV entry holding
// the whole serialized document.
//
// Whole-document, not entry-per-key, because the thing readers need
// atomically is "the catalog at revision N" — a per-entry cache would let a
// reader assemble a document that never existed at any revision. The write
// is eager (write-through from the same call that committed Postgres), and a
// miss falls through to Postgres, which is the lab's settled shape.
package kvcache

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

// Bucket is lowercase-kebab per the storage naming rule; a bucket name is a
// stream name, so renaming it orphans the old stream rather than migrating.
const Bucket = "mfe-registry"

// Key is context-scoped like every other KV key in the lab. The registry is
// platform-wide, so its context is the reserved `_platform`.
const Key = "_platform.frontend-plugins.current"

type Cache struct {
	js jetstream.JetStream

	mu sync.Mutex
	kv jetstream.KeyValue
}

func New(js jetstream.JetStream) *Cache { return &Cache{js: js} }

func (c *Cache) bucket(ctx context.Context) (jetstream.KeyValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.kv != nil {
		return c.kv, nil
	}
	kv, err := c.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: Bucket})
	if err != nil {
		return nil, err
	}
	c.kv = kv
	return kv, nil
}

// Ensure provisions the bucket at startup so it is visible in the Admin UI's
// KV inspector before anything has ever been written.
func (c *Cache) Ensure(ctx context.Context) error {
	_, err := c.bucket(ctx)
	return err
}

// Get returns the cached document. A miss is (Document{}, false, nil) — the
// caller falls through to Postgres rather than treating it as an error.
func (c *Cache) Get(ctx context.Context) (domain.Document, bool, error) {
	kv, err := c.bucket(ctx)
	if err != nil {
		return domain.Document{}, false, err
	}
	e, err := kv.Get(ctx, Key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return domain.Document{}, false, nil
		}
		return domain.Document{}, false, err
	}
	var doc domain.Document
	if err := json.Unmarshal(e.Value(), &doc); err != nil {
		// A cache entry that will not parse is a miss, not an outage: the
		// source of truth is one query away.
		return domain.Document{}, false, nil
	}
	return doc, true, nil
}

// Put overwrites the cached document.
//
// The whole document is marshalled as-is, which is what keeps a signed
// entry's manifest bytes intact (BR-AS50): domain.Manifest.Bytes is []byte,
// so encoding/json carries it as base64 in both directions and this hop
// never re-serialises the artifact it is copying.
//
// Called by the same code path that committed the write, after the commit —
// the cache never leads Postgres.
func (c *Cache) Put(ctx context.Context, doc domain.Document) error {
	kv, err := c.bucket(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = kv.Put(ctx, Key, body)
	return err
}
