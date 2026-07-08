// Package queries holds the read-side use cases for all three shapes.
//
// Shape A treats NATS KV as the read model: reads never touch Postgres.
// Shape B treats KV as a cache in front of the canonical Postgres projection.
// Shape C reconstructs state by replaying JetStream from seq=1 (see shape_c.go).
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

// ─── Shape A ─────────────────────────────────────────────────────────────────

// ShapeA reads directly from the KV read model.
type ShapeA struct {
	kv *kvstore.Store
}

func NewShapeA(kv *kvstore.Store) *ShapeA { return &ShapeA{kv: kv} }

// ListShips returns every ship state in the context's KV bucket.
func (q *ShapeA) ListShips(ctx context.Context, kvContext string) ([]domain.ShipState, error) {
	keys, err := q.kv.Keys(ctx, kvContext)
	if err != nil {
		return nil, err
	}
	ships := make([]domain.ShipState, 0, len(keys))
	for _, key := range keys {
		value, _, err := q.kv.Get(ctx, kvContext, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var state domain.ShipState
		if err := json.Unmarshal(value, &state); err != nil {
			return nil, fmt.Errorf("unmarshal kv value %s: %w", key, err)
		}
		ships = append(ships, state)
	}
	return ships, nil
}

// ─── Shape B ─────────────────────────────────────────────────────────────────

// ShapeB reads from the KV cache, falling through to Postgres on a miss.
type ShapeB struct {
	kv   *kvstore.Store
	repo domain.ShipRepository
}

func NewShapeB(kv *kvstore.Store, repo domain.ShipRepository) *ShapeB {
	return &ShapeB{kv: kv, repo: repo}
}

// GetShip returns the ship state and whether it was served from the cache.
// On a miss the state is fetched from Postgres and written back to KV.
func (q *ShapeB) GetShip(ctx context.Context, kvContext, shipID string) (domain.ShipState, bool, error) {
	key := "ship." + shipID
	raw, _, err := q.kv.Get(ctx, kvContext, key)
	if err == nil {
		var state domain.ShipState
		if err := json.Unmarshal(raw, &state); err != nil {
			return domain.ShipState{}, false, fmt.Errorf("unmarshal kv value %s: %w", key, err)
		}
		return state, true, nil
	}
	if !errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.ShipState{}, false, err
	}

	// Cache miss: fall through to Postgres.
	state, err := q.repo.Find(ctx, kvContext, shipID)
	if err != nil {
		return domain.ShipState{}, false, err
	}

	if data, err := json.Marshal(state); err == nil {
		_, _ = q.kv.Put(ctx, kvContext, key, data)
	}
	return state, false, nil
}

// ListShips returns the canonical Postgres projection rows for a fleet context.
func (q *ShapeB) ListShips(ctx context.Context, kvContext string) ([]domain.ShipState, error) {
	return q.repo.List(ctx, kvContext)
}

// EvictCacheShip removes a ship's key from the Shape B KV cache so the demo
// can show the miss → Postgres → backfill path.
func (q *ShapeB) EvictCacheShip(ctx context.Context, kvContext, shipID string) error {
	err := q.kv.Delete(ctx, kvContext, "ship."+shipID)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.ErrNotFound
	}
	return err
}
