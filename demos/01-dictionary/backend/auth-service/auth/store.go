package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrNotFound is returned by AccountReader.Get when no account has the given name.
var ErrNotFound = errors.New("account not found")

// account is the subset of accounts-service's accounts.accounts row this
// service needs to mint a browser JWT: identity (PublicKey), signing
// authority (SigningKeySeed), and whether the account is still usable
// (Status). See accounts-service/accounts/store.go for the authoritative
// schema — this service only ever reads it.
type account struct {
	PublicKey      string
	SigningKeySeed string
	Status         string
}

const statusActive = "active"

// AccountReader is a read-only view onto accounts-service's Postgres table
// (accounts-postgres, port 5434). auth-service never writes to this table —
// account lifecycle (mint/suspend/reactivate) stays accounts-service's
// responsibility; this is a POC-only coupling documented in
// Main-POC-Plan.md Phase 15c as a trade-off a production split would
// replace with an RPC call or a shared auth database.
type AccountReader struct {
	db *sql.DB
}

func NewAccountReader(db *sql.DB) *AccountReader {
	return &AccountReader{db: db}
}

// Get returns the named account, or ErrNotFound.
func (r *AccountReader) Get(ctx context.Context, name string) (account, error) {
	var a account
	err := r.db.QueryRowContext(ctx, `
		SELECT public_key, signing_key_seed, status
		FROM accounts.accounts WHERE name = $1`, name).
		Scan(&a.PublicKey, &a.SigningKeySeed, &a.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return account{}, ErrNotFound
	}
	if err != nil {
		return account{}, err
	}
	return a, nil
}

// reservedTenantNames mirrors accounts-service's reservedAccountNames
// (accounts/handler.go) and shipping-service's nonTenantCredsFiles
// (dictionary/internal/rest/tenant.go) — DEFAULT and SYS are never
// switchable tenants, checked case-insensitively for the same
// defense-in-depth reason those files document.
var reservedTenantNames = map[string]bool{"default": true, "sys": true}

// ListTenants returns every active account name except the reserved ones,
// sorted for a stable dropdown order — this is the browser's replacement
// for shipping-service's discoverTenants() creds-directory scan (Phase 13b/
// 14b), sourced from the same Postgres table accounts-service already
// maintains rather than a second directory scan.
func (r *AccountReader) ListTenants(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name FROM accounts.accounts WHERE status = $1 ORDER BY name`, statusActive)
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
		// Checked case-insensitively — same defense-in-depth as
		// accounts-service's reservedAccountNames and shipping-service's
		// nonTenantCredsFiles (dictionary/internal/rest/tenant.go) against a
		// differently-cased "Default"/"SYS"/etc. ever reaching this table.
		if reservedTenantNames[strings.ToLower(name)] {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
