// Package postgres is the canonical TransporterProfile Shape-B projection.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
)

type Projection struct{ db *sql.DB }

func NewProjection(db *sql.DB) *Projection { return &Projection{db: db} }

func (p *Projection) Migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS trading_partner`); err != nil {
		return err
	}
	_, err := p.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS trading_partner.transporter_profiles (
			trading_partner_id UUID        PRIMARY KEY,
			context            TEXT        NOT NULL,
			status             TEXT        NOT NULL CHECK (status IN ('AwaitingDocumentation', 'DocumentsInReview', 'Vetted', 'Rejected')),
			attempt_number     INTEGER     NOT NULL DEFAULT 0,
			fleet_availability_gate BOOLEAN NOT NULL DEFAULT FALSE,
			git_verified        BOOLEAN     NOT NULL DEFAULT FALSE,
			document_reviews    JSONB       NOT NULL DEFAULT '{}'::jsonb,
			updated_at         TIMESTAMPTZ NOT NULL
		);
		ALTER TABLE trading_partner.transporter_profiles ADD COLUMN IF NOT EXISTS attempt_number INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE trading_partner.transporter_profiles ADD COLUMN IF NOT EXISTS fleet_availability_gate BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE trading_partner.transporter_profiles ADD COLUMN IF NOT EXISTS git_verified BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE trading_partner.transporter_profiles ADD COLUMN IF NOT EXISTS document_reviews JSONB NOT NULL DEFAULT '{}'::jsonb`)
	return err
}

// Get always reads Postgres directly. In particular, BR-TP19's activation
// gate is wired to this type and never to the cache adapter.
func (p *Projection) Get(ctx context.Context, tradingPartnerID string) (profiledomain.State, error) {
	var state profiledomain.State
	var reviews []byte
	err := p.db.QueryRowContext(ctx, `
		SELECT context, trading_partner_id, status, attempt_number, fleet_availability_gate, git_verified, document_reviews, updated_at
		FROM trading_partner.transporter_profiles
		WHERE trading_partner_id = $1`, tradingPartnerID,
	).Scan(&state.Context, &state.ID, &state.Status, &state.AttemptNumber, &state.FleetAvailabilityGate, &state.GitVerified, &reviews, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return profiledomain.State{}, profiledomain.ErrNotFound
	}
	if err != nil {
		return profiledomain.State{}, err
	}
	if err := json.Unmarshal(reviews, &state.DocumentReviews); err != nil {
		return profiledomain.State{}, err
	}
	return state, nil
}

func (p *Projection) Upsert(ctx context.Context, state profiledomain.State) error {
	reviews, err := json.Marshal(state.DocumentReviews)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO trading_partner.transporter_profiles
			(trading_partner_id, context, status, attempt_number, fleet_availability_gate, git_verified, document_reviews, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (trading_partner_id) DO UPDATE SET
			context = EXCLUDED.context,
			status = EXCLUDED.status,
			attempt_number = EXCLUDED.attempt_number,
			fleet_availability_gate = EXCLUDED.fleet_availability_gate,
			git_verified = EXCLUDED.git_verified,
			document_reviews = EXCLUDED.document_reviews,
			updated_at = EXCLUDED.updated_at`,
		state.ID, state.Context, state.Status, state.AttemptNumber, state.FleetAvailabilityGate, state.GitVerified, reviews, state.UpdatedAt,
	)
	return err
}
