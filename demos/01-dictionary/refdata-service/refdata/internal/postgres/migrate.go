package postgres

import (
	"context"
	"database/sql"
)

// Migrate creates the refdata schema and its tables if they don't already
// exist. Own schema, own tables — no changes to the shipping backend's
// tables in the same Postgres instance.
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS refdata`,

		`CREATE TABLE IF NOT EXISTS refdata.dictionary_types (
			type_key    TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT ''
		)`,

		// BR-D09 (Phase 11.7): category is a controlled vocabulary validated in
		// the domain layer (domain.ValidateCategory), not a DB CHECK constraint —
		// consistent with how ItemStatus is validated in Go, not SQL.
		`ALTER TABLE refdata.dictionary_types ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'standards'`,

		// No per-item version column: versioning is a property of the type's
		// whole set (BR-D04), tracked in dictionary_set_versions and stamped
		// onto the KV cache entry (kvcache.Entry.Version) — not on the row.
		`CREATE TABLE IF NOT EXISTS refdata.dictionary_items (
			context    TEXT        NOT NULL,
			type_key   TEXT        NOT NULL REFERENCES refdata.dictionary_types(type_key),
			code       TEXT        NOT NULL,
			status     TEXT        NOT NULL DEFAULT 'active',
			attrs      JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (context, type_key, code)
		)`,

		`ALTER TABLE refdata.dictionary_items DROP COLUMN IF EXISTS version`,

		`CREATE TABLE IF NOT EXISTS refdata.dictionary_localizations (
			context     TEXT NOT NULL,
			type_key    TEXT NOT NULL,
			code        TEXT NOT NULL,
			locale      TEXT NOT NULL,
			label       TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			source      TEXT NOT NULL DEFAULT 'manual',
			PRIMARY KEY (context, type_key, code, locale),
			FOREIGN KEY (context, type_key, code) REFERENCES refdata.dictionary_items (context, type_key, code) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS refdata.dictionary_references (
			context       TEXT        NOT NULL,
			from_type_key TEXT        NOT NULL,
			from_code     TEXT        NOT NULL,
			relation      TEXT        NOT NULL,
			to_type_key   TEXT        NOT NULL,
			to_code       TEXT        NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (context, from_type_key, from_code, relation),
			FOREIGN KEY (context, from_type_key, from_code) REFERENCES refdata.dictionary_items (context, type_key, code) ON DELETE CASCADE,
			FOREIGN KEY (context, to_type_key, to_code) REFERENCES refdata.dictionary_items (context, type_key, code)
		)`,

		`CREATE TABLE IF NOT EXISTS refdata.dictionary_locales (
			context    TEXT    NOT NULL,
			locale     TEXT    NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT false,
			PRIMARY KEY (context, locale)
		)`,

		`CREATE TABLE IF NOT EXISTS refdata.dictionary_set_versions (
			context  TEXT    NOT NULL,
			type_key TEXT    NOT NULL,
			version  INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (context, type_key)
		)`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
