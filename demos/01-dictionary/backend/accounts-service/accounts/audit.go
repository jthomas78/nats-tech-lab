package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Audit actions recorded by the three lifecycle handlers (BR-AC11) — one per
// handler in accounts/handler.go.
const (
	AuditActionCreated     = "created"
	AuditActionSuspended   = "suspended"
	AuditActionReactivated = "reactivated"
)

const (
	AuditOutcomeSuccess = "success"
	AuditOutcomeFailed  = "failed"
)

// AuditEntry is one row of accounts.audit_events — an immutable record of a
// lifecycle action taken against an account. Metadata is a free-form JSONB
// payload (e.g. {"error": "..."} on a failed outcome) rather than a fixed
// column set, since what's worth capturing differs per action and outcome.
type AuditEntry struct {
	ID        string
	Account   string
	Action    string
	Actor     string
	SourceIP  string
	Outcome   string
	Metadata  map[string]any
	CreatedAt time.Time
}

// AuditLog is the Postgres-backed, append-only audit trail for
// accounts-service's lifecycle actions (BR-AC11) — no UPDATE, no DELETE,
// only Record and read paths. Same schema/table/instance as Store, in the
// same accounts.accounts row's neighborhood (accounts.audit_events), not a
// separate service — the audit trail describes this service's own actions.
type AuditLog struct {
	db *sql.DB
}

func NewAuditLog(db *sql.DB) *AuditLog {
	return &AuditLog{db: db}
}

// Record inserts one immutable audit row. Callers treat this as best-effort
// (see BR-AC11): a failed audit write must be logged by the caller but must
// never roll back or block the lifecycle operation it's describing.
func (a *AuditLog) Record(ctx context.Context, entry AuditEntry) error {
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
		outcome = AuditOutcomeSuccess
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO accounts.audit_events (account, action, actor, source_ip, outcome, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.Account, entry.Action, entry.Actor, entry.SourceIP, outcome, metadataJSON)
	return err
}

// ListByAccount returns every audit row for name, newest first.
func (a *AuditLog) ListByAccount(ctx context.Context, name string) ([]AuditEntry, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, account, action, actor, source_ip, outcome, metadata, created_at
		FROM accounts.audit_events WHERE account = $1 ORDER BY created_at DESC`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var metadataJSON []byte
		if err := rows.Scan(&e.ID, &e.Account, &e.Action, &e.Actor, &e.SourceIP, &e.Outcome, &metadataJSON, &e.CreatedAt); err != nil {
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
