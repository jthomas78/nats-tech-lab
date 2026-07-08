package domain

import "context"

// ShipRepository is the port for the Shape B canonical projection in Postgres.
type ShipRepository interface {
	// Upsert inserts or updates the ship row and returns the stored state.
	Upsert(ctx context.Context, state ShipState) (ShipState, error)

	// Find returns ErrNotFound when no row exists for the given context + shipID.
	Find(ctx context.Context, kvContext, shipID string) (ShipState, error)

	// List returns every ship in the given fleet context.
	List(ctx context.Context, kvContext string) ([]ShipState, error)
}
