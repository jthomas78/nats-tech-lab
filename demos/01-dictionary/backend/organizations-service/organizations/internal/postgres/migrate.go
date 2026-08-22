package postgres

import (
	"context"
	"database/sql"
)

// Migrate creates the organizations schema and its tables if they don't
// already exist. Own schema, own tables, own Postgres instance — no
// datastore of any kind is shared with shipping-service, refdata-service,
// accounts-service, or pricing-service (see tenant_service_separation_decision.md).
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE SCHEMA IF NOT EXISTS organizations`,

		// Organization (BR-TP01-BR-TP05). ID is server-generated — name is
		// not a reliable natural key (not guaranteed unique per context).
		`CREATE TABLE IF NOT EXISTS organizations.organizations (
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
		`CREATE INDEX IF NOT EXISTS organizations_context_idx ON organizations.organizations (context)`,

		// ComplianceDocument (BR-TP07-BR-TP11). BR-TP08's "one per
		// (partner, type)" invariant is the primary key itself — adding a
		// document for a type that already exists is an upsert (ON CONFLICT
		// DO UPDATE in the repository), not a separate row.
		`CREATE TABLE IF NOT EXISTS organizations.compliance_documents (
			organization_id UUID        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
			type               TEXT        NOT NULL,
			status             TEXT        NOT NULL CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
			reference          TEXT        NOT NULL,
			expires_at         TIMESTAMPTZ,
			coverage_cents     BIGINT,
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (organization_id, type)
		)`,

		// FleetAsset (BR-TP12-BR-TP14). registration_no is the primary key —
		// BR-TP13's global-uniqueness invariant, not a separate surrogate ID.
		`CREATE TABLE IF NOT EXISTS organizations.fleet_assets (
			registration_no    TEXT        PRIMARY KEY,
			organization_id UUID        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
			vin                TEXT        NOT NULL DEFAULT '',
			make               TEXT        NOT NULL DEFAULT '',
			model              TEXT        NOT NULL DEFAULT '',
			vehicle_type_code  TEXT        NOT NULL,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS fleet_assets_organization_idx ON organizations.fleet_assets (organization_id)`,

		// Audit trail (BR-TP06) — append-only, mirrors accounts.audit_events
		// (BR-AC11) verbatim: no UPDATE, no DELETE, only Record and read paths.
		`CREATE TABLE IF NOT EXISTS organizations.audit_events (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			organization_id UUID        NOT NULL,
			action             TEXT        NOT NULL,
			actor              TEXT        NOT NULL,
			source_ip          TEXT        NOT NULL DEFAULT '',
			outcome            TEXT        NOT NULL CHECK (outcome IN ('success', 'failed')),
			metadata           JSONB       NOT NULL DEFAULT '{}'::jsonb,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS audit_events_organization_idx ON organizations.audit_events (organization_id, created_at DESC)`,

		// --- Phase 38c-i ---------------------------------------------------
		// Idempotent, data-preserving alterations, following the same
		// ALTER ... IF NOT EXISTS pattern as transporterprofile/postgres's
		// projection migration. Existing dev rows survive; no volume reset.

		// BR-TP33: row version. DEFAULT 1 backfills existing rows, so a
		// partner registered before this migration reads as version 1 rather
		// than 0 and the first edit's expected version is honest.
		`ALTER TABLE organizations.organizations
			ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1`,

		// BR-TP29: compliance documents gain a service-minted identity, and
		// the PK widens from (organization_id, type) to
		// (organization_id, id). Done in three idempotent steps so a
		// re-run is a no-op:
		//   1. add the id column, minting one per existing row;
		//   2. drop the old (organization_id, type) PK;
		//   3. adopt the new PK.
		// Step 2 is what stops the old one-row-per-type constraint from
		// blocking BR-TP30's superseded history, so it must not be skipped.
		// Swapped only when the PK is still the old (organization_id, type)
		// shape. Guarding on the actual key columns — rather than
		// unconditionally dropping and re-adding — keeps a restart from
		// rebuilding the primary index every boot.
		`ALTER TABLE organizations.compliance_documents
			ADD COLUMN IF NOT EXISTS id UUID NOT NULL DEFAULT gen_random_uuid()`,
		`DO $$
		DECLARE
			key_columns TEXT;
		BEGIN
			SELECT string_agg(a.attname, ',' ORDER BY a.attname)
			INTO key_columns
			FROM pg_constraint c
			JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
			WHERE c.conrelid = 'organizations.compliance_documents'::regclass
			  AND c.contype = 'p';

			IF key_columns = 'organization_id,type' THEN
				ALTER TABLE organizations.compliance_documents
					DROP CONSTRAINT compliance_documents_pkey;
				ALTER TABLE organizations.compliance_documents
					ADD CONSTRAINT compliance_documents_pkey PRIMARY KEY (organization_id, id);
			ELSIF key_columns IS NULL THEN
				ALTER TABLE organizations.compliance_documents
					ADD CONSTRAINT compliance_documents_pkey PRIMARY KEY (organization_id, id);
			END IF;
		END $$`,

		// BR-TP30: SUPERSEDED joins the status vocabulary. Postgres has no
		// ALTER CONSTRAINT for a CHECK expression, so it must be dropped and
		// re-added — but only when it does not already allow SUPERSEDED,
		// since re-adding forces a full validation scan of the table.
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'organizations.compliance_documents'::regclass
				  AND conname = 'compliance_documents_status_check'
				  AND pg_get_constraintdef(oid) LIKE '%SUPERSEDED%'
			) THEN
				ALTER TABLE organizations.compliance_documents
					DROP CONSTRAINT IF EXISTS compliance_documents_status_check;
				ALTER TABLE organizations.compliance_documents
					ADD CONSTRAINT compliance_documents_status_check
					CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'SUPERSEDED'));
			END IF;
		END $$`,

		// BR-TP31 reads only current documents, and BR-TP30 needs to find the
		// incumbent for a (partner, type) to supersede it. Both are this
		// lookup, so it earns a partial index rather than a full scan.
		`CREATE INDEX IF NOT EXISTS compliance_documents_current_idx
			ON organizations.compliance_documents (organization_id, type)
			WHERE status <> 'SUPERSEDED'`,

		// BR-TP45 (38c-ii): the projection of a document's uploaded bytes. All
		// four are nullable together — a document legitimately has no file
		// until one is uploaded, and BR-TP43 makes the transition one-way, so
		// there is no "had a file, now doesn't" state to represent.
		//
		// Storing this here rather than reading it back from the Object Store
		// keeps the listing path off the object store entirely: a Documents tab
		// renders names and sizes from one Postgres query, and the bucket is
		// touched only when bytes actually move.
		`ALTER TABLE organizations.compliance_documents
			ADD COLUMN IF NOT EXISTS file_name TEXT,
			ADD COLUMN IF NOT EXISTS file_content_type TEXT,
			ADD COLUMN IF NOT EXISTS file_size_bytes BIGINT,
			ADD COLUMN IF NOT EXISTS file_object_name TEXT,
			ADD COLUMN IF NOT EXISTS file_uploaded_at TIMESTAMPTZ`,

		// --- Phase 39a ------------------------------------------------------
		// GIT is event-sourced from this point: this table is its replay-fed
		// projection. Contact name/number are the named exception (BR-TP72):
		// commands write them directly and a replay rebuild restores NULL,
		// because immutable stream events must not contain redactable contact
		// values.
		`ALTER TABLE organizations.compliance_documents
			ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			-- JSONB, not TEXT[]: pgx's database/sql path hands a Postgres array
			-- back as a string and cannot scan it into []string, and every
			-- other structured column in this service is already JSONB
			-- (transporter_profiles.document_reviews/certificates).
			ADD COLUMN IF NOT EXISTS goods_types JSONB NOT NULL DEFAULT '[]'::jsonb,
			ADD COLUMN IF NOT EXISTS insurer_name TEXT NOT NULL DEFAULT '',
			ADD COLUMN IF NOT EXISTS insurance_contact_name TEXT,
			ADD COLUMN IF NOT EXISTS insurance_contact_number TEXT`,

		// Postgres cannot ALTER a CHECK expression. Extend it in the same
		// guarded DO-block idiom used for SUPERSEDED above.
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'organizations.compliance_documents'::regclass
				  AND conname = 'compliance_documents_status_check'
				  AND pg_get_constraintdef(oid) LIKE '%FOR_REVIEW%'
			) THEN
				ALTER TABLE organizations.compliance_documents DROP CONSTRAINT IF EXISTS compliance_documents_status_check;
				ALTER TABLE organizations.compliance_documents
					ADD CONSTRAINT compliance_documents_status_check
					CHECK (status IN ('PENDING', 'FOR_REVIEW', 'APPROVED', 'REJECTED', 'SUPERSEDED'));
			END IF;
		END $$`,

		// BR-TP69: this must fail rather than repair existing dev data. If a
		// database already has two approved rows for one type, reseed it with
		// cmd/seed-transporters; inventing a repair would fabricate provenance.
		`CREATE UNIQUE INDEX IF NOT EXISTS compliance_documents_one_approved_idx
			ON organizations.compliance_documents (organization_id, type)
			WHERE status = 'APPROVED'`,

		// --- Phase 38d-ii ---------------------------------------------------
		// Operating areas (BR-TP46-BR-TP50). Deliberately mirrors V2's own
		// denormalization: country_code is carried on every row, including
		// REGION rows where it is the parent resolved from refdata's
		// `country` relation (BR-D47). V2's unpopulated
		// TransporterOperatingAreaEntity denormalized the same field "for
		// query performance" (its own comment); here it also makes BR-TP48's
		// overlap check a single-column comparison rather than a join back
		// into refdata on every write.
		//
		// No FK to a region table exists on purpose — the corpus lives in
		// refdata-service, a separate service with its own database, so
		// membership is validated over rpc.* at write time (BR-TP47) rather
		// than by a constraint that cannot span the boundary.
		`CREATE TABLE IF NOT EXISTS organizations.transporter_operating_areas (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			organization_id UUID        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
			level              TEXT        NOT NULL CHECK (level IN ('COUNTRY', 'REGION')),
			code               TEXT        NOT NULL,
			country_code       TEXT        NOT NULL,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// BR-TP49: the same area cannot be assigned twice. Enforced by the
		// database rather than a domain guard, the same treatment BR-TP13
		// gives fleet_assets.registration_no — a read-then-write check in the
		// domain would be racy.
		`CREATE UNIQUE INDEX IF NOT EXISTS transporter_operating_areas_unique_idx
			ON organizations.transporter_operating_areas (organization_id, level, code)`,
		// BR-TP48 reads a partner's whole set on every add, and the Operating
		// Areas tab lists it; both are this lookup.
		`CREATE INDEX IF NOT EXISTS transporter_operating_areas_organization_idx
			ON organizations.transporter_operating_areas (organization_id)`,

		// Tracking credentials (BR-TP51-BR-TP55). Note what is NOT here:
		// no api_key, no password, no username, no metadata blob. V2 stores
		// exactly those as plain columns across 20 per-provider satellite
		// tables with no encryption anywhere; this table records only that a
		// provider IS configured and how, while the payload itself is sealed
		// by the service and lives only in the organizations-secrets KV
		// bucket (BR-TP52). A column added here for "just the username"
		// would defeat the whole rule.
		`CREATE TABLE IF NOT EXISTS organizations.tracking_credentials (
			organization_id UUID        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
			provider           TEXT        NOT NULL,
			credential_type    TEXT        NOT NULL CHECK (credential_type IN ('API_KEY', 'USERNAME_PASSWORD', 'METADATA_ONLY')),
			configured_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			-- BR-TP51: at most one credential per (transporter, provider).
			-- BR-TP54 makes re-configuring an overwrite, so this is the key
			-- the upsert conflicts on, not a constraint to work around.
			PRIMARY KEY (organization_id, provider)
		)`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
