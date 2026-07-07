package domain

import "context"

// Repository is the port for the Shape B canonical projection in Postgres.
type Repository interface {
	// Upsert inserts or updates the projection row and returns the entry
	// with its persisted version and createdAt filled in.
	Upsert(ctx context.Context, entry DictionaryEntry) (DictionaryEntry, error)

	// Find returns ErrNotFound when no row exists.
	Find(ctx context.Context, kvContext, entityType, id string) (DictionaryEntry, error)

	List(ctx context.Context, kvContext string) ([]DictionaryEntry, error)
}
