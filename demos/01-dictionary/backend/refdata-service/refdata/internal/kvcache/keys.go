package kvcache

import (
	"context"
	"errors"
	"sync"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// Key layout for the refdata KV buckets (BR-D31). Every key belonging to a
// type sits under that type's namespace — the item entries and the type's
// _meta stamp alike — so a whole category is addressable as one subject
// subtree (enum.>) for watches and permissions, without splitting the
// bucket per type. The namespace is a projection of the type's existing
// BR-D09 category, not new per-item state.

// ItemKey is the KV key for one item's cache entry.
func ItemKey(namespace, typeKey, code string) string {
	return namespace + typeKey + "." + code
}

// MetaKey is the KV key for a type's set version/count stamp.
func MetaKey(namespace, typeKey string) string {
	return namespace + typeKey + "._meta"
}

// TypeKeyPrefix is the key prefix every entry of a type shares — used to
// scan a type's entries out of a bucket holding many types.
func TypeKeyPrefix(namespace, typeKey string) string {
	return namespace + typeKey + "."
}

// TypeNamespaces resolves a type's key namespace (BR-D31), memoizing the
// category lookup for the process lifetime.
//
// The memo matters: the KV-first read paths (BR-D08) exist to serve a warm
// read without touching Postgres, and resolving a namespace per lookup would
// put a Postgres round-trip back in front of every cache hit. Caching is safe
// because a registered type's category is effectively immutable —
// TypeRepository offers no update path — and the registry is tiny. Changing a
// live type's category would move its keys, so it requires a cache rebuild
// either way.
//
// An unregistered type resolves to the unnamespaced default and is
// deliberately not memoized, so a type registered later still picks up its
// namespace.
type TypeNamespaces struct {
	types domain.TypeRepository
	mu    sync.RWMutex
	known map[string]string
}

func NewTypeNamespaces(types domain.TypeRepository) *TypeNamespaces {
	return &TypeNamespaces{types: types, known: map[string]string{}}
}

// For returns typeKey's key namespace, "" for an unnamespaced category.
func (n *TypeNamespaces) For(ctx context.Context, typeKey string) (string, error) {
	if n == nil {
		return "", nil
	}
	n.mu.RLock()
	namespace, ok := n.known[typeKey]
	n.mu.RUnlock()
	if ok {
		return namespace, nil
	}

	t, err := n.types.Get(ctx, typeKey)
	if errors.Is(err, domain.ErrTypeNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	namespace = domain.KeyNamespace(t.Category)
	n.mu.Lock()
	n.known[typeKey] = namespace
	n.mu.Unlock()
	return namespace, nil
}
