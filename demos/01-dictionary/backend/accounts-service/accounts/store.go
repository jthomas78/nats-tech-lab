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

// ErrBUNotFound is returned by BU methods when no business unit matches.
var ErrBUNotFound = errors.New("business unit not found")

// ErrBUDuplicate is returned by InsertBusinessUnit when a BU with that name
// already exists for the account.
var ErrBUDuplicate = errors.New("business unit already exists")

// BusinessUnit is one registered business unit for an account (Phase 22,
// BR-AC15).
//
// Name and Context are deliberately distinct (Phase 22b, BR-AC26): Name is the
// free-text English label an operator reads ("Pacific Fleet") and may rename at
// will; Context is the subject-safe slug refdata-service knows the same unit by
// ("acme-pacific-fleet"), and is immutable once written — nothing in
// refdata-service's data tables carries a foreign key back to a context, so a
// rename would silently orphan every item, localization, KV bucket and
// already-published event recorded under the old value.
//
// IsDefault marks the one auto-created business unit every account gets
// (BR-AC28). It is an explicit column rather than a name/slug comparison so
// nothing has to string-match a reserved value to recognize it.
type BusinessUnit struct {
	ID        string
	AccountID string
	Name      string
	Context   string
	Visible   bool
	IsDefault bool
	CreatedAt time.Time
}

// NewBusinessUnit is the input to the two insert paths. A struct rather than a
// positional argument list because the two string fields (Name, Context) are
// easy to transpose at a call site and impossible to tell apart by type.
type NewBusinessUnit struct {
	AccountID string
	Name      string
	Context   string
	Visible   bool
	IsDefault bool
}

// Account is one NATS account this service knows about — both the ones it
// minted itself (Phase 14b) and the three pre-existing accounts from Phase
// 13a/14a's bootstrap (PLATFORM/ACME/GLOBEX), seeded at startup so the list
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
		// Phase 22: per-account business unit registry (BR-AC15/BR-AC16).
		// name mirrors the {context} token in refdata-service and subject taxonomy.
		// FK to accounts so orphaned BUs can't accumulate, but the account row must
		// exist before BUs are registered (auto-create at account-creation time).
		`CREATE TABLE IF NOT EXISTS accounts.business_units (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts.accounts(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			visible    BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (account_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS business_units_account_idx ON accounts.business_units (account_id)`,
		// Phase 22b (BR-AC26/BR-AC27/BR-AC28): split the single `name` column
		// into a free-text display name and an immutable subject-safe `context`
		// slug, and mark the auto-created default explicitly.
		//
		// The backfill is lossless because every name written under Phase 22 was
		// already slug-shaped (it *was* the context token): `acme-pacific-fleet`,
		// `_default_bu`. So `context = name` is the correct history, and only the
		// legacy shared `_default_bu` rows need rewriting — each becomes that
		// account's own `{tenant}-default`, which is the whole point of the
		// phase (one shared context meant two tenants writing the same
		// `(context, type_key, code)` rows in refdata-service).
		//
		// Statement order matters: the `_default_bu` rewrite must land before the
		// global unique index is built, or two accounts both carrying the legacy
		// row would collide on `context` and the index creation would fail.
		`ALTER TABLE accounts.business_units ADD COLUMN IF NOT EXISTS context TEXT`,
		`ALTER TABLE accounts.business_units ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false`,
		`UPDATE accounts.business_units SET context = name WHERE context IS NULL`,
		`UPDATE accounts.business_units bu
			SET name = 'Default', context = a.name || '-default', is_default = true
			FROM accounts.accounts a
			WHERE a.id = bu.account_id AND bu.name = '_default_bu'`,
		`ALTER TABLE accounts.business_units ALTER COLUMN context SET NOT NULL`,
		// Global, not per-account: refdata-service's `contexts.context` is a
		// primary key, and its Register upserts on conflict, so two accounts
		// claiming one slug would let the second silently overwrite the first's
		// context row (name and tenant ownership alike). BR-AC27.
		`CREATE UNIQUE INDEX IF NOT EXISTS business_units_context_key ON accounts.business_units (context)`,
		// BR-AC20: platform-global system config. A singleton row (the
		// CHECK (singleton) + boolean PK guarantees at most one) holding the
		// browser/admin JWT expiry policy. Seeded immediately below with the
		// BR-AC20 defaults so GetTokenTTLConfig always has a row to read.
		`CREATE TABLE IF NOT EXISTS accounts.system_config (
			singleton             BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
			token_ttl_minutes     INT NOT NULL DEFAULT 15,
			token_ttl_min_minutes INT NOT NULL DEFAULT 15,
			token_ttl_max_minutes INT NOT NULL DEFAULT 30,
			updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO accounts.system_config (singleton) VALUES (true) ON CONFLICT (singleton) DO NOTHING`,
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
// PLATFORM/ACME/GLOBEX's Postgres row from their legacy uppercase nsc account
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
// used at startup to register the pre-existing PLATFORM/ACME/GLOBEX accounts
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
// reserved ones (platform/sys, checked case-insensitively against the same
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

// SetJSLimits updates the four JetStream limit columns for an existing
// account. Returns ErrNotFound if no account has that name.
func (s *Store) SetJSLimits(ctx context.Context, name string, limits JSLimits) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts.accounts
		SET js_max_mem = $2, js_max_file = $3, js_max_streams = $4, js_max_consumers = $5, updated_at = now()
		WHERE name = $1`, name, limits.MaxMem, limits.MaxFile, limits.MaxStreams, limits.MaxConsumers)
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

// buColumns is the select list every BU read shares, so a new column can't be
// added to one query and forgotten in the other.
const buColumns = `bu.id, bu.account_id, bu.name, bu.context, bu.visible, bu.is_default, bu.created_at`

func scanBU(dest *BusinessUnit) []any {
	return []any{&dest.ID, &dest.AccountID, &dest.Name, &dest.Context, &dest.Visible, &dest.IsDefault, &dest.CreatedAt}
}

// InsertBusinessUnit adds a new business unit row. Returns ErrBUDuplicate when
// either the display name is already taken within the account or the context
// slug is already taken by any account (BR-AC27 — the slug is globally unique).
func (s *Store) InsertBusinessUnit(ctx context.Context, in NewBusinessUnit) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts.business_units (account_id, name, context, visible, is_default)
		VALUES ($1, $2, $3, $4, $5)`,
		in.AccountID, in.Name, in.Context, in.Visible, in.IsDefault)
	if err != nil && strings.Contains(err.Error(), "unique") {
		return ErrBUDuplicate
	}
	return err
}

// InsertBusinessUnitIfMissing is like InsertBusinessUnit but silently ignores
// conflicts — used for idempotent seeding. Untargeted ON CONFLICT deliberately:
// a seed re-run can collide on either the per-account name or the global
// context slug, and both mean "already seeded".
func (s *Store) InsertBusinessUnitIfMissing(ctx context.Context, in NewBusinessUnit) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts.business_units (account_id, name, context, visible, is_default)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING`,
		in.AccountID, in.Name, in.Context, in.Visible, in.IsDefault)
	return err
}

// ListBusinessUnits returns all business units for the named account. The
// account's default sorts first (BR-AC28 — it is the one row guaranteed to
// exist, and reads as the anchor of the list rather than an alphabetical
// accident), then the rest by display name.
func (s *Store) ListBusinessUnits(ctx context.Context, accountName string) ([]BusinessUnit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+buColumns+`
		FROM accounts.business_units bu
		JOIN accounts.accounts a ON a.id = bu.account_id
		WHERE a.name = $1
		ORDER BY bu.is_default DESC, bu.name`, accountName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BusinessUnit
	for rows.Next() {
		var bu BusinessUnit
		if err := rows.Scan(scanBU(&bu)...); err != nil {
			return nil, err
		}
		out = append(out, bu)
	}
	return out, rows.Err()
}

// GetBusinessUnit returns a specific business unit by account name + context
// slug. Keyed on the slug, not the display name: the slug is the immutable
// identity (BR-AC26), which is what makes it safe to carry in a URL path.
// Returns ErrBUNotFound if it doesn't exist.
func (s *Store) GetBusinessUnit(ctx context.Context, accountName, buContext string) (BusinessUnit, error) {
	var bu BusinessUnit
	err := s.db.QueryRowContext(ctx, `
		SELECT `+buColumns+`
		FROM accounts.business_units bu
		JOIN accounts.accounts a ON a.id = bu.account_id
		WHERE a.name = $1 AND bu.context = $2`, accountName, buContext).
		Scan(scanBU(&bu)...)
	if errors.Is(err, sql.ErrNoRows) {
		return BusinessUnit{}, ErrBUNotFound
	}
	if err != nil {
		return BusinessUnit{}, err
	}
	return bu, nil
}

// RenameBusinessUnit updates a BU's display name. The context slug is never
// touched — it has no rename path anywhere by design (BR-AC26). Returns
// ErrBUNotFound if no such row exists, or ErrBUDuplicate if the account
// already has another unit under that name.
func (s *Store) RenameBusinessUnit(ctx context.Context, accountName, buContext, name string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts.business_units bu
		SET name = $3
		FROM accounts.accounts a
		WHERE a.id = bu.account_id AND a.name = $1 AND bu.context = $2`,
		accountName, buContext, name)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return ErrBUDuplicate
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrBUNotFound
	}
	return nil
}

// SetBusinessUnitVisible updates the visible flag for a BU identified by
// account name + context slug. Returns ErrBUNotFound if no such row exists.
func (s *Store) SetBusinessUnitVisible(ctx context.Context, accountName, buContext string, visible bool) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts.business_units bu
		SET visible = $3
		FROM accounts.accounts a
		WHERE a.id = bu.account_id AND a.name = $1 AND bu.context = $2`,
		accountName, buContext, visible)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrBUNotFound
	}
	return nil
}
