package orchestration

import (
	"context"
	"errors"

	tradingcommands "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	tradingdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
)

const GitStatusDropReason = "GIT insurance expired or was revoked"

type GitDropPartnerGateway interface {
	Get(context.Context, string) (tradingdomain.TradingPartner, error)
	Suspend(context.Context, tradingcommands.Actor, string, string) (tradingdomain.TradingPartner, error)
}

type GitStatusDropHandler struct {
	contextKey string
	store      EventStore
	partners   GitDropPartnerGateway
}

func NewGitStatusDropHandler(contextKey string, store EventStore, partners GitDropPartnerGateway) *GitStatusDropHandler {
	return &GitStatusDropHandler{contextKey: contextKey, store: store, partners: partners}
}

func (h *GitStatusDropHandler) HandleGitStatusDrop(ctx context.Context, tradingPartnerID string) error {
	agg, sequence, err := h.store.Hydrate(ctx, h.contextKey, tradingPartnerID)
	if err != nil {
		return err
	}
	state := agg.State()
	if !state.FleetAvailabilityGate {
		// A prior call may have appended revocation and then failed to suspend.
		// Finish that second half on retry, but never suspend a profile whose
		// gate was never opened by successful vetting.
		if state.Status != profiledomain.StatusVetted || state.GitVerified {
			return nil
		}
		partner, getErr := h.partners.Get(ctx, tradingPartnerID)
		if getErr != nil {
			return getErr
		}
		if partner.Status == tradingdomain.StatusSuspended {
			return nil
		}
		_, err = h.partners.Suspend(ctx, tradingcommands.Actor{Name: "temporal-git-monitor"}, tradingPartnerID, GitStatusDropReason)
		return err
	}
	if state.Status != profiledomain.StatusVetted {
		return nil
	}
	event, err := agg.RevokeFleetAvailability()
	if err != nil {
		return err
	}
	if _, err = h.store.Append(ctx, h.contextKey, tradingPartnerID, event, sequence); err != nil {
		if errors.Is(err, ErrSequenceConflict) {
			return ErrSequenceConflict
		}
		return err
	}
	_, err = h.partners.Suspend(ctx, tradingcommands.Actor{Name: "temporal-git-monitor"}, tradingPartnerID, GitStatusDropReason)
	return err
}
