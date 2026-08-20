package postgres

import (
	"context"
	"database/sql"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// TrackingCredentialRepository is BR-TP51/BR-TP54's Postgres adapter. It
// stores the non-secret record only — see the table's own comment in
// migrate.go for why no payload column exists.
type TrackingCredentialRepository struct {
	db *sql.DB
}

func NewTrackingCredentialRepository(db *sql.DB) *TrackingCredentialRepository {
	return &TrackingCredentialRepository{db: db}
}

// UpsertTrackingCredential records a configured provider, replacing any
// existing row for the same (partner, provider).
//
// An upsert rather than an insert-or-error, because BR-TP54 makes
// re-configuring a provider an overwrite: a credential is current state, not
// evidence, and rotation is routine hygiene rather than an exceptional case
// the caller should have to handle.
func (r *TrackingCredentialRepository) UpsertTrackingCredential(ctx context.Context, partnerID string, cred domain.TrackingCredential) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO trading_partner.tracking_credentials (trading_partner_id, provider, credential_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (trading_partner_id, provider)
		DO UPDATE SET credential_type = EXCLUDED.credential_type, configured_at = now()`,
		partnerID, string(cred.Provider), string(cred.CredentialType))
	return err
}

// ListTrackingCredentials returns the non-secret records for a partner.
// There is deliberately no Get-with-payload here: the payload never travels
// through this repository at all (BR-TP52).
func (r *TrackingCredentialRepository) ListTrackingCredentials(ctx context.Context, partnerID string) ([]domain.TrackingCredential, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT provider, credential_type
		FROM trading_partner.tracking_credentials
		WHERE trading_partner_id = $1
		ORDER BY provider`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	creds := []domain.TrackingCredential{}
	for rows.Next() {
		var c domain.TrackingCredential
		var provider, credentialType string
		if err := rows.Scan(&provider, &credentialType); err != nil {
			return nil, err
		}
		c.Provider = domain.Provider(provider)
		c.CredentialType = domain.CredentialType(credentialType)
		// A row exists only because BR-TP53 stored the payload first, so a
		// persisted credential is by construction a configured one.
		c.CredentialsConfigured = true
		creds = append(creds, c)
	}
	return creds, rows.Err()
}
