package queries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

// Meta KV keys — cross-cutting derived lookup sets in the meta-{context}
// bucket, maintained incrementally by the meta projector.
//
// known-ports was retired: ports are now the Postgres-backed reference table
// (BR-017, BR-018) served by commands.PortHandler / GET /api/ports/{context},
// not a derived event-history projection.
const (
	MetaKeyKnownContainers = "known-containers"
)

// Meta reads the cross-cutting lookup sets from the meta-{context} KV bucket.
// These back UI selectors (container pickers) so the full history survives
// reload without client-side accumulation or event replay.
type Meta struct {
	kv *kvstore.Store
}

func NewMeta(kv *kvstore.Store) *Meta { return &Meta{kv: kv} }

// KnownContainers returns every container ID ever registered in the context. Sorted.
func (q *Meta) KnownContainers(ctx context.Context, kvContext string) ([]string, error) {
	return q.stringSet(ctx, kvContext, MetaKeyKnownContainers)
}

func (q *Meta) stringSet(ctx context.Context, kvContext, key string) ([]string, error) {
	raw, _, err := q.kv.Get(ctx, kvContext, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("unmarshal meta %s: %w", key, err)
	}
	return values, nil
}
