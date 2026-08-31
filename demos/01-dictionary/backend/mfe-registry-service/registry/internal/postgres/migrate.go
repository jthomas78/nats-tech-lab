// Package postgres is the registry's source of truth. Its schema is the
// registry's own — no table here joins to an accounts-service table, so the
// module could be lifted into its own service without untangling a foreign
// key (decision 39).
package postgres

import (
	"context"
	"database/sql"
)

// Migrate creates the registry schema and its tables if they don't already
// exist.
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS registry`,

		// One row per curated plugin. The entry is stored as a document
		// rather than shredded into columns on purpose: the shell validates
		// the shape, and a column per contribution field would give the two
		// sides two definitions of one contract to drift apart. `enabled` is
		// lifted out because it is the only field a write toggles on its own
		// (BR-AS24). Lifecycle is also lifted below for Admin filtering/sorting.
		`CREATE TABLE IF NOT EXISTS registry.entries (
			id         TEXT        NOT NULL PRIMARY KEY,
			enabled    BOOLEAN     NOT NULL DEFAULT true,
			entry      JSONB       NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// Lift lifecycle beside enabled: Admin will filter/sort by it. Empty
		// preserves unclassified legacy rows; it must never default to dynamic.
		`ALTER TABLE registry.entries ADD COLUMN IF NOT EXISTS lifecycle TEXT NOT NULL DEFAULT ''`,

		// The revision is a single row, not a sequence: it must be readable,
		// lockable and comparable inside the same transaction as the write
		// it keys (BR-AS17, BR-AS18), and a sequence gives none of that.
		// `only_row` pins the table to one row at the schema level.
		`CREATE TABLE IF NOT EXISTS registry.revision (
			only_row   BOOLEAN     NOT NULL PRIMARY KEY DEFAULT true CHECK (only_row),
			revision   BIGINT      NOT NULL DEFAULT 0 CHECK (revision >= 0),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO registry.revision (only_row, revision) VALUES (true, 0) ON CONFLICT DO NOTHING`,

		// Append-only: BR-AS23 wants every write, accepted or refused, to
		// leave a row. Shape mirrors accounts.AuditEntry deliberately
		// (decision 31) — the shape is reused, the table never is.
		`CREATE TABLE IF NOT EXISTS registry.audit (
			id         BIGSERIAL   PRIMARY KEY,
			revision   BIGINT,
			op         TEXT        NOT NULL,
			entry_id   TEXT        NOT NULL,
			actor      TEXT        NOT NULL,
			outcome    TEXT        NOT NULL,
			detail     TEXT        NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS registry_audit_created_at ON registry.audit(created_at DESC)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
