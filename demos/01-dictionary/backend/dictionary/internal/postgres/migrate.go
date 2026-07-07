package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrate applies the Shape B projection schema. Idempotent; good enough for
// a POC in place of a real migration tool.
func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS dictionary_entries (
			context     TEXT        NOT NULL,
			entity_type TEXT        NOT NULL,
			id          TEXT        NOT NULL,
			label       TEXT        NOT NULL,
			attributes  JSONB       NOT NULL DEFAULT '{}',
			version     INT         NOT NULL DEFAULT 1,
			created_at  TIMESTAMPTZ NOT NULL,
			updated_at  TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (context, entity_type, id)
		)`)
	if err != nil {
		return fmt.Errorf("migrate dictionary_entries: %w", err)
	}
	return nil
}
