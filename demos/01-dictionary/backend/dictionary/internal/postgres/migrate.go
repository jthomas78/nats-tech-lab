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
		CREATE TABLE IF NOT EXISTS ships (
			context      TEXT        NOT NULL,
			ship_id      TEXT        NOT NULL,
			ship_name    TEXT        NOT NULL DEFAULT '',
			current_port TEXT        NOT NULL DEFAULT '',
			cargo        JSONB       NOT NULL DEFAULT '[]',
			updated_at   TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (context, ship_id)
		)`)
	if err != nil {
		return fmt.Errorf("migrate ships: %w", err)
	}
	return nil
}
