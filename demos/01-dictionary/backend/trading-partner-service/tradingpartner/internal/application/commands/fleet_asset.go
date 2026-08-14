package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// FleetAssetHandler wires BR-TP12/BR-TP13's domain guards to persistence,
// plus BR-TP14's refdata-backed vehicleTypeCode existence check via the
// VehicleTypeValidator port (internal/refdataclient's rpc.* adapter in
// production, a fake in tests).
type FleetAssetHandler struct {
	partners  domain.TradingPartnerRepository
	fleet     domain.FleetAssetRepository
	validator domain.VehicleTypeValidator
}

func NewFleetAssetHandler(partners domain.TradingPartnerRepository, fleet domain.FleetAssetRepository, validator domain.VehicleTypeValidator) *FleetAssetHandler {
	return &FleetAssetHandler{partners: partners, fleet: fleet, validator: validator}
}

// AddFleetAsset implements BR-TP12/BR-TP13 (partner-type + required-field
// guards) then BR-TP14 (refdata corpus existence) before persisting. tenant
// names which NATS account's connection BR-TP14's rpc.* call should ride —
// see domain.VehicleTypeValidator's doc comment for why the caller (not
// this service) has to supply it; the Admin UI's existing tenant.js
// selection is the source.
func (h *FleetAssetHandler) AddFleetAsset(ctx context.Context, partnerID, tenant, registrationNo, vin, vehicleMake, model, vehicleTypeCode string) (domain.FleetAsset, error) {
	tp, err := h.partners.Get(ctx, partnerID)
	if err != nil {
		return domain.FleetAsset{}, err
	}
	asset, err := domain.AddFleetAsset(tp.Type, registrationNo, vin, vehicleMake, model, vehicleTypeCode)
	if err != nil {
		return domain.FleetAsset{}, err
	}

	// BR-TP14: not a pure domain rule (see domain.VehicleTypeValidator's doc
	// comment) — checked here, at the application layer, after the pure
	// BR-TP12/BR-TP13 guards have already passed.
	ok, err := h.validator.Exists(ctx, tenant, tp.Context, vehicleTypeCode)
	if err != nil {
		return domain.FleetAsset{}, err
	}
	if !ok {
		return domain.FleetAsset{}, domain.ErrUnknownVehicleTypeCode
	}

	return h.fleet.AddFleetAsset(ctx, partnerID, asset)
}

func (h *FleetAssetHandler) ListFleetAssets(ctx context.Context, partnerID string) ([]domain.FleetAsset, error) {
	return h.fleet.ListFleetAssets(ctx, partnerID)
}
