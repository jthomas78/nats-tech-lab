package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrate applies the canonical projection schemas. Idempotent; good enough
// for a POC in place of a real migration tool.
func Migrate(ctx context.Context, db *sql.DB) error {
	// Ship's identity is a surrogate key (UUID) in the id column — the primary
	// key — while ship_id (the mutable natural key: call-sign / fleet code)
	// keeps its own uniqueness constraint and is correctable via CorrectShipID
	// without needing to rekey this row. Fresh installs get this shape
	// directly; per this repo's convention for this class of change
	// (see the containers table below), `docker compose down -v` is required
	// when adopting it — no in-place migration off the old (context, ship_id)
	// PK is attempted here.
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS ships (
			context      TEXT        NOT NULL,
			id           TEXT        NOT NULL,
			ship_id      TEXT        NOT NULL,
			ship_name    TEXT        NOT NULL DEFAULT '',
			current_port TEXT        NOT NULL DEFAULT '',
			updated_at   TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (context, id),
			UNIQUE (context, ship_id)
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
//
// Phase 16e: these are the fully-qualified context values (acme,
// acme-atlantic-fleet, acme-pacific-fleet — formerly global, atlantic-fleet,
// pacific-fleet), mirroring refdata-service's real context tree. This list is
// NOT tenant-scoped: shipping-service's Postgres schema has no tenant column
// at all today (ports/ships/containers are one shared table set for every
// tenant, keyed only by context; see Main-POC-Plan.md § Phase 16e) — tenant
// isolation for this data lives entirely in which NATS account a request
// authenticates into, not in a Postgres row. Making per-tenant context
// seeding real would require adding a tenant dimension to this schema, which
// is out of scope here.
func seedDefaultPorts(ctx context.Context, db *sql.DB) error {
	contexts := []string{"acme-pacific-fleet", "acme-atlantic-fleet"}
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
