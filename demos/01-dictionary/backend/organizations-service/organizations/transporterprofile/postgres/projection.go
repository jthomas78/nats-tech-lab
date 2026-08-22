// Package postgres is the canonical TransporterProfile Shape-B projection.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

type Projection struct{ db *sql.DB }

func NewProjection(db *sql.DB) *Projection { return &Projection{db: db} }

func (p *Projection) Migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS organizations`); err != nil {
		return err
	}
	_, err := p.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS organizations.transporter_profiles (
			organization_id UUID        PRIMARY KEY,
			context            TEXT        NOT NULL,
			status             TEXT        NOT NULL CHECK (status IN ('AwaitingDocumentation', 'DocumentsInReview', 'Vetted', 'Rejected', 'CoverLapsed')),
			attempt_number     INTEGER     NOT NULL DEFAULT 0,
			fleet_availability_gate BOOLEAN NOT NULL DEFAULT FALSE,
			git_verified        BOOLEAN     NOT NULL DEFAULT FALSE,
			document_reviews    JSONB       NOT NULL DEFAULT '{}'::jsonb,
			certificates        JSONB       NOT NULL DEFAULT '{}'::jsonb,
			updated_at         TIMESTAMPTZ NOT NULL
		);
		ALTER TABLE organizations.transporter_profiles ADD COLUMN IF NOT EXISTS attempt_number INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE organizations.transporter_profiles ADD COLUMN IF NOT EXISTS fleet_availability_gate BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE organizations.transporter_profiles ADD COLUMN IF NOT EXISTS git_verified BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE organizations.transporter_profiles ADD COLUMN IF NOT EXISTS document_reviews JSONB NOT NULL DEFAULT '{}'::jsonb;
		ALTER TABLE organizations.transporter_profiles ADD COLUMN IF NOT EXISTS certificates JSONB NOT NULL DEFAULT '{}'::jsonb;

		-- BR-TP63 (38h-ii) adds CoverLapsed. The CHECK is dropped and recreated
		-- rather than left to the CREATE TABLE above, which only runs on a
		-- fresh database.
		--
		-- This is not cosmetic: an unmigrated database rejects the projector's
		-- write, the projector Naks, and JetStream redelivers the same event
		-- forever — observed 2026-08-21 as an ack floor frozen one message
		-- behind while the consumer sequence climbed into the tens of
		-- thousands. Because the drop suspends the organization *before* the
		-- projection catches up, the visible symptom is a suspended
		-- organization whose profile still reads Vetted with the gate open:
		-- the two halves of BR-TP28 disagreeing, with nothing in the logs.
		ALTER TABLE organizations.transporter_profiles DROP CONSTRAINT IF EXISTS transporter_profiles_status_check;
		ALTER TABLE organizations.transporter_profiles ADD CONSTRAINT transporter_profiles_status_check
			CHECK (status IN ('AwaitingDocumentation', 'DocumentsInReview', 'Vetted', 'Rejected', 'CoverLapsed'))`)
	return err
}

// Get always reads Postgres directly. In particular, BR-TP19's activation
// gate is wired to this type and never to the cache adapter.
func (p *Projection) Get(ctx context.Context, organizationID string) (profiledomain.State, error) {
	var state profiledomain.State
	var reviews, certificates []byte
	err := p.db.QueryRowContext(ctx, `
		SELECT context, organization_id, status, attempt_number, fleet_availability_gate, git_verified, document_reviews, certificates, updated_at
		FROM organizations.transporter_profiles
		WHERE organization_id = $1`, organizationID,
	).Scan(&state.Context, &state.ID, &state.Status, &state.AttemptNumber, &state.FleetAvailabilityGate, &state.GitVerified, &reviews, &certificates, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return profiledomain.State{}, profiledomain.ErrNotFound
	}
	if err != nil {
		return profiledomain.State{}, err
	}
	if err := json.Unmarshal(reviews, &state.DocumentReviews); err != nil {
		return profiledomain.State{}, err
	}
	if err := json.Unmarshal(certificates, &state.Certificates); err != nil {
		return profiledomain.State{}, err
	}
	return state, nil
}

func (p *Projection) Upsert(ctx context.Context, state profiledomain.State) error {
	reviews, err := json.Marshal(state.DocumentReviews)
	if err != nil {
		return err
	}
	certificates, err := json.Marshal(state.Certificates)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO organizations.transporter_profiles
			(organization_id, context, status, attempt_number, fleet_availability_gate, git_verified, document_reviews, certificates, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (organization_id) DO UPDATE SET
			context = EXCLUDED.context,
			status = EXCLUDED.status,
			attempt_number = EXCLUDED.attempt_number,
			fleet_availability_gate = EXCLUDED.fleet_availability_gate,
			git_verified = EXCLUDED.git_verified,
			document_reviews = EXCLUDED.document_reviews,
			certificates = EXCLUDED.certificates,
			updated_at = EXCLUDED.updated_at`,
		state.ID, state.Context, state.Status, state.AttemptNumber, state.FleetAvailabilityGate, state.GitVerified, reviews, certificates, state.UpdatedAt,
	)
	return err
}
