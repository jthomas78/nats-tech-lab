// Package postgres implements the Shape B canonical projection repository.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Upsert(ctx context.Context, entry domain.DictionaryEntry) (domain.DictionaryEntry, error) {
	attrs, err := json.Marshal(entry.Attributes)
	if err != nil {
		return domain.DictionaryEntry{}, fmt.Errorf("marshal attributes: %w", err)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dictionary_entries (context, entity_type, id, label, attributes, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 1, $6, $7)
		ON CONFLICT (context, entity_type, id) DO UPDATE
		SET label      = EXCLUDED.label,
		    attributes = EXCLUDED.attributes,
		    version    = dictionary_entries.version + 1,
		    updated_at = EXCLUDED.updated_at
		RETURNING version, created_at`,
		entry.Context, entry.EntityType, entry.ID, entry.Label, attrs, entry.UpdatedAt, entry.UpdatedAt)
	if err := row.Scan(&entry.Version, &entry.CreatedAt); err != nil {
		return domain.DictionaryEntry{}, fmt.Errorf("upsert entry: %w", err)
	}
	return entry, nil
}

func (r *Repository) Find(ctx context.Context, kvContext, entityType, id string) (domain.DictionaryEntry, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT context, entity_type, id, label, attributes, version, created_at, updated_at
		FROM dictionary_entries
		WHERE context = $1 AND entity_type = $2 AND id = $3`,
		kvContext, entityType, id)
	entry, err := scanEntry(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DictionaryEntry{}, domain.ErrNotFound
	}
	return entry, err
}

func (r *Repository) List(ctx context.Context, kvContext string) ([]domain.DictionaryEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT context, entity_type, id, label, attributes, version, created_at, updated_at
		FROM dictionary_entries
		WHERE context = $1
		ORDER BY entity_type, id`,
		kvContext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]domain.DictionaryEntry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows.Scan)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func scanEntry(scan func(...any) error) (domain.DictionaryEntry, error) {
	var entry domain.DictionaryEntry
	var attrs []byte
	if err := scan(&entry.Context, &entry.EntityType, &entry.ID, &entry.Label, &attrs, &entry.Version, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
		return domain.DictionaryEntry{}, err
	}
	if err := json.Unmarshal(attrs, &entry.Attributes); err != nil {
		return domain.DictionaryEntry{}, fmt.Errorf("unmarshal attributes: %w", err)
	}
	return entry, nil
}
