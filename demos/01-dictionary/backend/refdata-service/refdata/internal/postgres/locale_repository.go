package postgres

import (
	"context"
	"database/sql"
	"errors"
)

type LocaleRepository struct {
	db *sql.DB
}

func NewLocaleRepository(db *sql.DB) *LocaleRepository { return &LocaleRepository{db: db} }

// Add registers a locale as known for a context. If isDefault, any other
// locale previously marked default for the same context is unmarked first,
// atomically, so at most one default ever exists per context.
func (r *LocaleRepository) Add(ctx context.Context, itemContext, locale string, isDefault bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if isDefault {
		if _, err := tx.ExecContext(ctx, `
			UPDATE refdata.dictionary_locales SET is_default = false WHERE context = $1`,
			itemContext); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO refdata.dictionary_locales (context, locale, is_default)
		VALUES ($1, $2, $3)
		ON CONFLICT (context, locale) DO UPDATE SET is_default = $3`,
		itemContext, locale, isDefault); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *LocaleRepository) List(ctx context.Context, itemContext string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT locale FROM refdata.dictionary_locales WHERE context = $1 ORDER BY locale`,
		itemContext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	locales := []string{}
	for rows.Next() {
		var locale string
		if err := rows.Scan(&locale); err != nil {
			return nil, err
		}
		locales = append(locales, locale)
	}
	return locales, rows.Err()
}

func (r *LocaleRepository) Default(ctx context.Context, itemContext string) (string, error) {
	var locale string
	err := r.db.QueryRowContext(ctx, `
		SELECT locale FROM refdata.dictionary_locales WHERE context = $1 AND is_default = true`,
		itemContext).Scan(&locale)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return locale, err
}
