package postgres

import (
	"context"
	"database/sql"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

// FleetAssetRepository is the Postgres adapter for
// domain.FleetAssetRepository.
type FleetAssetRepository struct{ db *sql.DB }

func NewFleetAssetRepository(db *sql.DB) *FleetAssetRepository {
	return &FleetAssetRepository{db: db}
}

// AddFleetAsset persists BR-TP13's global registrationNo-uniqueness
// invariant as the table's own primary key — a duplicate is rejected with
// domain.ErrRegistrationNoAlreadyExists rather than a raw Postgres error.
func (r *FleetAssetRepository) AddFleetAsset(ctx context.Context, partnerID string, asset domain.FleetAsset) (domain.FleetAsset, error) {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO organizations.fleet_assets
			(registration_no, organization_id, vin, make, model, vehicle_type_code)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		asset.RegistrationNo, partnerID, asset.VIN, asset.Make, asset.Model, asset.VehicleTypeCode)
	if err != nil {
		return domain.FleetAsset{}, mapUniqueViolation(err, "fleet_assets_pkey", domain.ErrRegistrationNoAlreadyExists)
	}
	return asset, nil
}

func (r *FleetAssetRepository) ListFleetAssets(ctx context.Context, partnerID string) ([]domain.FleetAsset, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT registration_no, vin, make, model, vehicle_type_code
		FROM organizations.fleet_assets WHERE organization_id = $1 ORDER BY registration_no`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.FleetAsset{}
	for rows.Next() {
		var a domain.FleetAsset
		if err := rows.Scan(&a.RegistrationNo, &a.VIN, &a.Make, &a.Model, &a.VehicleTypeCode); err != nil {
			return nil, err
		}
		all = append(all, a)
	}
	return all, rows.Err()
}
