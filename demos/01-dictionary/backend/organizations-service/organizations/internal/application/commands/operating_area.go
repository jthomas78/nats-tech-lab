package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

// OperatingAreaHandler wires BR-TP46-BR-TP48's domain guards to persistence,
// plus BR-TP47's refdata-backed corpus check via the OperatingAreaResolver
// port and BR-TP50's audit trail.
type OperatingAreaHandler struct {
	partners domain.OrganizationRepository
	areas    domain.OperatingAreaRepository
	resolver domain.OperatingAreaResolver
	audit    domain.AuditRecorder
}

func NewOperatingAreaHandler(
	partners domain.OrganizationRepository,
	areas domain.OperatingAreaRepository,
	resolver domain.OperatingAreaResolver,
	audit domain.AuditRecorder,
) *OperatingAreaHandler {
	return &OperatingAreaHandler{partners: partners, areas: areas, resolver: resolver, audit: audit}
}

// AddOperatingArea validates and persists one assignment.
//
// Order matters and is not arbitrary: the partner-type guard (BR-TP46) and
// the shape guards (BR-TP47) are pure and free, so they run before the
// network call to refdata. Only then is the code resolved, and only then is
// the partner's existing set read for BR-TP48's overlap check — an add that
// was always going to be refused should not cost an rpc.* round trip or a
// second query.
//
// BR-TP50: no lifecycle check. An area may be added while the profile is
// Vetted, and doing so neither re-runs the vetting workflow nor touches
// FleetAvailabilityGate, because no vetting branch reads operating areas.
func (h *OperatingAreaHandler) AddOperatingArea(ctx context.Context, actor Actor, partnerID, tenant string, level domain.AreaLevel, code string) (domain.OperatingArea, error) {
	tp, err := h.partners.Get(ctx, partnerID)
	if err != nil {
		return domain.OperatingArea{}, err
	}

	// Cheap guards first: partner type and level/code shape, neither of
	// which needs refdata or the database. AddOperatingArea below re-runs
	// them as part of the full check, so this is an early exit, not a
	// separate rule.
	if err := domain.ValidateOperatingAreaShape(tp.Type, level, code); err != nil {
		return domain.OperatingArea{}, err
	}

	// BR-TP47: not a pure domain rule — corpus membership requires a live
	// rpc.* call (see domain.OperatingAreaResolver). This also yields the
	// parent country BR-TP48 needs, straight from BR-D47's relation.
	countryCode, found, err := h.resolver.ResolveArea(ctx, tenant, level, code)
	if err != nil {
		return domain.OperatingArea{}, err
	}
	if !found {
		return domain.OperatingArea{}, domain.ErrUnknownOperatingAreaCode
	}

	existing, err := h.areas.ListOperatingAreas(ctx, partnerID)
	if err != nil {
		return domain.OperatingArea{}, err
	}

	area, err := domain.AddOperatingArea(tp.Type, existing, level, code, countryCode)
	if err != nil {
		return domain.OperatingArea{}, err
	}

	saved, err := h.areas.AddOperatingArea(ctx, partnerID, area)
	if err != nil {
		return domain.OperatingArea{}, err
	}

	h.record(ctx, partnerID, domain.AuditActionOperatingAreaAdded, actor, map[string]any{
		"level": string(area.Level), "code": area.Code, "countryCode": area.CountryCode,
	})
	return saved, nil
}

// RemoveOperatingArea drops one assignment and audits it (BR-TP50).
func (h *OperatingAreaHandler) RemoveOperatingArea(ctx context.Context, actor Actor, partnerID string, level domain.AreaLevel, code string) error {
	if err := h.areas.RemoveOperatingArea(ctx, partnerID, level, code); err != nil {
		return err
	}
	h.record(ctx, partnerID, domain.AuditActionOperatingAreaRemoved, actor, map[string]any{
		"level": string(level), "code": code,
	})
	return nil
}

func (h *OperatingAreaHandler) ListOperatingAreas(ctx context.Context, partnerID string) ([]domain.OperatingArea, error) {
	return h.areas.ListOperatingAreas(ctx, partnerID)
}

// record writes BR-TP50's audit entry. Best-effort, per AuditRecorder's
// contract (BR-TP06): a failed trail write must never undo the change it
// describes, which has already committed.
func (h *OperatingAreaHandler) record(ctx context.Context, partnerID, action string, actor Actor, metadata map[string]any) {
	if h.audit == nil {
		return
	}
	_ = h.audit.Record(ctx, domain.AuditEntry{
		OrganizationID: partnerID,
		Action:         action,
		Actor:          actor.Name,
		SourceIP:       actor.SourceIP,
		Outcome:        domain.AuditOutcomeSuccess,
		Metadata:       metadata,
	})
}
