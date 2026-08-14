package domain

import "errors"

var (
	// ErrFleetAssetRequiresTransporter — BR-TP12: only a TRANSPORTER may
	// carry fleet assets.
	ErrFleetAssetRequiresTransporter = errors.New("fleet assets may only be added to a Transporter trading partner")

	// ErrRegistrationNoRequired — BR-TP13: a fleet asset must carry its own
	// vehicle registration number.
	ErrRegistrationNoRequired = errors.New("fleet asset registration number is required")

	// ErrVehicleTypeCodeRequired — BR-TP13: vehicleTypeCode is required at
	// this domain layer; whether the code actually exists in refdata's
	// vehicle-type corpus is BR-TP14, enforced by the 26d adapter, not here.
	ErrVehicleTypeCodeRequired = errors.New("fleet asset vehicle type code is required")

	// ErrRegistrationNoAlreadyExists — BR-TP13: registrationNo is globally
	// unique across every TradingPartner's fleet.
	ErrRegistrationNoAlreadyExists = errors.New("a fleet asset with this registration number already exists")

	// ErrUnknownVehicleTypeCode — BR-TP14: vehicleTypeCode does not exist in
	// refdata-service's vehicle-type corpus.
	ErrUnknownVehicleTypeCode = errors.New("vehicle type code is not recognized by refdata")
)

// FleetAsset is one truck/trailer owned by a Transporter (BR-TP12), a
// trimmed FleetAssetEntity — no subcontractingOwner in v1. VIN/Make/Model
// stay free text: they identify the specific vehicle, not a vocabulary.
// VehicleTypeCode is validated for presence here, but its existence in
// refdata-service's vehicle-type corpus is BR-TP14, checked by 26d's
// tenant-scoped rpc.* adapter, not this package.
type FleetAsset struct {
	RegistrationNo  string `json:"registrationNo"`
	VIN             string `json:"vin,omitempty"`
	Make            string `json:"make,omitempty"`
	Model           string `json:"model,omitempty"`
	VehicleTypeCode string `json:"vehicleTypeCode"`
}

// AddFleetAsset implements BR-TP12/BR-TP13 — only a TRANSPORTER may own a
// fleet asset, and registrationNo/vehicleTypeCode are required; vin/make/
// model are optional free text.
func AddFleetAsset(partnerType PartnerType, registrationNo, vin, vehicleMake, model, vehicleTypeCode string) (FleetAsset, error) {
	if partnerType != PartnerTypeTransporter {
		return FleetAsset{}, ErrFleetAssetRequiresTransporter
	}
	if registrationNo == "" {
		return FleetAsset{}, ErrRegistrationNoRequired
	}
	if vehicleTypeCode == "" {
		return FleetAsset{}, ErrVehicleTypeCodeRequired
	}
	return FleetAsset{
		RegistrationNo:  registrationNo,
		VIN:             vin,
		Make:            vehicleMake,
		Model:           model,
		VehicleTypeCode: vehicleTypeCode,
	}, nil
}
