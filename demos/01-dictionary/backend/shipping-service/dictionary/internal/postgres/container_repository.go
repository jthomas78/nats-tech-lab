package postgres

import (
	"context"
	"database/sql"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
)

// ContainerRepository is the canonical container projection: one row per
// container, upserted by the container projector on every event.
type ContainerRepository struct {
	db *sql.DB
}

func NewContainerRepository(db *sql.DB) *ContainerRepository {
	return &ContainerRepository{db: db}
}

func (r *ContainerRepository) Upsert(ctx context.Context, state domain.ContainerState) (domain.ContainerState, error) {
	// Conflict target is the surrogate key (context, id): container events after
	// registration carry the same id, so loaded/unloaded updates land on the
	// same row. container_id is written too and stays unique per context.
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO containers (context, id, container_id, cargo, origin_port, dest_port, status, terminal_port, on_ship_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (context, id) DO UPDATE
		SET container_id  = EXCLUDED.container_id,
		    cargo         = EXCLUDED.cargo,
		    origin_port   = EXCLUDED.origin_port,
		    dest_port     = EXCLUDED.dest_port,
		    status        = EXCLUDED.status,
		    terminal_port = EXCLUDED.terminal_port,
		    on_ship_id    = EXCLUDED.on_ship_id,
		    updated_at    = EXCLUDED.updated_at`,
		state.Context, state.ID, state.ContainerID, state.Cargo, state.OriginPort, state.DestPort,
		state.Status, state.TerminalPort, state.OnShipID, state.UpdatedAt)
	if err != nil {
		return domain.ContainerState{}, err
	}
	return state, nil
}
