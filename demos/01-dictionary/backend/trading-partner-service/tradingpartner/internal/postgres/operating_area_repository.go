package postgres

import (
	"context"
	"database/sql"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// OperatingAreaRepository is BR-TP46-BR-TP50's Postgres adapter.
type OperatingAreaRepository struct {
	db *sql.DB
}

func NewOperatingAreaRepository(db *sql.DB) *OperatingAreaRepository {
	return &OperatingAreaRepository{db: db}
}

// AddOperatingArea persists one assignment. BR-TP49's uniqueness is the
// database's job, not the caller's: a read-then-write check in the command
// layer would be racy, so a unique-violation here is translated rather than
// pre-empted — the same treatment fleet_assets.registration_no gets.
func (r *OperatingAreaRepository) AddOperatingArea(ctx context.Context, partnerID string, area domain.OperatingArea) (domain.OperatingArea, error) {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO trading_partner.transporter_operating_areas (trading_partner_id, level, code, country_code)
		VALUES ($1, $2, $3, $4)`,
		partnerID, string(area.Level), area.Code, area.CountryCode)
	if err != nil {
		return domain.OperatingArea{}, mapUniqueViolation(err,
			"transporter_operating_areas_unique_idx", domain.ErrOperatingAreaAlreadyAssigned)
	}
	return area, nil
}

func (r *OperatingAreaRepository) ListOperatingAreas(ctx context.Context, partnerID string) ([]domain.OperatingArea, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT level, code, country_code
		FROM trading_partner.transporter_operating_areas
		WHERE trading_partner_id = $1
		ORDER BY country_code, level, code`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	areas := []domain.OperatingArea{}
	for rows.Next() {
		var a domain.OperatingArea
		var level string
		if err := rows.Scan(&level, &a.Code, &a.CountryCode); err != nil {
			return nil, err
		}
		a.Level = domain.AreaLevel(level)
		areas = append(areas, a)
	}
	return areas, rows.Err()
}

// RemoveOperatingArea deletes one assignment. Removing an area a partner
// does not hold is reported rather than silently succeeding — BR-TP50
// records every change in the audit trail, and an entry describing a
// deletion that never happened would be worse than no entry.
func (r *OperatingAreaRepository) RemoveOperatingArea(ctx context.Context, partnerID string, level domain.AreaLevel, code string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM trading_partner.transporter_operating_areas
		WHERE trading_partner_id = $1 AND level = $2 AND code = $3`,
		partnerID, string(level), code)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrOperatingAreaNotAssigned
	}
	return nil
}
