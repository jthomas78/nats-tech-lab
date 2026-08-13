package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// FixedRateRepository is the Postgres adapter for domain.FixedRateRepository.
type FixedRateRepository struct{ db *sql.DB }

func NewFixedRateRepository(db *sql.DB) *FixedRateRepository { return &FixedRateRepository{db: db} }

func (r *FixedRateRepository) Register(ctx context.Context, fr domain.FixedRate) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pricing.fixed_rates (context, name, customer_key, route_key, active)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (context, name) DO UPDATE SET customer_key = $3, route_key = $4, active = $5`,
		fr.Context, fr.Name, fr.CustomerKey, fr.RouteKey, fr.Active)
	return err
}

func (r *FixedRateRepository) Get(ctx context.Context, contextKey, name string) (domain.FixedRate, error) {
	var fr domain.FixedRate
	err := r.db.QueryRowContext(ctx, `
		SELECT context, name, customer_key, route_key, active FROM pricing.fixed_rates WHERE context = $1 AND name = $2`,
		contextKey, name).Scan(&fr.Context, &fr.Name, &fr.CustomerKey, &fr.RouteKey, &fr.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FixedRate{}, domain.ErrFixedRateNotFound
	}
	return fr, err
}

func (r *FixedRateRepository) List(ctx context.Context, contextKey string) ([]domain.FixedRate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, name, customer_key, route_key, active FROM pricing.fixed_rates WHERE context = $1 ORDER BY name`,
		contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.FixedRate{}
	for rows.Next() {
		var fr domain.FixedRate
		if err := rows.Scan(&fr.Context, &fr.Name, &fr.CustomerKey, &fr.RouteKey, &fr.Active); err != nil {
			return nil, err
		}
		all = append(all, fr)
	}
	return all, rows.Err()
}

func (r *FixedRateRepository) CreateDraft(ctx context.Context, contextKey, name string, centRate int64, pointCount int, centAdditionalDropRate int64) (domain.FixedRateVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FixedRateVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var version int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM pricing.fixed_rate_versions WHERE context = $1 AND fixed_rate_name = $2`,
		contextKey, name).Scan(&version); err != nil {
		return domain.FixedRateVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.fixed_rate_versions (context, fixed_rate_name, version, status, cent_rate, point_count, cent_additional_drop_rate)
		VALUES ($1, $2, $3, 'draft', $4, $5, $6)`,
		contextKey, name, version, centRate, pointCount, centAdditionalDropRate); err != nil {
		return domain.FixedRateVersion{}, mapUniqueViolation(err, "one_fixed_rate_draft", domain.ErrFixedRateDraftAlreadyExists)
	}

	if err := tx.Commit(); err != nil {
		return domain.FixedRateVersion{}, err
	}
	return domain.FixedRateVersion{
		Context: contextKey, Version: version, Status: domain.VersionDraft,
		CentRate: centRate, PointCount: pointCount, CentAdditionalDropRate: centAdditionalDropRate,
	}, nil
}

func (r *FixedRateRepository) Publish(ctx context.Context, contextKey, name string) (domain.FixedRateVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FixedRateVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var v domain.FixedRateVersion
	v.Context, v.Status = contextKey, domain.VersionPublished
	err = tx.QueryRowContext(ctx, `
		UPDATE pricing.fixed_rate_versions SET status = 'published', published_at = now()
		WHERE context = $1 AND fixed_rate_name = $2 AND status = 'draft'
		RETURNING version, cent_rate, point_count, cent_additional_drop_rate`, contextKey, name).
		Scan(&v.Version, &v.CentRate, &v.PointCount, &v.CentAdditionalDropRate)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FixedRateVersion{}, domain.ErrFixedRateDraftNotFound
	}
	if err != nil {
		return domain.FixedRateVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.FixedRateVersion{}, err
	}
	return v, nil
}

func (r *FixedRateRepository) Rollback(ctx context.Context, contextKey, name string, targetVersion int) (domain.FixedRateVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FixedRateVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var targetStatus domain.VersionStatus
	var centRate int64
	var pointCount int
	var centAdditionalDropRate int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, cent_rate, point_count, cent_additional_drop_rate FROM pricing.fixed_rate_versions
		WHERE context = $1 AND fixed_rate_name = $2 AND version = $3`,
		contextKey, name, targetVersion).Scan(&targetStatus, &centRate, &pointCount, &centAdditionalDropRate); errors.Is(err, sql.ErrNoRows) {
		return domain.FixedRateVersion{}, domain.ErrFixedRateRollbackTargetNotPublished
	} else if err != nil {
		return domain.FixedRateVersion{}, err
	}
	if err := domain.CanRollbackFixedRateTo(domain.FixedRateVersion{Status: targetStatus}); err != nil {
		return domain.FixedRateVersion{}, err
	}

	var current int
	if err := tx.QueryRowContext(ctx, `
		SELECT version FROM pricing.fixed_rate_versions WHERE context = $1 AND fixed_rate_name = $2 AND status = 'published' ORDER BY version DESC LIMIT 1`,
		contextKey, name).Scan(&current); err != nil {
		return domain.FixedRateVersion{}, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM pricing.fixed_rate_versions WHERE context = $1 AND fixed_rate_name = $2`,
		contextKey, name).Scan(&next); err != nil {
		return domain.FixedRateVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.fixed_rate_versions (context, fixed_rate_name, version, status, parent_version, cent_rate, point_count, cent_additional_drop_rate, published_at)
		VALUES ($1, $2, $3, 'published', $4, $5, $6, $7, now())`,
		contextKey, name, next, targetVersion, centRate, pointCount, centAdditionalDropRate); err != nil {
		return domain.FixedRateVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pricing.fixed_rate_versions SET status = 'rolled-back', rolled_back_by = $3
		WHERE context = $1 AND fixed_rate_name = $2 AND version = $4`,
		contextKey, name, next, current); err != nil {
		return domain.FixedRateVersion{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.FixedRateVersion{}, err
	}
	return domain.FixedRateVersion{
		Context: contextKey, Version: next, Status: domain.VersionPublished, ParentVersion: &targetVersion,
		CentRate: centRate, PointCount: pointCount, CentAdditionalDropRate: centAdditionalDropRate,
	}, nil
}

func (r *FixedRateRepository) Versions(ctx context.Context, contextKey, name string) ([]domain.FixedRateVersion, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT version, status, parent_version, rolled_back_by, cent_rate, point_count, cent_additional_drop_rate
		FROM pricing.fixed_rate_versions WHERE context = $1 AND fixed_rate_name = $2 ORDER BY version DESC`,
		contextKey, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []domain.FixedRateVersion{}
	for rows.Next() {
		v := domain.FixedRateVersion{Context: contextKey}
		var parentVersion, rolledBackBy sql.NullInt64
		if err := rows.Scan(&v.Version, &v.Status, &parentVersion, &rolledBackBy, &v.CentRate, &v.PointCount, &v.CentAdditionalDropRate); err != nil {
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
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (r *FixedRateRepository) ActiveVersion(ctx context.Context, contextKey, name string) (domain.FixedRateVersion, error) {
	var active bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT active FROM pricing.fixed_rates WHERE context = $1 AND name = $2`,
		contextKey, name).Scan(&active); errors.Is(err, sql.ErrNoRows) {
		return domain.FixedRateVersion{}, domain.ErrFixedRateNotFound
	} else if err != nil {
		return domain.FixedRateVersion{}, err
	}
	if !active {
		return domain.FixedRateVersion{}, domain.ErrNoActiveFixedRateVersion
	}

	v := domain.FixedRateVersion{Context: contextKey, Status: domain.VersionPublished}
	err := r.db.QueryRowContext(ctx, `
		SELECT version, cent_rate, point_count, cent_additional_drop_rate FROM pricing.fixed_rate_versions
		WHERE context = $1 AND fixed_rate_name = $2 AND status = 'published' ORDER BY version DESC LIMIT 1`,
		contextKey, name).Scan(&v.Version, &v.CentRate, &v.PointCount, &v.CentAdditionalDropRate)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FixedRateVersion{}, domain.ErrNoActiveFixedRateVersion
	}
	return v, err
}
