package postgres

import (
	"context"
	"database/sql"
)

// PortRepository is the Postgres-backed ports reference table (BR-017,
// BR-018) — plain master data, not an event-sourced projection.
type PortRepository struct {
	db *sql.DB
}

func NewPortRepository(db *sql.DB) *PortRepository { return &PortRepository{db: db} }

func (r *PortRepository) Exists(ctx context.Context, kvContext, name string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM ports WHERE context = $1 AND name = $2)`,
		kvContext, name).Scan(&exists)
	return exists, err
}

func (r *PortRepository) Register(ctx context.Context, kvContext, name string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ports (context, name) VALUES ($1, $2)
		ON CONFLICT (context, name) DO NOTHING`,
		kvContext, name)
	return err
}

func (r *PortRepository) List(ctx context.Context, kvContext string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name FROM ports WHERE context = $1 ORDER BY name`, kvContext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ports := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		ports = append(ports, name)
	}
	return ports, rows.Err()
}
