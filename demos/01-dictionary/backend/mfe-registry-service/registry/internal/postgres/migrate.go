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

		// The publisher's signed bytes, base64, in a column of their own
		// (BR-AS37, decisions 68 and 101). Deliberately NOT the `entry`
		// column and deliberately not JSONB: JSONB reorders keys and strips
		// whitespace, which is exactly what invalidates a signature. `entry`
		// stays the queryable projection; this is the artifact. Empty is the
		// normal state of a curated or preload row, which nobody signed.
		`ALTER TABLE registry.entries ADD COLUMN IF NOT EXISTS manifest TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE registry.entries ADD COLUMN IF NOT EXISTS signature TEXT NOT NULL DEFAULT ''`,
		// The key that actually signed, not only the publisher holding it
		// (decision 103): a revocation re-evaluates the entries one key
		// signed, and a publisher rotating keys has several.
		`ALTER TABLE registry.entries ADD COLUMN IF NOT EXISTS signing_key TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS registry_entries_signing_key ON registry.entries(signing_key)`,
		// Withheld is its own column rather than a flavour of enabled=false,
		// because 7e has to tell "never reviewed" apart from "we took this
		// away": only the second unloads a plugin from a running shell.
		`ALTER TABLE registry.entries ADD COLUMN IF NOT EXISTS withheld BOOLEAN NOT NULL DEFAULT false`,

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
		// One audit trail, two revision counters. `scope` says which counter
		// a row's revision belongs to, so an operator reads one history
		// instead of two and the numbers still mean something (BR-AS38).
		`ALTER TABLE registry.audit ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'registry'`,

		// The trusted-publishers table (BR-AS38, decisions 69, 97, 103). Three
		// tables, not one document, because each is written on its own: a key
		// change must not be able to rewrite an ownership list by accident,
		// which is exactly what a single JSON body would allow.
		`CREATE TABLE IF NOT EXISTS registry.publishers (
			id         TEXT        NOT NULL PRIMARY KEY,
			name       TEXT        NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// No delete: trust is withdrawn by moving a key to `revoked`, and the
		// row stays so an entry that key signed can still be attributed.
		`CREATE TABLE IF NOT EXISTS registry.publisher_keys (
			public_key   TEXT        NOT NULL PRIMARY KEY,
			publisher_id TEXT        NOT NULL REFERENCES registry.publishers(id),
			state        TEXT        NOT NULL,
			added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			changed_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS registry_publisher_keys_publisher ON registry.publisher_keys(publisher_id)`,
		// Ownership is its own table keyed on the plugin id, so a plugin has
		// exactly one owner by construction rather than by convention.
		`CREATE TABLE IF NOT EXISTS registry.plugin_owners (
			plugin_id    TEXT        NOT NULL PRIMARY KEY,
			publisher_id TEXT        NOT NULL REFERENCES registry.publishers(id),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// The trust table carries its own revision. Adding a key has not
		// changed the catalogue, and bumping the plugin document's revision
		// for it would make every shell re-read for nothing. The counters
		// meet in 7d, where a revocation does withhold entries.
		`CREATE TABLE IF NOT EXISTS registry.publisher_revision (
			only_row   BOOLEAN     NOT NULL PRIMARY KEY DEFAULT true CHECK (only_row),
			revision   BIGINT      NOT NULL DEFAULT 0 CHECK (revision >= 0),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO registry.publisher_revision (only_row, revision) VALUES (true, 0) ON CONFLICT DO NOTHING`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
