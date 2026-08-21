// Package commands is organizations-service's application layer —
// orchestrates domain policy, repository persistence, and BR-TP06's audit
// trail. Unlike accounts-service (flat, no domain/application split), audit
// recording lives here rather than in the REST layer, since this service
// has an explicit application layer to hold it; the BR-TP06 behavior itself
// (best-effort, actor/source-IP, nothing on pre-mutation failure) is
// identical to accounts-service's BR-AC11.
package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

// Actor bundles who's calling and from where — threaded through every
// lifecycle command so BR-TP06's audit row can record it. Mirrors
// accounts-service's actor/sourceIP handler parameters.
type Actor struct {
	Name     string
	SourceIP string
}

// OrganizationHandler is the application-layer entry point for
// registration and lifecycle commands (BR-TP01-BR-TP06).
type OrganizationHandler struct {
	repo  domain.OrganizationRepository
	audit domain.AuditRecorder
}

func NewOrganizationHandler(repo domain.OrganizationRepository, audit domain.AuditRecorder) *OrganizationHandler {
	return &OrganizationHandler{repo: repo, audit: audit}
}

func (h *OrganizationHandler) Get(ctx context.Context, id string) (domain.Organization, error) {
	return h.repo.Get(ctx, id)
}

func (h *OrganizationHandler) List(ctx context.Context, contextKey string) ([]domain.Organization, error) {
	return h.repo.List(ctx, contextKey)
}

// Register implements BR-TP01/BR-TP02: validates via domain.Register, then
// persists. A validation failure never reaches the repository, so no audit
// row is written for it (BR-TP06 — "nothing written for a request that
// fails validation before any mutation").
// BR-TP35 widens this to carry the optional Company Information fields, so
// 38d's registration wizard can commit a partner and its details in one call
// rather than register-then-update (ADR-049 finding 6's half-commit shape).
func (h *OrganizationHandler) Register(ctx context.Context, actor Actor, partnerType domain.PartnerType, contextKey string, details domain.Details) (domain.Organization, error) {
	tp, err := domain.RegisterWithDetails(partnerType, contextKey, details)
	if err != nil {
		return domain.Organization{}, err
	}
	tp, err = h.repo.Register(ctx, tp)
	if err != nil {
		return domain.Organization{}, err
	}
	h.recordAudit(ctx, tp.ID, domain.AuditActionRegistered, actor, domain.AuditOutcomeSuccess, nil)
	return tp, nil
}

// UpdateDetails implements BR-TP32/BR-TP34 — Company Information edits under
// the caller's expected version. A version conflict returns
// domain.ErrVersionConflict and writes no audit row: nothing was mutated, so
// BR-TP06's "nothing written on pre-mutation failure" rule applies exactly as
// it does to a validation failure.
func (h *OrganizationHandler) UpdateDetails(ctx context.Context, actor Actor, id string, expectedVersion int, details domain.Details) (domain.Organization, error) {
	tp, err := h.repo.UpdateDetails(ctx, id, expectedVersion, details)
	if err != nil {
		return domain.Organization{}, err
	}
	h.recordAudit(ctx, id, domain.AuditActionDetailsUpdated, actor, domain.AuditOutcomeSuccess, map[string]any{
		"version": tp.Version,
	})
	return tp, nil
}

// Activate implements BR-TP03.
func (h *OrganizationHandler) Activate(ctx context.Context, actor Actor, id string) (domain.Organization, error) {
	tp, err := h.repo.Activate(ctx, id)
	if err != nil {
		return domain.Organization{}, err
	}
	h.recordAudit(ctx, id, domain.AuditActionActivated, actor, domain.AuditOutcomeSuccess, nil)
	return tp, nil
}

// Suspend implements BR-TP04 — reason lands in the audit row's metadata.
func (h *OrganizationHandler) Suspend(ctx context.Context, actor Actor, id string, reason string) (domain.Organization, error) {
	if reason == "" {
		// BR-TP04: rejected before any mutation is attempted — no audit row
		// (same "nothing written on pre-mutation failure" rule as accounts-service).
		return domain.Organization{}, domain.ErrSuspendReasonRequired
	}
	tp, err := h.repo.Suspend(ctx, id, reason)
	if err != nil {
		return domain.Organization{}, err
	}
	h.recordAudit(ctx, id, domain.AuditActionSuspended, actor, domain.AuditOutcomeSuccess, map[string]any{"reason": reason})
	return tp, nil
}

// Reactivate implements BR-TP05.
func (h *OrganizationHandler) Reactivate(ctx context.Context, actor Actor, id string) (domain.Organization, error) {
	tp, err := h.repo.Reactivate(ctx, id)
	if err != nil {
		return domain.Organization{}, err
	}
	h.recordAudit(ctx, id, domain.AuditActionReactivated, actor, domain.AuditOutcomeSuccess, nil)
	return tp, nil
}

// recordAudit is best-effort (BR-TP06): a failed write is swallowed here —
// callers already have their real result (the mutation itself succeeded);
// audit logging must never turn a successful lifecycle change into a
// reported failure. (Mirrors accounts-service's own recordAudit contract;
// unlike it, there's no logger threaded through yet, since this is a POC-scale
// service — a follow-on can add one if a failed audit write ever needs
// surfacing.)
func (h *OrganizationHandler) recordAudit(ctx context.Context, partnerID, action string, actor Actor, outcome string, metadata map[string]any) {
	_ = h.audit.Record(ctx, domain.AuditEntry{
		OrganizationID: partnerID,
		Action:         action,
		Actor:          actor.Name,
		SourceIP:       actor.SourceIP,
		Outcome:        outcome,
		Metadata:       metadata,
	})
}
