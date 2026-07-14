package postgres

import (
	"context"
	"database/sql"
	"errors"
)

type VersionRepository struct {
	db *sql.DB
}

func NewVersionRepository(db *sql.DB) *VersionRepository { return &VersionRepository{db: db} }

// Bump is a single atomic UPSERT — Postgres guarantees statement-level
// atomicity, so a concurrent bump can never observe or produce a torn write.
func (r *VersionRepository) Bump(ctx context.Context, itemContext, typeKey string) (int, error) {
	var version int
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO refdata.dictionary_set_versions (context, type_key, version)
		VALUES ($1, $2, 1)
		ON CONFLICT (context, type_key) DO UPDATE SET version = refdata.dictionary_set_versions.version + 1
		RETURNING version`,
		itemContext, typeKey).Scan(&version)
	return version, err
}

func (r *VersionRepository) Current(ctx context.Context, itemContext, typeKey string) (int, error) {
	var version int
	err := r.db.QueryRowContext(ctx, `
		SELECT version FROM refdata.dictionary_set_versions WHERE context = $1 AND type_key = $2`,
		itemContext, typeKey).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}
