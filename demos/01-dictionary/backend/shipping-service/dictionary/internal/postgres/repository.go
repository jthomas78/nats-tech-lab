// Package postgres implements the canonical projection repositories for the
// shipping domain (Shape B ships table, containers table).
package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Upsert(ctx context.Context, state domain.ShipState) (domain.ShipState, error) {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ships (context, ship_id, ship_name, current_port, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (context, ship_id) DO UPDATE
		SET ship_name    = EXCLUDED.ship_name,
		    current_port = EXCLUDED.current_port,
		    updated_at   = EXCLUDED.updated_at`,
		state.Context, state.ShipID, state.ShipName, state.CurrentPort, state.UpdatedAt)
	if err != nil {
		return domain.ShipState{}, err
	}
	return state, nil
}

func (r *Repository) Find(ctx context.Context, kvContext, shipID string) (domain.ShipState, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT context, ship_id, ship_name, current_port, updated_at
		FROM ships
		WHERE context = $1 AND ship_id = $2`,
		kvContext, shipID)
	state, err := scanShip(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ShipState{}, domain.ErrNotFound
	}
	return state, err
}

func (r *Repository) List(ctx context.Context, kvContext string) ([]domain.ShipState, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, ship_id, ship_name, current_port, updated_at
		FROM ships
		WHERE context = $1
		ORDER BY ship_id`,
		kvContext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ships []domain.ShipState
	for rows.Next() {
		state, err := scanShip(rows.Scan)
		if err != nil {
			return nil, err
		}
		ships = append(ships, state)
	}
	return ships, rows.Err()
}

func scanShip(scan func(...any) error) (domain.ShipState, error) {
	var state domain.ShipState
	if err := scan(&state.Context, &state.ShipID, &state.ShipName, &state.CurrentPort, &state.UpdatedAt); err != nil {
		return domain.ShipState{}, err
	}
	if state.CurrentPort != "" {
		state.Status = domain.StatusDocked
	} else {
		state.Status = domain.StatusInTransit
	}
	return state, nil
}
