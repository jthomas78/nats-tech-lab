package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// FeeScaleRepository is the Postgres adapter for domain.FeeScaleRepository.
// Publish and Rollback are transaction-bound so a partial version can never
// be observed.
type FeeScaleRepository struct{ db *sql.DB }

func NewFeeScaleRepository(db *sql.DB) *FeeScaleRepository { return &FeeScaleRepository{db: db} }

func (r *FeeScaleRepository) Register(ctx context.Context, fs domain.FeeScale) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pricing.fee_scales (context, name, deleted)
		VALUES ($1, $2, $3)
		ON CONFLICT (context, name) DO UPDATE SET deleted = $3`,
		fs.Context, fs.Name, fs.Deleted)
	return err
}

func (r *FeeScaleRepository) Get(ctx context.Context, contextKey, name string) (domain.FeeScale, error) {
	var fs domain.FeeScale
	err := r.db.QueryRowContext(ctx, `
		SELECT context, name, deleted FROM pricing.fee_scales WHERE context = $1 AND name = $2`,
		contextKey, name).Scan(&fs.Context, &fs.Name, &fs.Deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FeeScale{}, domain.ErrFeeScaleNotFound
	}
	return fs, err
}

func (r *FeeScaleRepository) List(ctx context.Context, contextKey string) ([]domain.FeeScale, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, name, deleted FROM pricing.fee_scales WHERE context = $1 ORDER BY name`,
		contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.FeeScale{}
	for rows.Next() {
		var fs domain.FeeScale
		if err := rows.Scan(&fs.Context, &fs.Name, &fs.Deleted); err != nil {
			return nil, err
		}
		all = append(all, fs)
	}
	return all, rows.Err()
}

func (r *FeeScaleRepository) CreateDraft(ctx context.Context, contextKey, name string) (domain.FeeScaleVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FeeScaleVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var version int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM pricing.fee_scale_versions WHERE context = $1 AND fee_scale_name = $2`,
		contextKey, name).Scan(&version); err != nil {
		return domain.FeeScaleVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.fee_scale_versions (context, fee_scale_name, version, status)
		VALUES ($1, $2, $3, 'draft')`,
		contextKey, name, version); err != nil {
		// The partial unique index is the concurrency-safe backstop for BR-P02.
		return domain.FeeScaleVersion{}, mapUniqueViolation(err, "one_fee_scale_draft", domain.ErrDraftAlreadyExists)
	}

	if err := tx.Commit(); err != nil {
		return domain.FeeScaleVersion{}, err
	}
	return domain.FeeScaleVersion{Context: contextKey, Version: version, Status: domain.VersionDraft}, nil
}

func (r *FeeScaleRepository) AddRange(ctx context.Context, contextKey, name string, version int, rng domain.FeeScaleRange) error {
	if err := domain.ValidateRange(rng); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pricing.fee_scale_ranges (context, fee_scale_name, version, cent_lower_limit, cent_upper_limit, rate_type, cent_fee, percentage_fee)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		contextKey, name, version, rng.CentLowerLimit, rng.CentUpperLimit, rng.RateType, rng.CentFee, rng.PercentageFee)
	return err
}

func (r *FeeScaleRepository) Publish(ctx context.Context, contextKey, name string) (domain.FeeScaleVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FeeScaleVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var version int
	err = tx.QueryRowContext(ctx, `
		UPDATE pricing.fee_scale_versions SET status = 'published', published_at = now()
		WHERE context = $1 AND fee_scale_name = $2 AND status = 'draft'
		RETURNING version`, contextKey, name).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FeeScaleVersion{}, domain.ErrFeeScaleDraftNotFound
	}
	if err != nil {
		return domain.FeeScaleVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.FeeScaleVersion{}, err
	}
	return domain.FeeScaleVersion{Context: contextKey, Version: version, Status: domain.VersionPublished}, nil
}

func (r *FeeScaleRepository) Rollback(ctx context.Context, contextKey, name string, targetVersion int) (domain.FeeScaleVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FeeScaleVersion{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var targetStatus domain.VersionStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT status FROM pricing.fee_scale_versions WHERE context = $1 AND fee_scale_name = $2 AND version = $3`,
		contextKey, name, targetVersion).Scan(&targetStatus); errors.Is(err, sql.ErrNoRows) {
		return domain.FeeScaleVersion{}, domain.ErrRollbackTargetNotPublished
	} else if err != nil {
		return domain.FeeScaleVersion{}, err
	}
	if err := domain.CanRollbackTo(domain.FeeScaleVersion{Status: targetStatus}); err != nil {
		return domain.FeeScaleVersion{}, err
	}

	var current int
	if err := tx.QueryRowContext(ctx, `
		SELECT version FROM pricing.fee_scale_versions WHERE context = $1 AND fee_scale_name = $2 AND status = 'published' ORDER BY version DESC LIMIT 1`,
		contextKey, name).Scan(&current); err != nil {
		return domain.FeeScaleVersion{}, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM pricing.fee_scale_versions WHERE context = $1 AND fee_scale_name = $2`,
		contextKey, name).Scan(&next); err != nil {
		return domain.FeeScaleVersion{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.fee_scale_versions (context, fee_scale_name, version, status, parent_version, published_at)
		VALUES ($1, $2, $3, 'published', $4, now())`,
		contextKey, name, next, targetVersion); err != nil {
		return domain.FeeScaleVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pricing.fee_scale_ranges (context, fee_scale_name, version, cent_lower_limit, cent_upper_limit, rate_type, cent_fee, percentage_fee)
		SELECT context, fee_scale_name, $3, cent_lower_limit, cent_upper_limit, rate_type, cent_fee, percentage_fee
		FROM pricing.fee_scale_ranges WHERE context = $1 AND fee_scale_name = $2 AND version = $4`,
		contextKey, name, next, targetVersion); err != nil {
		return domain.FeeScaleVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pricing.fee_scale_versions SET status = 'rolled-back', rolled_back_by = $3
		WHERE context = $1 AND fee_scale_name = $2 AND version = $4`,
		contextKey, name, next, current); err != nil {
		return domain.FeeScaleVersion{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.FeeScaleVersion{}, err
	}
	return domain.FeeScaleVersion{Context: contextKey, Version: next, Status: domain.VersionPublished, ParentVersion: &targetVersion}, nil
}

func (r *FeeScaleRepository) Versions(ctx context.Context, contextKey, name string) ([]domain.FeeScaleVersion, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT version, status, parent_version, rolled_back_by
		FROM pricing.fee_scale_versions WHERE context = $1 AND fee_scale_name = $2 ORDER BY version DESC`,
		contextKey, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []domain.FeeScaleVersion{}
	for rows.Next() {
		v := domain.FeeScaleVersion{Context: contextKey}
		var parentVersion, rolledBackBy sql.NullInt64
		if err := rows.Scan(&v.Version, &v.Status, &parentVersion, &rolledBackBy); err != nil {
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

func (r *FeeScaleRepository) ActiveVersion(ctx context.Context, contextKey, name string) (domain.FeeScaleVersion, error) {
	var v domain.FeeScaleVersion
	v.Context = contextKey
	v.Status = domain.VersionPublished
	err := r.db.QueryRowContext(ctx, `
		SELECT version FROM pricing.fee_scale_versions
		WHERE context = $1 AND fee_scale_name = $2 AND status = 'published' ORDER BY version DESC LIMIT 1`,
		contextKey, name).Scan(&v.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FeeScaleVersion{}, domain.ErrNoActiveFeeScaleVersion
	}
	if err != nil {
		return domain.FeeScaleVersion{}, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT cent_lower_limit, cent_upper_limit, rate_type, cent_fee, percentage_fee
		FROM pricing.fee_scale_ranges WHERE context = $1 AND fee_scale_name = $2 AND version = $3
		ORDER BY cent_upper_limit ASC`,
		contextKey, name, v.Version)
	if err != nil {
		return domain.FeeScaleVersion{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var rng domain.FeeScaleRange
		if err := rows.Scan(&rng.CentLowerLimit, &rng.CentUpperLimit, &rng.RateType, &rng.CentFee, &rng.PercentageFee); err != nil {
			return domain.FeeScaleVersion{}, err
		}
		v.Ranges = append(v.Ranges, rng)
	}
	return v, rows.Err()
}
