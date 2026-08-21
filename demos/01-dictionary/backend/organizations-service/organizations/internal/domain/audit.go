package domain

import (
	"context"
	"time"
)

// Audit actions recorded by the lifecycle handlers (BR-TP06) — one per
// Organization lifecycle command, mirroring accounts-service's
// AuditActionCreated/Suspended/Reactivated naming (BR-AC11).
const (
	AuditActionRegistered  = "registered"
	AuditActionActivated   = "activated"
	AuditActionSuspended   = "suspended"
	AuditActionReactivated = "reactivated"

	// AuditActionDetailsUpdated — BR-TP32 (38c-i). Company Information edits
	// are audited like the lifecycle transitions: who changed a partner's
	// registered company details, and when, is exactly the kind of question
	// BR-TP06's trail exists to answer.
	AuditActionDetailsUpdated = "details-updated"

	// AuditActionOperatingAreaAdded/Removed — BR-TP50 (38d-ii). Operating
	// areas are freely editable at any lifecycle state, including Vetted,
	// precisely because no vetting branch reads them — so the audit trail is
	// the only record that coverage changed, and who changed it.
	AuditActionOperatingAreaAdded   = "operating-area-added"
	AuditActionOperatingAreaRemoved = "operating-area-removed"
)

const (
	AuditOutcomeSuccess = "success"
	AuditOutcomeFailed  = "failed"
)

// AuditEntry is one row of organizations.audit_events — an immutable
// record of a lifecycle action taken against an Organization. Mirrors
// accounts-service's AuditEntry shape verbatim (BR-AC11), substituting
// OrganizationID for Account.
type AuditEntry struct {
	ID             string
	OrganizationID string
	Action         string
	Actor          string
	SourceIP       string
	Outcome        string
	Metadata       map[string]any
	CreatedAt      time.Time
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
