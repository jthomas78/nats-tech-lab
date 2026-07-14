package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
)

type TypeRepository struct {
	db *sql.DB
}

func NewTypeRepository(db *sql.DB) *TypeRepository { return &TypeRepository{db: db} }

func (r *TypeRepository) Register(ctx context.Context, t domain.DictionaryType) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO refdata.dictionary_types (type_key, name, description)
		VALUES ($1, $2, $3)
		ON CONFLICT (type_key) DO UPDATE SET name = $2, description = $3`,
		t.TypeKey, t.Name, t.Description)
	return err
}

func (r *TypeRepository) Get(ctx context.Context, typeKey string) (domain.DictionaryType, error) {
	var t domain.DictionaryType
	err := r.db.QueryRowContext(ctx, `
		SELECT type_key, name, description FROM refdata.dictionary_types WHERE type_key = $1`,
		typeKey).Scan(&t.TypeKey, &t.Name, &t.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DictionaryType{}, domain.ErrTypeNotFound
	}
	return t, err
}

func (r *TypeRepository) List(ctx context.Context) ([]domain.DictionaryType, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT type_key, name, description FROM refdata.dictionary_types ORDER BY type_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []domain.DictionaryType{}
	for rows.Next() {
		var t domain.DictionaryType
		if err := rows.Scan(&t.TypeKey, &t.Name, &t.Description); err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, rows.Err()
}
