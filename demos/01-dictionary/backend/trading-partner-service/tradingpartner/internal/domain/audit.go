package domain

import (
	"context"
	"time"
)

// Audit actions recorded by the lifecycle handlers (BR-TP06) — one per
// TradingPartner lifecycle command, mirroring accounts-service's
// AuditActionCreated/Suspended/Reactivated naming (BR-AC11).
const (
	AuditActionRegistered  = "registered"
	AuditActionActivated   = "activated"
	AuditActionSuspended   = "suspended"
	AuditActionReactivated = "reactivated"
)

const (
	AuditOutcomeSuccess = "success"
	AuditOutcomeFailed  = "failed"
)

// AuditEntry is one row of trading_partner.audit_events — an immutable
// record of a lifecycle action taken against a TradingPartner. Mirrors
// accounts-service's AuditEntry shape verbatim (BR-AC11), substituting
// TradingPartnerID for Account.
type AuditEntry struct {
	ID               string
	TradingPartnerID string
	Action           string
	Actor            string
	SourceIP         string
	Outcome          string
	Metadata         map[string]any
	CreatedAt        time.Time
}

// AuditRecorder is the port BR-TP06's guarantees are enforced against — an
// append-only trail (no UPDATE, no DELETE at the adapter), best-effort
// writes (a failed Record must never block or roll back the lifecycle
// operation it describes). The Postgres adapter lives in internal/postgres.
type AuditRecorder interface {
	Record(ctx context.Context, entry AuditEntry) error
}

// AuditReader is the read side of the audit trail — separated from
// AuditRecorder since the REST layer's GET /audit endpoint has no lifecycle
// policy of its own to route through the commands layer, unlike every
// write path.
type AuditReader interface {
	ListByPartner(ctx context.Context, partnerID string) ([]AuditEntry, error)
}
