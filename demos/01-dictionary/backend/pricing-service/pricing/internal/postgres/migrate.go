package postgres

import (
	"context"
	"database/sql"
)

// Migrate creates the pricing schema and its tables if they don't already
// exist. Own schema, own tables, own Postgres instance — no datastore of
// any kind is shared with shipping-service or refdata-service (this domain
// is write-adjacent in the source system it was ported from, unlike
// refdata-service's read-only boundary — see BUSINESS_RULES-PRICING.md).
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS pricing`,

		// FeeScale (BR-P01–BR-P06).
		`CREATE TABLE IF NOT EXISTS pricing.fee_scales (
			context    TEXT        NOT NULL,
			name       TEXT        NOT NULL,
			deleted    BOOLEAN     NOT NULL DEFAULT false,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (context, name)
		)`,
		`CREATE TABLE IF NOT EXISTS pricing.fee_scale_versions (
			context        TEXT        NOT NULL,
			fee_scale_name TEXT        NOT NULL,
			version        INTEGER     NOT NULL CHECK (version > 0),
			status         TEXT        NOT NULL CHECK (status IN ('draft', 'published', 'rolled-back')),
			parent_version INTEGER,
			rolled_back_by INTEGER,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			published_at   TIMESTAMPTZ,
			PRIMARY KEY (context, fee_scale_name, version),
			FOREIGN KEY (context, fee_scale_name) REFERENCES pricing.fee_scales(context, name)
		)`,
		// The partial index is the concurrency-safe database backstop for BR-P02.
		`CREATE UNIQUE INDEX IF NOT EXISTS one_fee_scale_draft
		 ON pricing.fee_scale_versions(context, fee_scale_name) WHERE status = 'draft'`,
		`CREATE TABLE IF NOT EXISTS pricing.fee_scale_ranges (
			context          TEXT             NOT NULL,
			fee_scale_name   TEXT             NOT NULL,
			version          INTEGER          NOT NULL,
			cent_lower_limit BIGINT           NOT NULL,
			cent_upper_limit BIGINT           NOT NULL,
			rate_type        TEXT             NOT NULL CHECK (rate_type IN ('flat', 'percentage')),
			cent_fee         BIGINT           NOT NULL DEFAULT 0,
			percentage_fee   DOUBLE PRECISION NOT NULL DEFAULT 0,
			FOREIGN KEY (context, fee_scale_name, version) REFERENCES pricing.fee_scale_versions(context, fee_scale_name, version) ON DELETE CASCADE
		)`,

		// RateSheet (BR-P07–BR-P12).
		`CREATE TABLE IF NOT EXISTS pricing.rate_sheets (
			context      TEXT        NOT NULL,
			name         TEXT        NOT NULL,
			customer_key TEXT        NOT NULL,
			type         TEXT        NOT NULL CHECK (type IN ('normal', 'fixed-rate')),
			active       BOOLEAN     NOT NULL DEFAULT true,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (context, name)
		)`,
		`CREATE TABLE IF NOT EXISTS pricing.rate_sheet_versions (
			context            TEXT        NOT NULL,
			rate_sheet_name    TEXT        NOT NULL,
			version            INTEGER     NOT NULL CHECK (version > 0),
			status             TEXT        NOT NULL CHECK (status IN ('draft', 'published', 'rolled-back')),
			parent_version     INTEGER,
			rolled_back_by     INTEGER,
			fee_scale_override TEXT,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
			published_at       TIMESTAMPTZ,
			PRIMARY KEY (context, rate_sheet_name, version),
			FOREIGN KEY (context, rate_sheet_name) REFERENCES pricing.rate_sheets(context, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS one_rate_sheet_draft
		 ON pricing.rate_sheet_versions(context, rate_sheet_name) WHERE status = 'draft'`,
		`CREATE TABLE IF NOT EXISTS pricing.rate_sheet_entries (
			context                   TEXT    NOT NULL,
			rate_sheet_name           TEXT    NOT NULL,
			version                   INTEGER NOT NULL,
			route_key                 TEXT    NOT NULL,
			vehicle_type              TEXT    NOT NULL,
			cent_base_rate            BIGINT  NOT NULL,
			drop_point_count          INTEGER NOT NULL DEFAULT 0,
			cent_additional_drop_rate BIGINT  NOT NULL DEFAULT 0,
			PRIMARY KEY (context, rate_sheet_name, version, route_key, vehicle_type),
			FOREIGN KEY (context, rate_sheet_name, version) REFERENCES pricing.rate_sheet_versions(context, rate_sheet_name, version) ON DELETE CASCADE
		)`,

		// FixedRate (BR-P13–BR-P15).
		`CREATE TABLE IF NOT EXISTS pricing.fixed_rates (
			context      TEXT        NOT NULL,
			name         TEXT        NOT NULL,
			customer_key TEXT        NOT NULL,
			route_key    TEXT        NOT NULL,
			active       BOOLEAN     NOT NULL DEFAULT true,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (context, name)
		)`,
		`CREATE TABLE IF NOT EXISTS pricing.fixed_rate_versions (
			context                   TEXT        NOT NULL,
			fixed_rate_name           TEXT        NOT NULL,
			version                   INTEGER     NOT NULL CHECK (version > 0),
			status                    TEXT        NOT NULL CHECK (status IN ('draft', 'published', 'rolled-back')),
			parent_version            INTEGER,
			rolled_back_by            INTEGER,
			cent_rate                 BIGINT      NOT NULL,
			point_count               INTEGER     NOT NULL DEFAULT 0,
			cent_additional_drop_rate BIGINT      NOT NULL DEFAULT 0,
			created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
			published_at              TIMESTAMPTZ,
			PRIMARY KEY (context, fixed_rate_name, version),
			FOREIGN KEY (context, fixed_rate_name) REFERENCES pricing.fixed_rates(context, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS one_fixed_rate_draft
		 ON pricing.fixed_rate_versions(context, fixed_rate_name) WHERE status = 'draft'`,

		// Phase 25i — diesel overlay additions.
		// ALTER TABLE is idempotent via IF NOT EXISTS — safe to run on an
		// existing schema that pre-dates this phase.
		`ALTER TABLE pricing.rate_sheet_versions
		 ADD COLUMN IF NOT EXISTS minor_version INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pricing.rate_sheet_entries
		 ADD COLUMN IF NOT EXISTS diesel_pct DOUBLE PRECISION NOT NULL DEFAULT 0`,
		`ALTER TABLE pricing.rate_sheet_entries
		 ADD COLUMN IF NOT EXISTS initial_diesel_cents BIGINT NOT NULL DEFAULT 0`,

		// Diesel price index (BR-P18): one row per context × active_date.
		// CoastalCents and InlandCents are ZAR cents per litre.
		`CREATE TABLE IF NOT EXISTS pricing.diesel_prices (
			context       TEXT        NOT NULL,
			active_date   DATE        NOT NULL,
			coastal_cents BIGINT      NOT NULL,
			inland_cents  BIGINT      NOT NULL DEFAULT 0,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (context, active_date)
		)`,

		// Effective-dated adjusted-rate overlay (BR-P20). One row per
		// context × rate_sheet_name × version × minor_version × route_key ×
		// vehicle_type.  end_date is exclusive (the next overlay's start_date);
		// NULL means "currently in effect."
		`CREATE TABLE IF NOT EXISTS pricing.rate_sheet_overlays (
			context             TEXT    NOT NULL,
			rate_sheet_name     TEXT    NOT NULL,
			version             INTEGER NOT NULL,
			minor_version       INTEGER NOT NULL,
			route_key           TEXT    NOT NULL,
			vehicle_type        TEXT    NOT NULL,
			start_date          DATE    NOT NULL,
			end_date            DATE,
			cent_adjusted_rate  BIGINT  NOT NULL,
			PRIMARY KEY (context, rate_sheet_name, version, minor_version, route_key, vehicle_type),
			FOREIGN KEY (context, rate_sheet_name, version)
				REFERENCES pricing.rate_sheet_versions(context, rate_sheet_name, version) ON DELETE CASCADE
		)`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
