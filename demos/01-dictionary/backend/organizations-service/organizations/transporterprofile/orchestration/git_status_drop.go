package orchestration

import (
	"context"
	"errors"

	organizationcommands "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

const GitStatusDropReason = "GIT insurance expired or was revoked"

type GitDropPartnerGateway interface {
	Get(context.Context, string) (organizationdomain.Organization, error)
	Suspend(context.Context, organizationcommands.Actor, string, string) (organizationdomain.Organization, error)
}

type GitStatusDropHandler struct {
	contextKey string
	store      EventStore
	partners   GitDropPartnerGateway
}

func NewGitStatusDropHandler(contextKey string, store EventStore, partners GitDropPartnerGateway) *GitStatusDropHandler {
	return &GitStatusDropHandler{contextKey: contextKey, store: store, partners: partners}
}

func (h *GitStatusDropHandler) HandleGitStatusDrop(ctx context.Context, organizationID string) error {
	agg, sequence, err := h.store.Hydrate(ctx, h.contextKey, organizationID)
	if err != nil {
		return err
	}
	state := agg.State()
	if !state.FleetAvailabilityGate {
		// A prior call may have appended revocation and then failed to suspend.
		// Finish that second half on retry, but never suspend a profile whose
		// gate was never opened by successful vetting.
		//
		// The state to look for is CoverLapsed, not Vetted (BR-TP63): the
		// revocation this is resuming is what moved the status, so a profile
		// still reading Vetted with the gate shut never got that far. Keyed
		// off Vetted this returns success without suspending, leaving a
		// lapsed transporter ACTIVE and assignable.
		if state.Status != profiledomain.StatusCoverLapsed || state.GitVerified {
			return nil
		}
		partner, getErr := h.partners.Get(ctx, organizationID)
		if getErr != nil {
			return getErr
		}
		if partner.Status == organizationdomain.StatusSuspended {
			return nil
		}
		_, err = h.partners.Suspend(ctx, organizationcommands.Actor{Name: "temporal-git-monitor"}, organizationID, GitStatusDropReason)
		return err
	}
	if state.Status != profiledomain.StatusVetted {
		return nil
	}
	event, err := agg.RevokeFleetAvailability()
	if err != nil {
		return err
	}
	if _, err = h.store.Append(ctx, h.contextKey, organizationID, event, sequence); err != nil {
		if errors.Is(err, ErrSequenceConflict) {
			return ErrSequenceConflict
		}
		return err
	}
	_, err = h.partners.Suspend(ctx, organizationcommands.Actor{Name: "temporal-git-monitor"}, organizationID, GitStatusDropReason)
	return err
}
