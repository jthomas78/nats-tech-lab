package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// mapUniqueViolation translates a Postgres unique-violation on the named
// constraint into mapped, leaving every other error untouched. Mirrors
// pricing-service's own postgres/errors.go helper.
func mapUniqueViolation(err error, constraint string, mapped error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint {
		return mapped
	}
	return err
}
