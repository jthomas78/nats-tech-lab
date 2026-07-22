package domain

import (
	"context"
	"time"
)

// ShipRepository is the port for the Shape B canonical projection in Postgres.
type ShipRepository interface {
	// Upsert inserts or updates the ship row and returns the stored state.
	Upsert(ctx context.Context, state ShipState) (ShipState, error)

	// Find returns ErrNotFound when no row exists for the given context + shipID.
	Find(ctx context.Context, kvContext, shipID string) (ShipState, error)

	// List returns every ship in the given fleet context.
	List(ctx context.Context, kvContext string) ([]ShipState, error)
}

// ContainerRepository is the port for the canonical container projection in
// Postgres. The projector upserts on every container event; reads are served
// from the container KV bucket, so no query methods are needed here.
type ContainerRepository interface {
	// Upsert inserts or updates the container row and returns the stored state.
	Upsert(ctx context.Context, state ContainerState) (ContainerState, error)
}

// PortRepository is the port for the ports reference table in Postgres — plain
// master data (not an event-sourced aggregate): a port is registered once and
// looked up by Ship/Container commands to enforce BR-017/BR-018.
type PortRepository interface {
	// Exists reports whether name is registered in the given fleet context.
	Exists(ctx context.Context, kvContext, name string) (bool, error)

	// Register adds name to the fleet context's ports registry. Idempotent.
	Register(ctx context.Context, kvContext, name string) error

	// List returns every registered port in the fleet context, sorted.
	List(ctx context.Context, kvContext string) ([]string, error)

	// ListRecords returns every registered port as a full row (name +
	// registration time), sorted by name — the raw-table view for the admin
	// Postgres Tables panel, as opposed to List's dropdown-friendly names.
	ListRecords(ctx context.Context, kvContext string) ([]PortRecord, error)
}

// PortRecord is one row of the ports reference table.
type PortRecord struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}
