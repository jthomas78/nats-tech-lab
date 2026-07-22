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

	// Phase 8.3: the container's identity is a surrogate key (UUID) in the id
	// column — the primary key — while container_id (the ISO 6346 natural key)
	// keeps its own uniqueness constraint. Fresh installs get this shape
	// directly; the plan calls for `docker compose down -v` when adopting it, so
	// no in-place data migration off the old (context, container_id) PK is
	// attempted here.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS containers (
			context       TEXT        NOT NULL,
			id            TEXT        NOT NULL,
			container_id  TEXT        NOT NULL,
			cargo         TEXT        NOT NULL DEFAULT '',
			origin_port   TEXT        NOT NULL DEFAULT '',
			dest_port     TEXT        NOT NULL DEFAULT '',
			status        TEXT        NOT NULL,
			terminal_port TEXT,
			on_ship_id    TEXT,
			updated_at    TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (context, id),
			UNIQUE (context, container_id)
		)`)
	if err != nil {
		return fmt.Errorf("migrate containers: %w", err)
	}

	// Ports are plain reference/master data (BR-017, BR-018), not an
	// event-sourced aggregate: registered once via POST /api/ports, looked up
	// by Ship/Container commands. Context-scoped like every other lookup here.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS ports (
			context    TEXT        NOT NULL,
			name       TEXT        NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (context, name)
		)`)
	if err != nil {
		return fmt.Errorf("migrate ports: %w", err)
	}
	if err := seedDefaultPorts(ctx, db); err != nil {
		return fmt.Errorf("seed ports: %w", err)
	}
	return nil
}

// seedDefaultPorts pre-registers the demo's original fixed port list (formerly
// hardcoded as BASE_PORTS in ShippingForm.vue) for every fleet context the
// frontends offer, so a fresh install still has a working set of ports without
// a manual registration step. Idempotent — ON CONFLICT DO NOTHING.
func seedDefaultPorts(ctx context.Context, db *sql.DB) error {
	contexts := []string{"global", "atlantic-fleet", "pacific-fleet"}
	defaults := []string{"Hamburg", "Rotterdam", "Singapore", "New York", "Shanghai", "Sydney"}
	for _, kvContext := range contexts {
		for _, name := range defaults {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO ports (context, name) VALUES ($1, $2)
				ON CONFLICT (context, name) DO NOTHING`,
				kvContext, name); err != nil {
				return err
			}
		}
	}
	return nil
}
