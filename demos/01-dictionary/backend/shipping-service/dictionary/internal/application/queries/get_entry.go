// Package queries holds the read-side use cases for ship/container/meta.
//
// Ships treats KV as a cache in front of the canonical Postgres projection.
package queries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// ─── Ships ───────────────────────────────────────────────────────────────────

// Ships reads from the KV cache, falling through to Postgres on a miss.
type Ships struct {
	kv   *kvstore.Store
	repo domain.ShipRepository
}

func NewShips(kv *kvstore.Store, repo domain.ShipRepository) *Ships {
	return &Ships{kv: kv, repo: repo}
}

// GetShip returns the ship state and whether it was served from the cache.
// On a miss the state is fetched from Postgres and written back to KV.
func (q *Ships) GetShip(ctx context.Context, kvContext, shipID string) (domain.ShipState, bool, error) {
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
func (q *Ships) ListShips(ctx context.Context, kvContext string) ([]domain.ShipState, error) {
	return q.repo.List(ctx, kvContext)
}

// EvictCacheShip removes a ship's key from the KV cache so the demo can
// show the miss → Postgres → backfill path.
func (q *Ships) EvictCacheShip(ctx context.Context, kvContext, shipID string) error {
	err := q.kv.Delete(ctx, kvContext, "ship."+shipID)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.ErrNotFound
	}
	return err
}
