package postgres

import (
	"context"
	"database/sql"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
)

// PortRepository is the Postgres-backed ports reference table (BR-017,
// BR-018) — plain master data, not an event-sourced projection.
type PortRepository struct {
	db *sql.DB
}

func NewPortRepository(db *sql.DB) *PortRepository { return &PortRepository{db: db} }

func (r *PortRepository) Exists(ctx context.Context, kvContext, name string) (bool, error) {
	var exists bool
	// TODO(tenant-scoping): ports should be tenant-scoped, not BU-scoped.
	// Fall back to _default_bu so seeded ports resolve for any BU context.
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM ports WHERE (context = $1 OR context = '_default_bu') AND name = $2)`,
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
	// TODO(tenant-scoping): same fallback as Exists — include _default_bu ports.
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT name FROM ports WHERE context = $1 OR context = '_default_bu' ORDER BY name`, kvContext)
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

func (r *PortRepository) ListRecords(ctx context.Context, kvContext string) ([]domain.PortRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name, created_at FROM ports WHERE context = $1 ORDER BY name`, kvContext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []domain.PortRecord{}
	for rows.Next() {
		var rec domain.PortRecord
		if err := rows.Scan(&rec.Name, &rec.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}
