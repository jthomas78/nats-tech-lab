// Package postgres implements the Shape B canonical projection repository
// for the shipping domain.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Upsert(ctx context.Context, state domain.ShipState) (domain.ShipState, error) {
	cargo, err := json.Marshal(state.Cargo)
	if err != nil {
		return domain.ShipState{}, fmt.Errorf("marshal cargo: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO ships (context, ship_id, ship_name, current_port, cargo, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (context, ship_id) DO UPDATE
		SET ship_name    = EXCLUDED.ship_name,
		    current_port = EXCLUDED.current_port,
		    cargo        = EXCLUDED.cargo,
		    updated_at   = EXCLUDED.updated_at`,
		state.Context, state.ShipID, state.ShipName, state.CurrentPort, cargo, state.UpdatedAt)
	if err != nil {
		return domain.ShipState{}, fmt.Errorf("upsert ship: %w", err)
	}
	return state, nil
}

func (r *Repository) Find(ctx context.Context, kvContext, shipID string) (domain.ShipState, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT context, ship_id, ship_name, current_port, cargo, updated_at
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
		SELECT context, ship_id, ship_name, current_port, cargo, updated_at
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
	var cargoJSON []byte
	if err := scan(&state.Context, &state.ShipID, &state.ShipName, &state.CurrentPort, &cargoJSON, &state.UpdatedAt); err != nil {
		return domain.ShipState{}, err
	}
	if err := json.Unmarshal(cargoJSON, &state.Cargo); err != nil {
		return domain.ShipState{}, fmt.Errorf("unmarshal cargo: %w", err)
	}
	if state.Cargo == nil {
		state.Cargo = []domain.Cargo{}
	}
	return state, nil
}
