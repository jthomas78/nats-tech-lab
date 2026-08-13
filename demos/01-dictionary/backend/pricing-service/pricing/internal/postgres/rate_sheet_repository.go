package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// RateSheetRepository is the Postgres adapter for domain.RateSheetRepository.
type RateSheetRepository struct{ db *sql.DB }

func NewRateSheetRepository(db *sql.DB) *RateSheetRepository { return &RateSheetRepository{db: db} }

func (r *RateSheetRepository) Register(ctx context.Context, rs domain.RateSheet) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pricing.rate_sheets (context, name, customer_key, type, active)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (context, name) DO UPDATE SET customer_key = $3, type = $4, active = $5`,
		rs.Context, rs.Name, rs.CustomerKey, rs.Type, rs.Active)
	return err
}

func (r *RateSheetRepository) Get(ctx context.Context, contextKey, name string) (domain.RateSheet, error) {
	var rs domain.RateSheet
	err := r.db.QueryRowContext(ctx, `
		SELECT context, name, customer_key, type, active FROM pricing.rate_sheets WHERE context = $1 AND name = $2`,
		contextKey, name).Scan(&rs.Context, &rs.Name, &rs.CustomerKey, &rs.Type, &rs.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RateSheet{}, domain.ErrRateSheetNotFound
	}
	return rs, err
}

func (r *RateSheetRepository) List(ctx context.Context, contextKey string) ([]domain.RateSheet, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, name, customer_key, type, active FROM pricing.rate_sheets WHERE context = $1 ORDER BY name`,
		contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.RateSheet{}
	for rows.Next() {
		var rs domain.RateSheet
		if err := rows.Scan(&rs.Context, &rs.Name, &rs.CustomerKey, &rs.Type, &rs.Active); err != nil {
			return nil, err
		}
		all = append(all, rs)
	}
	return all, rows.Err()
}

func (r *RateSheetRepository) CreateDraft(ctx context.Context, contextKey, name string) (domain.RateSheetVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var version int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM pricing.rate_sheet_versions WHERE context = $1 AND rate_sheet_name = $2`,
		contextKey, name).Scan(&version); err != nil {
		return domain.RateSheetVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.rate_sheet_versions (context, rate_sheet_name, version, status)
		VALUES ($1, $2, $3, 'draft')`,
		contextKey, name, version); err != nil {
		return domain.RateSheetVersion{}, mapUniqueViolation(err, "one_rate_sheet_draft", domain.ErrRateSheetDraftAlreadyExists)
	}

	if err := tx.Commit(); err != nil {
		return domain.RateSheetVersion{}, err
	}
	return domain.RateSheetVersion{Context: contextKey, Version: version, Status: domain.VersionDraft}, nil
}

func (r *RateSheetRepository) AddEntry(ctx context.Context, contextKey, name string, version int, e domain.RateSheetEntry) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pricing.rate_sheet_entries
			(context, rate_sheet_name, version, route_key, vehicle_type,
			 cent_base_rate, drop_point_count, cent_additional_drop_rate,
			 diesel_pct, initial_diesel_cents)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (context, rate_sheet_name, version, route_key, vehicle_type) DO UPDATE
		SET cent_base_rate = $6, drop_point_count = $7, cent_additional_drop_rate = $8,
		    diesel_pct = $9, initial_diesel_cents = $10`,
		contextKey, name, version,
		e.RouteKey, e.VehicleType, e.CentBaseRate, e.DropPointCount, e.CentAdditionalDropRate,
		e.DieselPct, e.InitialDieselCents)
	return err
}

func (r *RateSheetRepository) SetFeeScaleOverride(ctx context.Context, contextKey, name string, version int, feeScaleName string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE pricing.rate_sheet_versions SET fee_scale_override = $4
		WHERE context = $1 AND rate_sheet_name = $2 AND version = $3`,
		contextKey, name, version, feeScaleName)
	return err
}

func (r *RateSheetRepository) Publish(ctx context.Context, contextKey, name string) (domain.RateSheetVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var version int
	err = tx.QueryRowContext(ctx, `
		UPDATE pricing.rate_sheet_versions SET status = 'published', published_at = now()
		WHERE context = $1 AND rate_sheet_name = $2 AND status = 'draft'
		RETURNING version`, contextKey, name).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RateSheetVersion{}, domain.ErrRateSheetDraftNotFound
	}
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.RateSheetVersion{}, err
	}
	return domain.RateSheetVersion{Context: contextKey, Version: version, Status: domain.VersionPublished}, nil
}

func (r *RateSheetRepository) Rollback(ctx context.Context, contextKey, name string, targetVersion int) (domain.RateSheetVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var targetStatus domain.VersionStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT status FROM pricing.rate_sheet_versions WHERE context = $1 AND rate_sheet_name = $2 AND version = $3`,
		contextKey, name, targetVersion).Scan(&targetStatus); errors.Is(err, sql.ErrNoRows) {
		return domain.RateSheetVersion{}, domain.ErrRateSheetRollbackTargetNotPublished
	} else if err != nil {
		return domain.RateSheetVersion{}, err
	}
	if err := domain.CanRollbackRateSheetTo(domain.RateSheetVersion{Status: targetStatus}); err != nil {
		return domain.RateSheetVersion{}, err
	}

	var current int
	if err := tx.QueryRowContext(ctx, `
		SELECT version FROM pricing.rate_sheet_versions WHERE context = $1 AND rate_sheet_name = $2 AND status = 'published' ORDER BY version DESC LIMIT 1`,
		contextKey, name).Scan(&current); err != nil {
		return domain.RateSheetVersion{}, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM pricing.rate_sheet_versions WHERE context = $1 AND rate_sheet_name = $2`,
		contextKey, name).Scan(&next); err != nil {
		return domain.RateSheetVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.rate_sheet_versions (context, rate_sheet_name, version, status, parent_version, fee_scale_override, published_at)
		SELECT context, rate_sheet_name, $3, 'published', $4, fee_scale_override, now()
		FROM pricing.rate_sheet_versions WHERE context = $1 AND rate_sheet_name = $2 AND version = $4`,
		contextKey, name, next, targetVersion); err != nil {
		return domain.RateSheetVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.rate_sheet_entries (context, rate_sheet_name, version, route_key, vehicle_type, cent_base_rate, drop_point_count, cent_additional_drop_rate)
		SELECT context, rate_sheet_name, $3, route_key, vehicle_type, cent_base_rate, drop_point_count, cent_additional_drop_rate
		FROM pricing.rate_sheet_entries WHERE context = $1 AND rate_sheet_name = $2 AND version = $4`,
		contextKey, name, next, targetVersion); err != nil {
		return domain.RateSheetVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pricing.rate_sheet_versions SET status = 'rolled-back', rolled_back_by = $3
		WHERE context = $1 AND rate_sheet_name = $2 AND version = $4`,
		contextKey, name, next, current); err != nil {
		return domain.RateSheetVersion{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.RateSheetVersion{}, err
	}
	return domain.RateSheetVersion{Context: contextKey, Version: next, Status: domain.VersionPublished, ParentVersion: &targetVersion}, nil
}

func (r *RateSheetRepository) Versions(ctx context.Context, contextKey, name string) ([]domain.RateSheetVersion, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT version, status, parent_version, rolled_back_by, fee_scale_override
		FROM pricing.rate_sheet_versions WHERE context = $1 AND rate_sheet_name = $2 ORDER BY version DESC`,
		contextKey, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []domain.RateSheetVersion{}
	for rows.Next() {
		v := domain.RateSheetVersion{Context: contextKey}
		var parentVersion, rolledBackBy sql.NullInt64
		var feeScaleOverride sql.NullString
		if err := rows.Scan(&v.Version, &v.Status, &parentVersion, &rolledBackBy, &feeScaleOverride); err != nil {
			return nil, err
		}
		if parentVersion.Valid {
			pv := int(parentVersion.Int64)
			v.ParentVersion = &pv
		}
		if rolledBackBy.Valid {
			rb := int(rolledBackBy.Int64)
			v.RolledBackBy = &rb
		}
		if feeScaleOverride.Valid {
			v.FeeScaleOverride = &feeScaleOverride.String
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (r *RateSheetRepository) ActiveVersion(ctx context.Context, contextKey, name string) (domain.RateSheetVersion, error) {
	var active bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT active FROM pricing.rate_sheets WHERE context = $1 AND name = $2`,
		contextKey, name).Scan(&active); errors.Is(err, sql.ErrNoRows) {
		return domain.RateSheetVersion{}, domain.ErrRateSheetNotFound
	} else if err != nil {
		return domain.RateSheetVersion{}, err
	}
	if !active {
		return domain.RateSheetVersion{}, domain.ErrNoActiveRateSheetVersion
	}

	v := domain.RateSheetVersion{Context: contextKey, Status: domain.VersionPublished}
	var feeScaleOverride sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT version, minor_version, fee_scale_override FROM pricing.rate_sheet_versions
		WHERE context = $1 AND rate_sheet_name = $2 AND status = 'published' ORDER BY version DESC LIMIT 1`,
		contextKey, name).Scan(&v.Version, &v.MinorVersion, &feeScaleOverride)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RateSheetVersion{}, domain.ErrNoActiveRateSheetVersion
	}
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	if feeScaleOverride.Valid {
		v.FeeScaleOverride = &feeScaleOverride.String
	}

	entryRows, err := r.db.QueryContext(ctx, `
		SELECT route_key, vehicle_type, cent_base_rate, drop_point_count, cent_additional_drop_rate,
		       diesel_pct, initial_diesel_cents
		FROM pricing.rate_sheet_entries WHERE context = $1 AND rate_sheet_name = $2 AND version = $3`,
		contextKey, name, v.Version)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	defer entryRows.Close()
	for entryRows.Next() {
		var e domain.RateSheetEntry
		if err := entryRows.Scan(
			&e.RouteKey, &e.VehicleType, &e.CentBaseRate, &e.DropPointCount, &e.CentAdditionalDropRate,
			&e.DieselPct, &e.InitialDieselCents,
		); err != nil {
			return domain.RateSheetVersion{}, err
		}
		v.Entries = append(v.Entries, e)
	}
	if err := entryRows.Err(); err != nil {
		return domain.RateSheetVersion{}, err
	}

	overlayRows, err := r.db.QueryContext(ctx, `
		SELECT minor_version, route_key, vehicle_type, start_date, end_date, cent_adjusted_rate
		FROM pricing.rate_sheet_overlays
		WHERE context = $1 AND rate_sheet_name = $2 AND version = $3
		ORDER BY minor_version ASC, route_key, vehicle_type`,
		contextKey, name, v.Version)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	defer overlayRows.Close()
	for overlayRows.Next() {
		var o domain.DieselOverlay
		var endDate sql.NullTime
		if err := overlayRows.Scan(
			&o.MinorVersion, &o.RouteKey, &o.VehicleType, &o.StartDate, &endDate, &o.CentAdjustedRate,
		); err != nil {
			return domain.RateSheetVersion{}, err
		}
		if endDate.Valid {
			t := endDate.Time
			o.EndDate = &t
		}
		v.Overlays = append(v.Overlays, o)
	}
	return v, overlayRows.Err()
}

// IndexDieselPrice upserts a diesel price row for the context (BR-P18).
func (r *RateSheetRepository) IndexDieselPrice(ctx context.Context, contextKey string, price domain.DieselPrice) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pricing.diesel_prices (context, active_date, coastal_cents, inland_cents)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (context, active_date) DO UPDATE SET coastal_cents = $3, inland_cents = $4`,
		contextKey, price.ActiveDate.UTC().Truncate(24*time.Hour),
		price.CoastalCents, price.InlandCents)
	return err
}

// ListDieselPrices returns every diesel price for the context ordered by
// active_date ascending (BR-P18 — callers use DieselPriceOn to resolve).
func (r *RateSheetRepository) ListDieselPrices(ctx context.Context, contextKey string) ([]domain.DieselPrice, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT active_date, coastal_cents, inland_cents FROM pricing.diesel_prices
		WHERE context = $1 ORDER BY active_date ASC`,
		contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var prices []domain.DieselPrice
	for rows.Next() {
		var p domain.DieselPrice
		if err := rows.Scan(&p.ActiveDate, &p.CoastalCents, &p.InlandCents); err != nil {
			return nil, err
		}
		prices = append(prices, p)
	}
	return prices, rows.Err()
}

// PersistDieselOverlay writes the result of a domain.AppendDieselOverlay call
// (BR-P20): bumps minor_version on the version row, closes overlay windows
// whose EndDate was just set, and inserts the new minor-version overlay rows.
// All three writes are transactional.
func (r *RateSheetRepository) PersistDieselOverlay(ctx context.Context, contextKey, name string, v domain.RateSheetVersion) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `
		UPDATE pricing.rate_sheet_versions SET minor_version = $4
		WHERE context = $1 AND rate_sheet_name = $2 AND version = $3`,
		contextKey, name, v.Version, v.MinorVersion); err != nil {
		return err
	}

	for _, o := range v.Overlays {
		if o.MinorVersion < v.MinorVersion {
			// Previously-existing overlay — update its end_date if now closed.
			if o.EndDate != nil {
				if _, err := tx.ExecContext(ctx, `
					UPDATE pricing.rate_sheet_overlays SET end_date = $7
					WHERE context = $1 AND rate_sheet_name = $2 AND version = $3
					  AND minor_version = $4 AND route_key = $5 AND vehicle_type = $6`,
					contextKey, name, v.Version,
					o.MinorVersion, o.RouteKey, o.VehicleType,
					o.EndDate.UTC().Truncate(24*time.Hour)); err != nil {
					return err
				}
			}
		} else {
			// New overlay — insert.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO pricing.rate_sheet_overlays
					(context, rate_sheet_name, version, minor_version, route_key, vehicle_type,
					 start_date, end_date, cent_adjusted_rate)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				contextKey, name, v.Version, o.MinorVersion, o.RouteKey, o.VehicleType,
				o.StartDate.UTC().Truncate(24*time.Hour), nil, o.CentAdjustedRate); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}
