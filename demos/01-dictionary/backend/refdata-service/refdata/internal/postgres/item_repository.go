package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

type ItemRepository struct {
	db *sql.DB
}

func NewItemRepository(db *sql.DB) *ItemRepository { return &ItemRepository{db: db} }

func (r *ItemRepository) Exists(ctx context.Context, typeKey, itemContext, code string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM refdata.dictionary_items WHERE context = $1 AND type_key = $2 AND code = $3)`,
		itemContext, typeKey, code).Scan(&exists)
	return exists, err
}

func (r *ItemRepository) Create(ctx context.Context, item domain.DictionaryItem) error {
	attrs, err := json.Marshal(item.Attrs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO refdata.dictionary_items (context, type_key, code, status, attrs)
		VALUES ($1, $2, $3, $4, $5)`,
		item.Context, item.TypeKey, item.Code, string(item.Status), attrs)
	return err
}

func (r *ItemRepository) Get(ctx context.Context, typeKey, itemContext, code string) (domain.DictionaryItem, error) {
	var item domain.DictionaryItem
	var status string
	var attrs []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT context, type_key, code, status, attrs
		FROM refdata.dictionary_items WHERE context = $1 AND type_key = $2 AND code = $3`,
		itemContext, typeKey, code).Scan(&item.Context, &item.TypeKey, &item.Code, &status, &attrs)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DictionaryItem{}, domain.ErrItemNotFound
	}
	if err != nil {
		return domain.DictionaryItem{}, err
	}
	item.Status = domain.ItemStatus(status)
	if err := json.Unmarshal(attrs, &item.Attrs); err != nil {
		return domain.DictionaryItem{}, err
	}
	return item, nil
}

func (r *ItemRepository) List(ctx context.Context, typeKey, itemContext string) ([]domain.DictionaryItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, type_key, code, status, attrs
		FROM refdata.dictionary_items WHERE context = $1 AND type_key = $2 ORDER BY code`,
		itemContext, typeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []domain.DictionaryItem{}
	for rows.Next() {
		var item domain.DictionaryItem
		var status string
		var attrs []byte
		if err := rows.Scan(&item.Context, &item.TypeKey, &item.Code, &status, &attrs); err != nil {
			return nil, err
		}
		item.Status = domain.ItemStatus(status)
		if err := json.Unmarshal(attrs, &item.Attrs); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ItemRepository) Deprecate(ctx context.Context, typeKey, itemContext, code string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE refdata.dictionary_items SET status = 'deprecated', updated_at = now()
		WHERE context = $1 AND type_key = $2 AND code = $3`,
		itemContext, typeKey, code)
	return err
}

func (r *ItemRepository) Reactivate(ctx context.Context, typeKey, itemContext, code string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE refdata.dictionary_items SET status = 'active', updated_at = now()
		WHERE context = $1 AND type_key = $2 AND code = $3`,
		itemContext, typeKey, code)
	return err
}

func (r *ItemRepository) UpdateAttrs(ctx context.Context, typeKey, itemContext, code string, attrs map[string]any) error {
	marshaled, err := json.Marshal(attrs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE refdata.dictionary_items SET attrs = $1, updated_at = now()
		WHERE context = $2 AND type_key = $3 AND code = $4`,
		marshaled, itemContext, typeKey, code)
	return err
}

func (r *ItemRepository) Delete(ctx context.Context, typeKey, itemContext, code string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM refdata.dictionary_items WHERE context = $1 AND type_key = $2 AND code = $3`,
		itemContext, typeKey, code)
	return err
}
