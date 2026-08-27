package accounts

// Phase 50a — the user registry (BR-AC38/BR-AC39).
//
// Nothing in a NATS operator-mode stack stores a user. The resolver holds
// ACCOUNT JWTs only; a user JWT is verified by signature at connect time and
// never written down server-side; $SYS.REQ.ACCOUNT.*.CLAIMS.LOOKUP is
// account-scoped, so there is no subject to ask "who are this account's
// users"; and /connz knows only who is connected right now. So the Admin UI's
// Users panel cannot read a list of users from anywhere — the issuer has to
// keep one, which is what this file is.
//
// The registry is a record of what this service minted, not an authority over
// what NATS will accept. Two consequences worth stating plainly:
//
//   - Deleting a row is a registry operation, never a revocation. A signed
//     user JWT that has escaped is revoked only by adding its NKey to the
//     account's revocation list and pushing the account claims update
//     ($SYS.REQ.CLAIMS.UPDATE) — Provisioner.RevokeUser, added in Phase 51b
//     (BR-AC42). That is the mechanism; MarkUserRevoked below only mirrors
//     its outcome onto the row, and DeleteUser still reaches the server not
//     at all.
//   - A row may exist for a credential that was never returned to anyone
//     (see UserStatusPending below). That is the honest state, and the panel
//     shows it rather than hiding it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nats-io/jwt/v2"
)

// ErrUserNotFound is returned by the registry reads when no user has the
// given public NKey.
var ErrUserNotFound = errors.New("user not found")

// ErrUserAlreadyRevoked is returned when a revocation is attempted against a
// row that already carries revoked_at. Revocation is terminal (BR-AC43), so
// this is a no-op reported rather than an idempotent success: a second
// operator pressing the button is asking a question ("is this dead?") that
// deserves a truthful answer, not a silent re-push of a claims update to
// every server in the deployment.
var ErrUserAlreadyRevoked = errors.New("user is already revoked")

// ErrNoUserRegistry is returned by every mint path constructed without a
// registry. BR-AC38 makes recording part of minting rather than a courtesy
// the caller extends afterwards, so a mint path that cannot record is not a
// degraded mint path — it is a misconfiguration, and it fails at
// construction (NewProvisioner) or at the call (the auth package's Mint*
// functions) rather than silently issuing credentials nothing can account
// for.
var ErrNoUserRegistry = errors.New("user registry is required to mint a user")

// UserKind separates the two things that authenticate into a NATS account
// and that an operator has to tell apart at a glance (Phase 50 design
// decision D1).
//
//   - credential — a long-lived .creds file held by a process
//     (shipping-admin, observability, a tenant's own acme.creds). No expiry;
//     it is revoked, rotated or deleted, never outlived.
//   - session — an ephemeral browser credential minted by auth/token.go with
//     a TTL measured in minutes (BR-AC20). Thousands of these are issued over
//     a stack's life; each dies on its own.
type UserKind string

const (
	UserKindCredential UserKind = "credential"
	UserKindSession    UserKind = "session"
)

// UserStatus is the mint outcome as the issuer knows it.
//
// pending is written BEFORE the JWT is signed and flipped to active only
// after (BR-AC38). Writing first is what makes the failure states legible:
// a row stuck at pending is a mint whose outcome this service genuinely does
// not know — the credential may have been signed and returned, or the process
// may have died mid-mint — and an unknown outcome is exactly what an operator
// needs shown rather than a gap in a list. The alternative ordering (sign,
// then record) has no such state: it simply loses the user.
type UserStatus string

const (
	UserStatusPending UserStatus = "pending"
	UserStatusActive  UserStatus = "active"
)

// UserSource records who minted the user, which is what makes the two
// convergence paths distinguishable in the panel.
//
//   - service — minted here, through Provisioner.CreateUser or auth's
//     Mint*Token, and therefore recorded at mint time.
//   - bootstrap — minted outside this service by nats/bootstrap-operator.sh's
//     `nsc add user`, and known only from the .creds file it left on the
//     shared volume (BR-AC39, BackfillCredsDirUsers).
type UserSource string

const (
	UserSourceService   UserSource = "service"
	UserSourceBootstrap UserSource = "bootstrap"
)

// NewUser is a user about to be (or already) minted. PublicKey is the user
// NKey — the identity the server reports as authorized_user in /connz, and
// therefore the join key the Users panel uses to pair a registry row with a
// live connection (Phase 50 design decision D2).
type NewUser struct {
	PublicKey  string
	Name       string // the JWT `name` claim; for a credential this is also its .creds filename
	Account    string // account NAME (lowercase), falling back to the account public key when the name is unknown
	AccountKey string
	// IssuerKey is the key that actually SIGNED the JWT — an account signing
	// key on every path this service mints, or the account identity key for
	// an nsc-minted user signed without one. Recorded separately from
	// AccountKey because it is the lookup key for the scope that decides
	// whether the JWT's own permissions are enforced at all (BR-AC41): a
	// scoped signing key's template wins and the JWT's grants are discarded
	// by the server, and nothing else on the row can tell you which key was
	// used.
	IssuerKey string
	// Permissions are the user claims' own permissions and limits, recorded
	// at mint time. The registry stores the CLAIMS, never the signed token:
	// a bearer JWT is a credential on its own, and a non-bearer one is still
	// half of one. This is what BR-AC41 returns as the JWT's grants — which
	// are authoritative only when IssuerKey is unscoped.
	Permissions *jwt.UserPermissionLimits
	Kind        UserKind
	Bearer      bool
	// ExpiresAt is nil for a credential (no expiry) and now+TTL for a
	// session.
	ExpiresAt *time.Time
	Source    UserSource
	// Status is honored by InsertUserIfMissing only; RecordPendingUser always
	// writes pending, since that is the whole point of it.
	Status UserStatus
}

// User is a registry row as read back.
type User struct {
	ID          string
	PublicKey   string
	Name        string
	Account     string
	AccountKey  string
	IssuerKey   string
	Permissions *jwt.UserPermissionLimits
	Kind        UserKind
	Status      UserStatus
	Bearer      bool
	Source      UserSource
	IssuedAt    time.Time
	ExpiresAt   *time.Time
	ActivatedAt *time.Time
	// RevokedAt mirrors the issuing account's JWT revocation list (BR-AC42).
	// Nil means this service has not revoked the credential; a value means
	// it pushed the revocation and the server refused the credential from
	// that instant on. It is a MIRROR of a server-side fact and never the
	// fact itself — the authority is the account JWT.
	RevokedAt *time.Time
}

// UserRegistry is the write side of the registry as the mint paths need it —
// the two calls that bracket signing. Both the accounts package's
// Provisioner and the auth package's Mint*Token take this rather than *Store
// so the ordering BR-AC38 requires can be asserted without a database.
type UserRegistry interface {
	RecordPendingUser(ctx context.Context, u NewUser) error
	MarkUserActive(ctx context.Context, publicKey string) error
}

// BootstrapUserSink is the narrower write side BackfillCredsDirUsers needs.
type BootstrapUserSink interface {
	InsertUserIfMissing(ctx context.Context, u NewUser) error
}

// userColumns is the select list every user read shares, so a new column
// can't be added to one query and forgotten in another.
const userColumns = `id, public_key, name, account, account_key, issuer_key, permissions, kind, status, bearer, source, issued_at, expires_at, activated_at, revoked_at`

// scanUser binds a row into dest. permissions comes back as raw JSONB and is
// decoded by finishUser, since database/sql has no hook for it.
func scanUser(dest *User, perms *[]byte) []any {
	return []any{&dest.ID, &dest.PublicKey, &dest.Name, &dest.Account, &dest.AccountKey, &dest.IssuerKey, perms,
		&dest.Kind, &dest.Status, &dest.Bearer, &dest.Source, &dest.IssuedAt, &dest.ExpiresAt, &dest.ActivatedAt, &dest.RevokedAt}
}

// finishUser decodes the permissions JSONB scanned alongside dest. A row
// written before Phase 50b — or by a mint path that had no claims to record —
// has NULL here, and that is a legitimate state: it means "this service does
// not know what this credential was granted", which the claims view reports
// rather than papering over with an empty permission set.
func finishUser(dest *User, perms []byte) error {
	if len(perms) == 0 {
		return nil
	}
	var out jwt.UserPermissionLimits
	if err := json.Unmarshal(perms, &out); err != nil {
		return err
	}
	dest.Permissions = &out
	return nil
}

// marshalPermissions renders the claims for the JSONB column; nil stays SQL
// NULL rather than becoming the JSON literal "null", so finishUser's
// len()==0 check reads it back as "unknown".
func marshalPermissions(p *jwt.UserPermissionLimits) (any, error) {
	if p == nil {
		return nil, nil
	}
	return json.Marshal(p)
}

// RecordPendingUser writes the pre-signature row (BR-AC38). The public NKey
// is already known at this point — a keypair can be generated before anything
// is signed, and an NKey on its own grants nothing — which is what makes
// recording first possible at all.
func (s *Store) RecordPendingUser(ctx context.Context, u NewUser) error {
	perms, err := marshalPermissions(u.Permissions)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO accounts.users (public_key, name, account, account_key, issuer_key, permissions, kind, status, bearer, source, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		u.PublicKey, u.Name, u.Account, u.AccountKey, u.IssuerKey, perms, string(u.Kind), string(UserStatusPending),
		u.Bearer, string(sourceOrDefault(u.Source)), u.ExpiresAt)
	return err
}

// MarkUserActive flips a recorded user to active once its JWT is signed.
// Returns ErrUserNotFound if nothing was recorded for that key.
func (s *Store) MarkUserActive(ctx context.Context, publicKey string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts.users SET status = $2, activated_at = now() WHERE public_key = $1`,
		publicKey, string(UserStatusActive))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// InsertUserIfMissing writes a user this service did not mint, ignoring a
// row that is already there — the idempotent shape BR-AC39's start-up
// convergence needs (the same contract SeedIfMissing has for accounts). It
// deliberately never updates: a re-run must not rewrite what an earlier run,
// or a live mint, already recorded.
func (s *Store) InsertUserIfMissing(ctx context.Context, u NewUser) error {
	status := u.Status
	if status == "" {
		status = UserStatusActive
	}
	perms, err := marshalPermissions(u.Permissions)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO accounts.users (public_key, name, account, account_key, issuer_key, permissions, kind, status, bearer, source, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (public_key) DO NOTHING`,
		u.PublicKey, u.Name, u.Account, u.AccountKey, u.IssuerKey, perms, string(u.Kind), string(status),
		u.Bearer, string(sourceOrDefault(u.Source)), u.ExpiresAt)
	return err
}

func sourceOrDefault(src UserSource) UserSource {
	if src == "" {
		return UserSourceService
	}
	return src
}

// ListUsers returns every recorded user — pending rows included, since an
// unknown mint outcome is precisely what an operator needs to see (BR-AC38).
// Ordered account-then-name for a stable listing, the same shape List uses
// for accounts.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+userColumns+` FROM accounts.users ORDER BY account, name, issued_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		var perms []byte
		if err := rows.Scan(scanUser(&u, &perms)...); err != nil {
			return nil, err
		}
		if err := finishUser(&u, perms); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetUser returns one user by public NKey, or ErrUserNotFound.
func (s *Store) GetUser(ctx context.Context, publicKey string) (User, error) {
	var u User
	var perms []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT `+userColumns+` FROM accounts.users WHERE public_key = $1`, publicKey).
		Scan(scanUser(&u, &perms)...)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	if err := finishUser(&u, perms); err != nil {
		return User{}, err
	}
	return u, nil
}

// MarkUserRevoked stamps revoked_at on a registry row, mirroring a
// revocation already pushed to the issuing account's JWT (BR-AC42).
//
// It is called AFTER the claims update lands, never before: if the push
// succeeds and this write fails, the credential is genuinely dead and the
// registry merely does not say so yet — recoverable, and visible as drift.
// The other ordering fails the dangerous way round, marking a row revoked
// while the credential keeps working.
//
// The WHERE clause refuses a second revocation rather than moving the
// timestamp: revocation is terminal (BR-AC43), and the instant that matters
// is the FIRST one — it is what the account JWT's own list records, and
// overwriting it here would manufacture drift against the authority this
// column exists to mirror. Returns ErrUserAlreadyRevoked when the row is
// already stamped, and ErrUserNotFound when there is no such row.
func (s *Store) MarkUserRevoked(ctx context.Context, publicKey string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts.users SET revoked_at = now() WHERE public_key = $1 AND revoked_at IS NULL`, publicKey)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Nothing updated is two different facts; tell them apart rather
		// than reporting the more alarming one for both.
		if _, err := s.GetUser(ctx, publicKey); err != nil {
			return err
		}
		return ErrUserAlreadyRevoked
	}
	return nil
}

// DeleteUser removes a registry row. Not a revocation — see this file's
// header, and BR-AC42 for what a revocation actually is. Returns
// ErrUserNotFound if there was nothing to remove.
func (s *Store) DeleteUser(ctx context.Context, publicKey string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM accounts.users WHERE public_key = $1`, publicKey)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ReapExpiredSessions removes expired session rows and returns how many it
// removed (BR-AC44). Phase 52; it subsumes the start-up-only
// SweepExpiredPendingUsers that preceded it.
//
// The predicate is a WHITELIST — every clause below narrows what may be
// deleted, so the failure mode of a future edit is a row that survives
// rather than a credential that does not:
//
//   - kind = 'session'. A credential has no expiry and so cannot match
//     today; the clause is belt-and-braces, and it means a bug that stamps
//     expires_at onto a credential costs a wrong badge in the panel, not a
//     deleted credential.
//   - expires_at IS NOT NULL AND already in the past. The window is measured
//     from the row's own expiry, never from now alone, so a retention of 0
//     still cannot touch a live session.
//   - revoked_at IS NULL. A revoked row is never reaped, at ANY age. The row
//     is the only human-readable mirror of an entry in the account JWT's
//     revocation list (BR-AC42/BR-AC43); that list keeps the NKey forever,
//     and deleting the row leaves an operator staring at a revocation whose
//     subject nothing in the stack can name. Revocations are rare and
//     operator-driven, so exempting them does not reintroduce unbounded
//     growth.
//
// A pending row is reaped the moment its TTL passes, with no retention
// grace, which is the behaviour BR-AC38 already had. The window exists to
// keep an EXPLAINABLE row around long enough to be read; a mint that never
// produced a credential has nothing to explain.
func (s *Store) ReapExpiredSessions(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM accounts.users
		WHERE kind = $1
		  AND revoked_at IS NULL
		  AND expires_at IS NOT NULL
		  AND expires_at < now()
		  AND (status = $2 OR expires_at < now() - $3::interval)`,
		string(UserKindSession), string(UserStatusPending), retention.String())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// normalizeAccountName keeps the registry's account column on the same
// lowercase spelling accounts.accounts uses (cmd/main.go's RenameIfExists
// migrated PLATFORM/ACME/GLOBEX onto it), so a join by name lines up.
func normalizeAccountName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
