package orchestration

import (
	"context"
	"errors"

	tradingcommands "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	tradingdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
)

var ErrTransporterProfileNotVetted = errors.New("transporter profile must exist and be Vetted before activation")

type PartnerGateway interface {
	Get(ctx context.Context, id string) (tradingdomain.TradingPartner, error)
	Activate(ctx context.Context, actor tradingcommands.Actor, id string) (tradingdomain.TradingPartner, error)
}

// CanonicalProjectionReader is deliberately the Postgres-shaped read port.
// The KV cache does not implement or enter this activation path.
type CanonicalProjectionReader interface {
	Get(ctx context.Context, tradingPartnerID string) (profiledomain.State, error)
}

type ActivationHandler struct {
	partners PartnerGateway
	profiles CanonicalProjectionReader
}

func NewActivationHandler(partners PartnerGateway, profiles CanonicalProjectionReader) *ActivationHandler {
	return &ActivationHandler{partners: partners, profiles: profiles}
}

func (h *ActivationHandler) Activate(ctx context.Context, actor tradingcommands.Actor, id string) (tradingdomain.TradingPartner, error) {
	partner, err := h.partners.Get(ctx, id)
	if err != nil {
		return tradingdomain.TradingPartner{}, err
	}
	if partner.Type == tradingdomain.PartnerTypeTransporter {
		if h.profiles == nil {
			return tradingdomain.TradingPartner{}, ErrTransporterProfileNotVetted
		}
		profile, profileErr := h.profiles.Get(ctx, id)
		if profileErr != nil || profile.Status != profiledomain.StatusVetted {
			return tradingdomain.TradingPartner{}, ErrTransporterProfileNotVetted
		}
	}
	return h.partners.Activate(ctx, actor, id)
}
