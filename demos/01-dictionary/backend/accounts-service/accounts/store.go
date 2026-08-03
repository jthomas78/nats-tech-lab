package accounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrNotFound is returned by Store.Get when no account has the given name.
var ErrNotFound = errors.New("account not found")

// Account is one NATS account this service knows about — both the ones it
// minted itself (Phase 14b) and the three pre-existing accounts from Phase
// 13a/14a's bootstrap (DEFAULT/ACME/GLOBEX), seeded at startup so the list
// is complete.
//
// SigningKeySeed is plaintext (spike-only, matching the plaintext-fixture
// precedent already established by nats/nats.conf and nats/creds/*.creds —
// see this package's doc comment) and empty for seeded pre-existing
// accounts, since this service never mints new users for them; only
// accounts it created itself have a seed here, needed to mint later users
// or revoke the account.
type Account struct {
	ID             string
	Name           string
	PublicKey      string
	SigningKeySeed string
	Status         string // "active" | "suspended"
	JSMaxMem       int64
	JSMaxFile      int64
	JSMaxStreams   int64
	JSMaxConsumers int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Migrate creates the accounts schema and table if they don't already
// exist. Own schema, own table, own Postgres instance — no datastore of any
// kind is shared with shipping-service or refdata-service (matching their
// own database-per-service isolation).
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS accounts`,
		`CREATE TABLE IF NOT EXISTS accounts.accounts (
			id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name             TEXT NOT NULL UNIQUE,
			public_key       TEXT NOT NULL UNIQUE,
			signing_key_seed TEXT NOT NULL DEFAULT '',
			status           TEXT NOT NULL DEFAULT 'active',
			js_max_mem       BIGINT NOT NULL DEFAULT 0,
			js_max_file      BIGINT NOT NULL DEFAULT 0,
			js_max_streams   BIGINT NOT NULL DEFAULT 0,
			js_max_consumers BIGINT NOT NULL DEFAULT 0,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// BR-AC11 — append-only audit trail for the three lifecycle actions.
		// No foreign key to accounts.accounts: a failed create/suspend/
		// reactivate may still need an audit row for an account name that
		// never ends up with (or has already lost) a corresponding row.
		`CREATE TABLE IF NOT EXISTS accounts.audit_events (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account    TEXT NOT NULL,
			action     TEXT NOT NULL,
			actor      TEXT NOT NULL DEFAULT '',
			source_ip  TEXT NOT NULL DEFAULT '',
			outcome    TEXT NOT NULL DEFAULT 'success',
			metadata   JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS audit_events_account_idx ON accounts.audit_events (account, created_at DESC)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// Store is the Postgres-backed accounts repository.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// RenameIfExists renames an account row from oldName to newName — a no-op
// if oldName doesn't exist. Used once by cmd/main.go's seeding step to move
// DEFAULT/ACME/GLOBEX's Postgres row from their legacy uppercase nsc account
// name to their actual lowercase tenant identity (matching the .creds
// filename/subject/KV-bucket convention every other account already uses),
// without disturbing anything else on the row (public key, status,
// timestamps, etc.). Safe to call every startup: once the rename has
// happened, oldName no longer exists and this does nothing.
func (s *Store) RenameIfExists(ctx context.Context, oldName, newName string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE accounts.accounts SET name = $2, updated_at = now() WHERE name = $1`, oldName, newName)
	return err
}

// SeedIfMissing inserts acc only if no account with that name exists yet —
// used at startup to register the pre-existing DEFAULT/ACME/GLOBEX accounts
// (see cmd/main.go) without re-minting or overwriting anything if this has
// already run.
func (s *Store) SeedIfMissing(ctx context.Context, acc Account) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts.accounts (name, public_key, signing_key_seed, status, js_max_mem, js_max_file, js_max_streams, js_max_consumers)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (name) DO NOTHING`,
		acc.Name, acc.PublicKey, acc.SigningKeySeed, acc.Status,
		acc.JSMaxMem, acc.JSMaxFile, acc.JSMaxStreams, acc.JSMaxConsumers)
	return err
}

// Insert adds a newly-minted account. Fails if the name is already taken.
func (s *Store) Insert(ctx context.Context, acc Account) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts.accounts (name, public_key, signing_key_seed, status, js_max_mem, js_max_file, js_max_streams, js_max_consumers)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		acc.Name, acc.PublicKey, acc.SigningKeySeed, acc.Status,
		acc.JSMaxMem, acc.JSMaxFile, acc.JSMaxStreams, acc.JSMaxConsumers)
	return err
}

// List returns every known account, ordered by name for a stable listing.
func (s *Store) List(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, public_key, signing_key_seed, status, js_max_mem, js_max_file, js_max_streams, js_max_consumers, created_at, updated_at
		FROM accounts.accounts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name, &a.PublicKey, &a.SigningKeySeed, &a.Status,
			&a.JSMaxMem, &a.JSMaxFile, &a.JSMaxStreams, &a.JSMaxConsumers, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Get returns the account named name, or ErrNotFound.
func (s *Store) Get(ctx context.Context, name string) (Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, public_key, signing_key_seed, status, js_max_mem, js_max_file, js_max_streams, js_max_consumers, created_at, updated_at
		FROM accounts.accounts WHERE name = $1`, name).
		Scan(&a.ID, &a.Name, &a.PublicKey, &a.SigningKeySeed, &a.Status,
			&a.JSMaxMem, &a.JSMaxFile, &a.JSMaxStreams, &a.JSMaxConsumers, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

// SetStatus updates an account's status (e.g. suspending it) and returns
// ErrNotFound if no account has that name.
func (s *Store) SetStatus(ctx context.Context, name, status string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts.accounts SET status = $2, updated_at = now() WHERE name = $1`, name, status)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListActiveTenantNames returns every active account name except the
// reserved ones (default/sys, checked case-insensitively against the same
// reservedAccountNames map handler.go's createAccount enforces on write),
// sorted for a stable dropdown order. This is the browser's replacement for
// shipping-service's discoverTenants() creds-directory scan (Phase 13b/14b)
// — used by auth/handler.go's GET /api/auth/tenants, sourced from this same
// Postgres table rather than a second directory scan.
func (s *Store) ListActiveTenantNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM accounts.accounts WHERE status = $1 ORDER BY name`, StatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if reservedAccountNames[strings.ToUpper(name)] {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// SetSigningKeySeed persists a signing key seed established for an account
// that didn't have one on record — a seeded pre-existing account (see
// Account.SigningKeySeed's doc comment) reactivated for the first time, per
// BR-AC04. Returns ErrNotFound if no account has that name.
func (s *Store) SetSigningKeySeed(ctx context.Context, name, seed string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts.accounts SET signing_key_seed = $2, updated_at = now() WHERE name = $1`, name, seed)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
