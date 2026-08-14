// Package commands is trading-partner-service's application layer —
// orchestrates domain policy, repository persistence, and BR-TP06's audit
// trail. Unlike accounts-service (flat, no domain/application split), audit
// recording lives here rather than in the REST layer, since this service
// has an explicit application layer to hold it; the BR-TP06 behavior itself
// (best-effort, actor/source-IP, nothing on pre-mutation failure) is
// identical to accounts-service's BR-AC11.
package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// Actor bundles who's calling and from where — threaded through every
// lifecycle command so BR-TP06's audit row can record it. Mirrors
// accounts-service's actor/sourceIP handler parameters.
type Actor struct {
	Name     string
	SourceIP string
}

// TradingPartnerHandler is the application-layer entry point for
// registration and lifecycle commands (BR-TP01-BR-TP06).
type TradingPartnerHandler struct {
	repo  domain.TradingPartnerRepository
	audit domain.AuditRecorder
}

func NewTradingPartnerHandler(repo domain.TradingPartnerRepository, audit domain.AuditRecorder) *TradingPartnerHandler {
	return &TradingPartnerHandler{repo: repo, audit: audit}
}

func (h *TradingPartnerHandler) Get(ctx context.Context, id string) (domain.TradingPartner, error) {
	return h.repo.Get(ctx, id)
}

func (h *TradingPartnerHandler) List(ctx context.Context, contextKey string) ([]domain.TradingPartner, error) {
	return h.repo.List(ctx, contextKey)
}

// Register implements BR-TP01/BR-TP02: validates via domain.Register, then
// persists. A validation failure never reaches the repository, so no audit
// row is written for it (BR-TP06 — "nothing written for a request that
// fails validation before any mutation").
func (h *TradingPartnerHandler) Register(ctx context.Context, actor Actor, name string, partnerType domain.PartnerType, contextKey string) (domain.TradingPartner, error) {
	tp, err := domain.Register(name, partnerType, contextKey)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	tp, err = h.repo.Register(ctx, tp)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	h.recordAudit(ctx, tp.ID, domain.AuditActionRegistered, actor, domain.AuditOutcomeSuccess, nil)
	return tp, nil
}

// Activate implements BR-TP03.
func (h *TradingPartnerHandler) Activate(ctx context.Context, actor Actor, id string) (domain.TradingPartner, error) {
	tp, err := h.repo.Activate(ctx, id)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	h.recordAudit(ctx, id, domain.AuditActionActivated, actor, domain.AuditOutcomeSuccess, nil)
	return tp, nil
}

// Suspend implements BR-TP04 — reason lands in the audit row's metadata.
func (h *TradingPartnerHandler) Suspend(ctx context.Context, actor Actor, id string, reason string) (domain.TradingPartner, error) {
	if reason == "" {
		// BR-TP04: rejected before any mutation is attempted — no audit row
		// (same "nothing written on pre-mutation failure" rule as accounts-service).
		return domain.TradingPartner{}, domain.ErrSuspendReasonRequired
	}
	tp, err := h.repo.Suspend(ctx, id, reason)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	h.recordAudit(ctx, id, domain.AuditActionSuspended, actor, domain.AuditOutcomeSuccess, map[string]any{"reason": reason})
	return tp, nil
}

// Reactivate implements BR-TP05.
func (h *TradingPartnerHandler) Reactivate(ctx context.Context, actor Actor, id string) (domain.TradingPartner, error) {
	tp, err := h.repo.Reactivate(ctx, id)
	if err != nil {
		return domain.TradingPartner{}, err
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
func (h *TradingPartnerHandler) recordAudit(ctx context.Context, partnerID, action string, actor Actor, outcome string, metadata map[string]any) {
	_ = h.audit.Record(ctx, domain.AuditEntry{
		TradingPartnerID: partnerID,
		Action:           action,
		Actor:            actor.Name,
		SourceIP:         actor.SourceIP,
		Outcome:          outcome,
		Metadata:         metadata,
	})
}
