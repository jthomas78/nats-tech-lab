package postgres

import (
	"context"
	"database/sql"
)

// Migrate creates the trading_partner schema and its tables if they don't
// already exist. Own schema, own tables, own Postgres instance — no
// datastore of any kind is shared with shipping-service, refdata-service,
// accounts-service, or pricing-service (see tenant_service_separation_decision.md).
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE SCHEMA IF NOT EXISTS trading_partner`,

		// TradingPartner (BR-TP01-BR-TP05). ID is server-generated — name is
		// not a reliable natural key (not guaranteed unique per context).
		`CREATE TABLE IF NOT EXISTS trading_partner.trading_partners (
			id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			context             TEXT        NOT NULL,
			name                TEXT        NOT NULL,
			type                TEXT        NOT NULL CHECK (type IN ('SHIPPER', 'TRANSPORTER')),
			status              TEXT        NOT NULL CHECK (status IN ('REGISTERED', 'ACTIVE', 'SUSPENDED')),
			trading_as          TEXT        NOT NULL DEFAULT '',
			company_name        TEXT        NOT NULL DEFAULT '',
			registration_no     TEXT        NOT NULL DEFAULT '',
			vat_registration_no TEXT        NOT NULL DEFAULT '',
			created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS trading_partners_context_idx ON trading_partner.trading_partners (context)`,

		// ComplianceDocument (BR-TP07-BR-TP11). BR-TP08's "one per
		// (partner, type)" invariant is the primary key itself — adding a
		// document for a type that already exists is an upsert (ON CONFLICT
		// DO UPDATE in the repository), not a separate row.
		`CREATE TABLE IF NOT EXISTS trading_partner.compliance_documents (
			trading_partner_id UUID        NOT NULL REFERENCES trading_partner.trading_partners(id) ON DELETE CASCADE,
			type               TEXT        NOT NULL,
			status             TEXT        NOT NULL CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
			reference          TEXT        NOT NULL,
			expires_at         TIMESTAMPTZ,
			coverage_cents     BIGINT,
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (trading_partner_id, type)
		)`,

		// FleetAsset (BR-TP12-BR-TP14). registration_no is the primary key —
		// BR-TP13's global-uniqueness invariant, not a separate surrogate ID.
		`CREATE TABLE IF NOT EXISTS trading_partner.fleet_assets (
			registration_no    TEXT        PRIMARY KEY,
			trading_partner_id UUID        NOT NULL REFERENCES trading_partner.trading_partners(id) ON DELETE CASCADE,
			vin                TEXT        NOT NULL DEFAULT '',
			make               TEXT        NOT NULL DEFAULT '',
			model              TEXT        NOT NULL DEFAULT '',
			vehicle_type_code  TEXT        NOT NULL,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS fleet_assets_partner_idx ON trading_partner.fleet_assets (trading_partner_id)`,

		// Audit trail (BR-TP06) — append-only, mirrors accounts.audit_events
		// (BR-AC11) verbatim: no UPDATE, no DELETE, only Record and read paths.
		`CREATE TABLE IF NOT EXISTS trading_partner.audit_events (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			trading_partner_id UUID        NOT NULL,
			action             TEXT        NOT NULL,
			actor              TEXT        NOT NULL,
			source_ip          TEXT        NOT NULL DEFAULT '',
			outcome            TEXT        NOT NULL CHECK (outcome IN ('success', 'failed')),
			metadata           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS audit_events_partner_idx ON trading_partner.audit_events (trading_partner_id, created_at DESC)`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
