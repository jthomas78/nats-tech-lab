package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

// AuditLog is the Postgres-backed, append-only audit trail for
// organizations-service's lifecycle actions (BR-TP06) — no UPDATE, no
// DELETE, only Record and read paths. Implements domain.AuditRecorder.
// Reuses BR-AC11's conventions verbatim, not reinvented.
type AuditLog struct{ db *sql.DB }

func NewAuditLog(db *sql.DB) *AuditLog { return &AuditLog{db: db} }

// Record inserts one immutable audit row. Best-effort (BR-TP06): a failed
// audit write must be logged by the caller but must never roll back or
// block the lifecycle operation it's describing.
func (a *AuditLog) Record(ctx context.Context, entry domain.AuditEntry) error {
	metadata := entry.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	outcome := entry.Outcome
	if outcome == "" {
		outcome = domain.AuditOutcomeSuccess
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO organizations.audit_events (organization_id, action, actor, source_ip, outcome, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.OrganizationID, entry.Action, entry.Actor, entry.SourceIP, outcome, metadataJSON)
	return err
}

// ListByPartner returns every audit row for partnerID, newest first.
func (a *AuditLog) ListByPartner(ctx context.Context, partnerID string) ([]domain.AuditEntry, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, organization_id, action, actor, source_ip, outcome, metadata, created_at
		FROM organizations.audit_events WHERE organization_id = $1 ORDER BY created_at DESC`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		var metadataJSON []byte
		if err := rows.Scan(&e.ID, &e.OrganizationID, &e.Action, &e.Actor, &e.SourceIP, &e.Outcome, &metadataJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &e.Metadata); err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
