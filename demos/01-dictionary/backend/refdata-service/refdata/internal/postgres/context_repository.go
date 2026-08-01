package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// ContextRepository persists the context tree. Cycle detection is performed
// before an upsert so database callers get a deterministic domain error.
type ContextRepository struct{ db *sql.DB }

func NewContextRepository(db *sql.DB) *ContextRepository { return &ContextRepository{db: db} }

func (r *ContextRepository) Register(ctx context.Context, value domain.Context) error {
	if value.Parent == value.Context {
		return domain.ErrContextCycle
	}
	if value.Parent != "" {
		ancestors, err := r.Ancestors(ctx, value.Parent)
		if err != nil {
			return err
		}
		for _, ancestor := range ancestors {
			if ancestor.Context == value.Context {
				return domain.ErrContextCycle
			}
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO refdata.contexts (context, parent, name, description, tenant)
		VALUES ($1, NULLIF($2, ''), $3, $4, NULLIF($5, ''))
		ON CONFLICT (context) DO UPDATE SET parent = EXCLUDED.parent, name = EXCLUDED.name, description = EXCLUDED.description, tenant = EXCLUDED.tenant`,
		value.Context, value.Parent, value.Name, value.Description, value.Tenant)
	return err
}

func (r *ContextRepository) Get(ctx context.Context, key string) (domain.Context, error) {
	var value domain.Context
	var tenant sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT context, COALESCE(parent, ''), name, description, tenant FROM refdata.contexts WHERE context = $1`, key).
		Scan(&value.Context, &value.Parent, &value.Name, &value.Description, &tenant)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Context{}, domain.ErrContextNotFound
	}
	value.Tenant = tenant.String
	return value, err
}

func (r *ContextRepository) List(ctx context.Context) ([]domain.Context, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT context, COALESCE(parent, ''), name, description, tenant FROM refdata.contexts ORDER BY context`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.Context{}
	for rows.Next() {
		var value domain.Context
		var tenant sql.NullString
		if err := rows.Scan(&value.Context, &value.Parent, &value.Name, &value.Description, &tenant); err != nil {
			return nil, err
		}
		value.Tenant = tenant.String
		values = append(values, value)
	}
	return values, rows.Err()
}

// ListByTenant returns every context whose tenant column equals tenant, plus
// every context with no tenant link at all (Phase 16f) — see the domain
// interface doc comment for why the platform roots are always included.
func (r *ContextRepository) ListByTenant(ctx context.Context, tenant string) ([]domain.Context, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, COALESCE(parent, ''), name, description, tenant
		FROM refdata.contexts WHERE tenant = $1 OR tenant IS NULL ORDER BY context`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.Context{}
	for rows.Next() {
		var value domain.Context
		var tenantCol sql.NullString
		if err := rows.Scan(&value.Context, &value.Parent, &value.Name, &value.Description, &tenantCol); err != nil {
			return nil, err
		}
		value.Tenant = tenantCol.String
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r *ContextRepository) Ancestors(ctx context.Context, key string) ([]domain.Context, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE tree AS (
			SELECT context, parent, name, description, tenant, 0 AS depth FROM refdata.contexts WHERE context = $1
			UNION ALL
			SELECT c.context, c.parent, c.name, c.description, c.tenant, tree.depth + 1
			FROM refdata.contexts c JOIN tree ON c.context = tree.parent
			WHERE tree.depth < 100
		) SELECT context, COALESCE(parent, ''), name, description, tenant FROM tree ORDER BY depth`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.Context{}
	for rows.Next() {
		var value domain.Context
		var tenant sql.NullString
		if err := rows.Scan(&value.Context, &value.Parent, &value.Name, &value.Description, &tenant); err != nil {
			return nil, err
		}
		value.Tenant = tenant.String
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, domain.ErrContextNotFound
	}
	return values, rows.Err()
}

func (r *ContextRepository) Descendants(ctx context.Context, key string) ([]domain.Context, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE tree AS (
			SELECT context, parent, name, description, tenant, 0 AS depth FROM refdata.contexts WHERE context = $1
			UNION ALL
			SELECT c.context, c.parent, c.name, c.description, c.tenant, tree.depth + 1
			FROM refdata.contexts c JOIN tree ON c.parent = tree.context
			WHERE tree.depth < 100
		) SELECT context, COALESCE(parent, ''), name, description, tenant FROM tree ORDER BY depth, context`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.Context{}
	for rows.Next() {
		var value domain.Context
		var tenant sql.NullString
		if err := rows.Scan(&value.Context, &value.Parent, &value.Name, &value.Description, &tenant); err != nil {
			return nil, err
		}
		value.Tenant = tenant.String
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, domain.ErrContextNotFound
	}
	return values, rows.Err()
}
