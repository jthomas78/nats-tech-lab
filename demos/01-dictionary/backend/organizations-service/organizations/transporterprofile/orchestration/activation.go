package orchestration

import (
	"context"
	"errors"

	organizationcommands "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

var ErrTransporterProfileNotVetted = errors.New("transporter profile must exist and be Vetted before activation")

type PartnerGateway interface {
	Get(ctx context.Context, id string) (organizationdomain.Organization, error)
	Activate(ctx context.Context, actor organizationcommands.Actor, id string) (organizationdomain.Organization, error)
}

// CanonicalProjectionReader is deliberately the Postgres-shaped read port.
// The KV cache does not implement or enter this activation path.
type CanonicalProjectionReader interface {
	Get(ctx context.Context, organizationID string) (profiledomain.State, error)
}

type ActivationHandler struct {
	partners PartnerGateway
	profiles CanonicalProjectionReader
}

func NewActivationHandler(partners PartnerGateway, profiles CanonicalProjectionReader) *ActivationHandler {
	return &ActivationHandler{partners: partners, profiles: profiles}
}

func (h *ActivationHandler) Activate(ctx context.Context, actor organizationcommands.Actor, id string) (organizationdomain.Organization, error) {
	partner, err := h.partners.Get(ctx, id)
	if err != nil {
		return organizationdomain.Organization{}, err
	}
	if partner.Type == organizationdomain.PartnerTypeTransporter {
		if h.profiles == nil {
			return organizationdomain.Organization{}, ErrTransporterProfileNotVetted
		}
		profile, profileErr := h.profiles.Get(ctx, id)
		if profileErr != nil || profile.Status != profiledomain.StatusVetted {
			return organizationdomain.Organization{}, ErrTransporterProfileNotVetted
		}
	}
	return h.partners.Activate(ctx, actor, id)
}
