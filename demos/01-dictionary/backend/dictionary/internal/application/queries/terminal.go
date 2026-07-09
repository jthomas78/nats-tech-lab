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

// Terminal reads the container projection from the container-{context} KV
// bucket. Because ContainerState models location as two explicit nullable
// fields, each query filters exactly one field — no branching on status.
type Terminal struct {
	kv *kvstore.Store
}

func NewTerminal(kv *kvstore.Store) *Terminal { return &Terminal{kv: kv} }

// List returns every container state in the context's KV bucket.
func (q *Terminal) List(ctx context.Context, kvContext string) ([]domain.ContainerState, error) {
	keys, err := q.kv.Keys(ctx, kvContext)
	if err != nil {
		return nil, err
	}
	containers := make([]domain.ContainerState, 0, len(keys))
	for _, key := range keys {
		value, _, err := q.kv.Get(ctx, kvContext, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var state domain.ContainerState
		if err := json.Unmarshal(value, &state); err != nil {
			return nil, fmt.Errorf("unmarshal kv value %s: %w", key, err)
		}
		containers = append(containers, state)
	}
	return containers, nil
}

// ListByPort returns the containers currently in the terminal yard at port.
func (q *Terminal) ListByPort(ctx context.Context, kvContext, port string) ([]domain.ContainerState, error) {
	all, err := q.List(ctx, kvContext)
	if err != nil {
		return nil, err
	}
	inYard := make([]domain.ContainerState, 0, len(all))
	for _, c := range all {
		if c.TerminalPort != nil && *c.TerminalPort == port {
			inYard = append(inYard, c)
		}
	}
	return inYard, nil
}

// ListByShip returns the containers currently on the named ship — this IS the
// ship's manifest (the join the ship aggregate no longer carries itself).
func (q *Terminal) ListByShip(ctx context.Context, kvContext, shipID string) ([]domain.ContainerState, error) {
	all, err := q.List(ctx, kvContext)
	if err != nil {
		return nil, err
	}
	manifest := make([]domain.ContainerState, 0, len(all))
	for _, c := range all {
		if c.OnShipID != nil && *c.OnShipID == shipID {
			manifest = append(manifest, c)
		}
	}
	return manifest, nil
}
