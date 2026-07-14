package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
)

type ReferenceRepository struct {
	db *sql.DB
}

func NewReferenceRepository(db *sql.DB) *ReferenceRepository { return &ReferenceRepository{db: db} }

func (r *ReferenceRepository) Create(ctx context.Context, ref domain.DictionaryReference) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO refdata.dictionary_references (context, from_type_key, from_code, relation, to_type_key, to_code)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (context, from_type_key, from_code, relation)
		DO UPDATE SET to_type_key = $5, to_code = $6`,
		ref.Context, ref.FromTypeKey, ref.FromCode, ref.Relation, ref.ToTypeKey, ref.ToCode)
	return err
}

func (r *ReferenceRepository) IsReferenced(ctx context.Context, typeKey, itemContext, code string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM refdata.dictionary_references
			WHERE context = $1 AND to_type_key = $2 AND to_code = $3
		)`,
		itemContext, typeKey, code).Scan(&exists)
	return exists, err
}

func (r *ReferenceRepository) Get(ctx context.Context, itemContext, fromTypeKey, fromCode, relation string) (domain.DictionaryReference, error) {
	ref := domain.DictionaryReference{Context: itemContext, FromTypeKey: fromTypeKey, FromCode: fromCode, Relation: relation}
	err := r.db.QueryRowContext(ctx, `
		SELECT to_type_key, to_code FROM refdata.dictionary_references
		WHERE context = $1 AND from_type_key = $2 AND from_code = $3 AND relation = $4`,
		itemContext, fromTypeKey, fromCode, relation).Scan(&ref.ToTypeKey, &ref.ToCode)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DictionaryReference{}, domain.ErrReferenceNotFound
	}
	return ref, err
}

func (r *ReferenceRepository) ListFrom(ctx context.Context, itemContext, fromTypeKey, fromCode string) ([]domain.DictionaryReference, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT relation, to_type_key, to_code FROM refdata.dictionary_references
		WHERE context = $1 AND from_type_key = $2 AND from_code = $3`,
		itemContext, fromTypeKey, fromCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	refs := []domain.DictionaryReference{}
	for rows.Next() {
		ref := domain.DictionaryReference{Context: itemContext, FromTypeKey: fromTypeKey, FromCode: fromCode}
		if err := rows.Scan(&ref.Relation, &ref.ToTypeKey, &ref.ToCode); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
