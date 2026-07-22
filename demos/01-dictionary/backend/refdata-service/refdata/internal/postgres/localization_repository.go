package postgres

import (
	"context"
	"database/sql"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

type LocalizationRepository struct {
	db *sql.DB
}

func NewLocalizationRepository(db *sql.DB) *LocalizationRepository {
	return &LocalizationRepository{db: db}
}

func (r *LocalizationRepository) Upsert(ctx context.Context, loc domain.Localization) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO refdata.dictionary_localizations (context, type_key, code, locale, label, description, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (context, type_key, code, locale)
		DO UPDATE SET label = $5, description = $6, source = $7`,
		loc.Context, loc.TypeKey, loc.Code, loc.Locale, loc.Label, loc.Description, loc.Source)
	return err
}

func (r *LocalizationRepository) ListForItem(ctx context.Context, typeKey, itemContext, code string) ([]domain.Localization, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, type_key, code, locale, label, description, source
		FROM refdata.dictionary_localizations
		WHERE context = $1 AND type_key = $2 AND code = $3`,
		itemContext, typeKey, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	locs := []domain.Localization{}
	for rows.Next() {
		var loc domain.Localization
		if err := rows.Scan(&loc.Context, &loc.TypeKey, &loc.Code, &loc.Locale, &loc.Label, &loc.Description, &loc.Source); err != nil {
			return nil, err
		}
		locs = append(locs, loc)
	}
	return locs, rows.Err()
}

func (r *LocalizationRepository) CountLocalized(ctx context.Context, typeKey, itemContext, locale string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM refdata.dictionary_localizations
		WHERE context = $1 AND type_key = $2 AND locale = $3`,
		itemContext, typeKey, locale).Scan(&count)
	return count, err
}
