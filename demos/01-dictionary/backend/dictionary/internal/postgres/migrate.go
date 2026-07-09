package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrate applies the canonical projection schemas. Idempotent; good enough
// for a POC in place of a real migration tool.
func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS ships (
			context      TEXT        NOT NULL,
			ship_id      TEXT        NOT NULL,
			ship_name    TEXT        NOT NULL DEFAULT '',
			current_port TEXT        NOT NULL DEFAULT '',
			updated_at   TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (context, ship_id)
		)`)
	if err != nil {
		return fmt.Errorf("migrate ships: %w", err)
	}

	// Phase 8: cargo moved off the ship projection — the manifest is now the
	// container join (on_ship_id). Drop the legacy column if it exists.
	if _, err := db.ExecContext(ctx, `ALTER TABLE ships DROP COLUMN IF EXISTS cargo`); err != nil {
		return fmt.Errorf("migrate ships (drop cargo): %w", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS containers (
			context       TEXT        NOT NULL,
			container_id  TEXT        NOT NULL,
			cargo         TEXT        NOT NULL DEFAULT '',
			origin_port   TEXT        NOT NULL DEFAULT '',
			dest_port     TEXT        NOT NULL DEFAULT '',
			status        TEXT        NOT NULL,
			terminal_port TEXT,
			on_ship_id    TEXT,
			updated_at    TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (context, container_id)
		)`)
	if err != nil {
		return fmt.Errorf("migrate containers: %w", err)
	}
	return nil
}
