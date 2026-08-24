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
			id                  TEXT        PRIMARY KEY,
			context             TEXT        NOT NULL,
			name                TEXT        NOT NULL,
			type                TEXT        NOT NULL CHECK (type IN ('SHIPPER', 'TRANSPORTER')),
			status              TEXT        NOT NULL CHECK (status IN ('registered', 'active', 'suspended')),
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
			organization_id TEXT        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
			type               TEXT        NOT NULL,
			status             TEXT        NOT NULL CHECK (status IN ('FOR_REVIEW', 'APPROVED', 'REJECTED', 'SUPERSEDED')),
			document_name      TEXT        NOT NULL,
			expires_at         TIMESTAMPTZ,
			coverage_cents     BIGINT,
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (organization_id, type)
		)`,

		// FleetAsset (BR-TP12-BR-TP14). registration_no is the primary key —
		// BR-TP13's global-uniqueness invariant, not a separate surrogate ID.
		`CREATE TABLE IF NOT EXISTS organizations.fleet_assets (
			registration_no    TEXT        PRIMARY KEY,
			organization_id TEXT        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
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
			id                 TEXT        PRIMARY KEY,
			organization_id TEXT        NOT NULL,
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
		//   1. add the id column;
		//   2. drop the old (organization_id, type) PK;
		//   3. adopt the new PK.
		// Step 2 is what stops the old one-row-per-type constraint from
		// blocking BR-TP30's superseded history, so it must not be skipped.
		// Swapped only when the PK is still the old (organization_id, type)
		// shape. Guarding on the actual key columns — rather than
		// unconditionally dropping and re-adding — keeps a restart from
		// rebuilding the primary index every boot.
		//
		// BR-TP73 (ADR-051): the column is added nullable and without a
		// default, because a ULID cannot be minted by Postgres — the service
		// mints it (identity.New) before the INSERT. Step 3 is what makes it
		// NOT NULL: ADD CONSTRAINT ... PRIMARY KEY sets that implicitly, and
		// on a fresh database the table is still empty when it runs.
		`ALTER TABLE organizations.compliance_documents
			ADD COLUMN IF NOT EXISTS id TEXT`,
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

		// The organization status vocabulary was lower-cased. Postgres has no
		// ALTER CONSTRAINT for a CHECK expression, so it must be dropped and
		// re-added — and it must be done here rather than left to the CREATE
		// TABLE above, which only runs on a fresh database. An unmigrated
		// database rejects every lifecycle write with a constraint violation
		// while the code believes the transition succeeded; see the same
		// lesson written up against transporter_profiles in
		// transporterprofile/postgres/projection.go. Guarded so re-adding does
		// not force a full validation scan on every boot.
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'organizations.organizations'::regclass
				  AND conname = 'organizations_status_check'
				  AND pg_get_constraintdef(oid) LIKE '%registered%'
			) THEN
				ALTER TABLE organizations.organizations
					DROP CONSTRAINT IF EXISTS organizations_status_check;
				ALTER TABLE organizations.organizations
					ADD CONSTRAINT organizations_status_check
					CHECK (status IN ('registered', 'active', 'suspended'));
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
		// Phase 40 removed file_name from this set: the document's name lives
		// in document_name on the row itself, and a projection column holding
		// a second copy of its own row's identity can only ever drift from it.
		`ALTER TABLE organizations.compliance_documents
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
			id                 TEXT        PRIMARY KEY,
			organization_id TEXT        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
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
			organization_id TEXT        NOT NULL REFERENCES organizations.organizations(id) ON DELETE CASCADE,
			provider           TEXT        NOT NULL,
			credential_type    TEXT        NOT NULL CHECK (credential_type IN ('API_KEY', 'USERNAME_PASSWORD', 'METADATA_ONLY')),
			configured_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			-- BR-TP51: at most one credential per (transporter, provider).
			-- BR-TP54 makes re-configuring an overwrite, so this is the key
			-- the upsert conflicts on, not a constraint to work around.
			PRIMARY KEY (organization_id, provider)
		)`,

		// --- BR-TP73 / ADR-051 — UUID identity retired in favour of ULID ----
		// Every id and organization_id column above now reads TEXT rather than
		// the native UUID type, because a ULID is 26 Crockford-base32
		// characters and Postgres has no ULID type to hold it. On a fresh
		// database the CREATE TABLE statements already say TEXT and this block
		// is a no-op.
		//
		// It exists for the other case: a database created before ADR-051 has
		// uuid columns, and every INSERT the service now issues supplies a
		// 26-character ULID, which uuid rejects outright ("invalid input syntax
		// for type uuid"). Without this conversion such a database does not
		// fail at migrate time — it starts cleanly and then fails every write,
		// which is a considerably worse way to find out.
		//
		// The conversion is one DO block, so it is one transaction: the foreign
		// keys have to come off before organizations.id can change type and go
		// back on afterwards, and a half-applied version of that leaves the
		// schema without referential integrity. uuid -> text needs no USING
		// clause; Postgres renders each value as its canonical 36-character
		// hyphenated form.
		//
		// Note what this deliberately does NOT do: invent ULIDs for existing
		// rows. Those rows keep their UUID text, and they are then wrong in a
		// way no migration can fix — a TransporterProfile's id is embedded in
		// every event subject it has ever published on the LimitsPolicy
		// TRANSPORTER stream, so renumbering a row here would orphan its whole
		// history and the aggregate would silently rehydrate as empty. The
		// supported path from a pre-ADR-051 database is a reseed: drop the
		// streams and KV buckets and re-run cmd/seed-transporters. See
		// ARCHITECTURE-ORGANIZATIONS.md § "Entity identity".
		`DO $$
		DECLARE
			fk RECORD;
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'organizations'
				  AND table_name = 'organizations'
				  AND column_name = 'id'
				  AND data_type = 'uuid'
			) THEN
				RETURN;
			END IF;

			-- Drop every FK pointing at organizations.organizations(id).
			-- Discovered rather than hardcoded: the four known ones are
			-- compliance_documents, fleet_assets,
			-- transporter_operating_areas and tracking_credentials, but a
			-- fifth added later must not silently break this migration.
			FOR fk IN
				SELECT c.conname, c.conrelid::regclass AS tbl
				FROM pg_constraint c
				WHERE c.contype = 'f'
				  AND c.confrelid = 'organizations.organizations'::regclass
			LOOP
				EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', fk.tbl, fk.conname);
			END LOOP;

			ALTER TABLE organizations.organizations
				ALTER COLUMN id DROP DEFAULT,
				ALTER COLUMN id TYPE TEXT;

			ALTER TABLE organizations.compliance_documents
				ALTER COLUMN organization_id TYPE TEXT,
				ALTER COLUMN id DROP DEFAULT,
				ALTER COLUMN id TYPE TEXT;

			ALTER TABLE organizations.fleet_assets
				ALTER COLUMN organization_id TYPE TEXT;

			-- No FK on this one (the audit trail outlives the row it
			-- describes), so it is converted here but was never in the loop
			-- above.
			ALTER TABLE organizations.audit_events
				ALTER COLUMN organization_id TYPE TEXT,
				ALTER COLUMN id DROP DEFAULT,
				ALTER COLUMN id TYPE TEXT;

			ALTER TABLE organizations.transporter_operating_areas
				ALTER COLUMN organization_id TYPE TEXT,
				ALTER COLUMN id DROP DEFAULT,
				ALTER COLUMN id TYPE TEXT;

			ALTER TABLE organizations.tracking_credentials
				ALTER COLUMN organization_id TYPE TEXT;

			ALTER TABLE organizations.compliance_documents
				ADD CONSTRAINT compliance_documents_organization_id_fkey
				FOREIGN KEY (organization_id)
				REFERENCES organizations.organizations(id) ON DELETE CASCADE;
			ALTER TABLE organizations.fleet_assets
				ADD CONSTRAINT fleet_assets_organization_id_fkey
				FOREIGN KEY (organization_id)
				REFERENCES organizations.organizations(id) ON DELETE CASCADE;
			ALTER TABLE organizations.transporter_operating_areas
				ADD CONSTRAINT transporter_operating_areas_organization_id_fkey
				FOREIGN KEY (organization_id)
				REFERENCES organizations.organizations(id) ON DELETE CASCADE;
			ALTER TABLE organizations.tracking_credentials
				ADD CONSTRAINT tracking_credentials_organization_id_fkey
				FOREIGN KEY (organization_id)
				REFERENCES organizations.organizations(id) ON DELETE CASCADE;
		END $$`,

		// --- Phase 40 — every compliance document is a file -----------------
		// Three changes to one table, in one guarded block so a partially
		// applied state cannot exist:
		//
		//   1. `reference` becomes `document_name`. The column held an opaque
		//      external locator in the metadata-only v1 shape; it now holds
		//      the document's file name, which is its identity.
		//   2. PENDING leaves the status vocabulary. Registration produces
		//      FOR_REVIEW and document resubmission is retired, so nothing can
		//      write it any more.
		//   3. The document name is unique per organization.
		//
		// Rows are NOT repaired. A pre-Phase-40 database can hold PENDING
		// rows, file-less rows, and duplicate names, none of which this
		// vocabulary admits — and inventing file names for rows that never had
		// bytes would fabricate the very identity the phase makes
		// load-bearing. The supported path is `docker compose down -v` plus a
		// reseed (ADR-051's standing rule, for the same reason: the streams
		// carry the old shape too). So the rename runs, and the CHECK and the
		// unique index are added only once the data already satisfies them —
		// on a dirty database they are skipped, loudly, rather than failing
		// the whole migration and taking the service down with it.
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'organizations' AND table_name = 'compliance_documents'
				  AND column_name = 'reference'
			) AND NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'organizations' AND table_name = 'compliance_documents'
				  AND column_name = 'document_name'
			) THEN
				ALTER TABLE organizations.compliance_documents RENAME COLUMN reference TO document_name;
			END IF;

			-- The old file_name column duplicated document_name once the two
			-- were required to match. Dropping it is safe on any database this
			-- migration will meet: a pre-Phase-40 row either has no file at
			-- all, or has file_name equal to the name the reseed will mint
			-- anyway.
			ALTER TABLE organizations.compliance_documents DROP COLUMN IF EXISTS file_name;

			IF EXISTS (SELECT 1 FROM organizations.compliance_documents WHERE status = 'PENDING') THEN
				RAISE WARNING 'Phase 40: compliance_documents still holds PENDING rows; leaving the status CHECK as-is. Reseed (docker compose down -v) to adopt the Phase 40 vocabulary.';
			ELSIF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'organizations.compliance_documents'::regclass
				  AND conname = 'compliance_documents_status_check'
				  AND pg_get_constraintdef(oid) NOT LIKE '%PENDING%'
			) THEN
				ALTER TABLE organizations.compliance_documents DROP CONSTRAINT IF EXISTS compliance_documents_status_check;
				ALTER TABLE organizations.compliance_documents
					ADD CONSTRAINT compliance_documents_status_check
					CHECK (status IN ('FOR_REVIEW', 'APPROVED', 'REJECTED', 'SUPERSEDED'));
			END IF;
		END $$`,

		// The uniqueness rule itself. Deliberately NOT partial: the name is
		// read-only, so a rejected or superseded row keeps its name for good,
		// and allowing a live row to reuse it would put two indistinguishable
		// names in one organization's history. Correcting a rejected
		// `scan0001.pdf` therefore means renaming the file before re-dropping
		// it — the accepted cost of the name being the identity (Phase 40
		// decision 4).
		//
		// CREATE UNIQUE INDEX fails on a database that already holds
		// duplicates, which is the correct outcome for the same reason
		// BR-TP69's index fails rather than repairing: reseed instead.
		`CREATE UNIQUE INDEX IF NOT EXISTS compliance_documents_document_name_idx
			ON organizations.compliance_documents (organization_id, document_name)`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
