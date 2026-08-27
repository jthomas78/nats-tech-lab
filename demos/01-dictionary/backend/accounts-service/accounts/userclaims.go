package accounts

// Phase 50b — the read side of the user registry (BR-AC40/BR-AC41).
//
// BR-AC41 exists because a user JWT's own permissions are not necessarily
// the permissions the server enforces. NATS lets an account sign a user with
// a SCOPED signing key, and when it does, the server applies that key's
// template and DISCARDS everything the user's own claims asked for. So a
// claims table that rendered the recorded JWT verbatim would show an
// operator access the user does not have — and, worse, would make a
// credential minted with the wrong permissions indistinguishable from a
// correct one.
//
// Hence the shape returned here: effective permissions first, and the JWT's
// own grants alongside them, present only when they were discarded and
// therefore only ever shown struck through. Showing effective ALONE was
// rejected for the same reason the strike-through exists — the operator
// still needs to see the mistake.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/jwt/v2"
)

// AccountClaimsLookup is the one thing the claims view needs from NATS: the
// issuing account's own claims, which carry the signing keys and their
// scopes. *Provisioner satisfies it via $SYS.REQ.ACCOUNT.<pub>.CLAIMS.LOOKUP;
// taking the interface keeps the resolution logic testable without a server.
type AccountClaimsLookup interface {
	LookupAccountClaims(ctx context.Context, accountPub string) (*jwt.AccountClaims, error)
}

// UserReadStore is the read half of the registry. *Store satisfies it.
type UserReadStore interface {
	ListUsers(ctx context.Context) ([]User, error)
	GetUser(ctx context.Context, publicKey string) (User, error)
}

// UserSummary is one row of the Users panel — metadata only. There is
// deliberately no permissions field, no seed and no credential body here
// (BR-AC40): a list is a roster, and permissions are a drill-in.
type UserSummary struct {
	PublicKey   string     `json:"publicKey"`
	Name        string     `json:"name"`
	Account     string     `json:"account"`
	AccountKey  string     `json:"accountKey,omitempty"`
	Kind        UserKind   `json:"kind"`
	Status      UserStatus `json:"status"`
	Bearer      bool       `json:"bearer"`
	Source      UserSource `json:"source"`
	IssuedAt    time.Time  `json:"issuedAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	ActivatedAt *time.Time `json:"activatedAt,omitempty"`
}

// UserClaimsView is one user's drill-in (BR-AC41).
//
// Effective is what the server actually enforces. JWTGrants is populated
// only when the two differ — i.e. only when a scoped signing key discarded
// the JWT's own permissions — so a client can render it as struck-through
// without deciding for itself whether it applies. Unresolved is set when
// this service could not establish which of the two it is looking at, and it
// is a first-class field rather than an error: the row is still worth
// showing, but labelling unverified grants "effective" is exactly the
// failure BR-AC41 exists to prevent.
type UserClaimsView struct {
	UserSummary
	IssuerKey  string                    `json:"issuerKey,omitempty"`
	Scoped     bool                      `json:"scoped"`
	ScopeRole  string                    `json:"scopeRole,omitempty"`
	Effective  *jwt.UserPermissionLimits `json:"effective,omitempty"`
	JWTGrants  *jwt.UserPermissionLimits `json:"jwtGrants,omitempty"`
	Unresolved string                    `json:"unresolved,omitempty"`
}

// UserClaimsReader answers the two Users-panel reads.
type UserClaimsReader struct {
	store    UserReadStore
	accounts AccountClaimsLookup
	log      *slog.Logger
}

// NewUserClaimsReader wires the registry to the account lookup. accounts may
// be nil — a deployment without a SYS connection can still list users and
// read a row, it just can't resolve a scope, which surfaces per-user as
// Unresolved rather than as a startup failure.
func NewUserClaimsReader(store UserReadStore, accts AccountClaimsLookup, log *slog.Logger) *UserClaimsReader {
	return &UserClaimsReader{store: store, accounts: accts, log: log}
}

func summarize(u User) UserSummary {
	return UserSummary{
		PublicKey:   u.PublicKey,
		Name:        u.Name,
		Account:     u.Account,
		AccountKey:  u.AccountKey,
		Kind:        u.Kind,
		Status:      u.Status,
		Bearer:      u.Bearer,
		Source:      u.Source,
		IssuedAt:    u.IssuedAt,
		ExpiresAt:   u.ExpiresAt,
		ActivatedAt: u.ActivatedAt,
	}
}

// List returns every recorded user, pending rows included (BR-AC38). It is
// cross-tenant by construction: the registry spans every account this
// service has minted into, and one operator call covers all of them.
func (r *UserClaimsReader) List(ctx context.Context) ([]UserSummary, error) {
	users, err := r.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserSummary, 0, len(users))
	for _, u := range users {
		out = append(out, summarize(u))
	}
	return out, nil
}

// Get returns one user's effective permissions (BR-AC41).
func (r *UserClaimsReader) Get(ctx context.Context, publicKey string) (UserClaimsView, error) {
	u, err := r.store.GetUser(ctx, publicKey)
	if err != nil {
		return UserClaimsView{}, err
	}

	view := UserClaimsView{UserSummary: summarize(u), IssuerKey: u.IssuerKey}

	scope, unresolved := r.resolveScope(ctx, u)
	switch {
	case unresolved != "":
		// Not knowing which key signed this user, or not being able to read
		// the account, means not knowing whether the recorded grants are
		// enforced. Say so and show them as the JWT's, unlabelled as
		// effective.
		view.Unresolved = unresolved
		view.Effective = u.Permissions
	case scope != nil:
		view.Scoped = true
		view.ScopeRole = scope.Role
		template := scope.Template
		view.Effective = &template
		// Only meaningful when there is something to strike through.
		view.JWTGrants = u.Permissions
	default:
		view.Effective = u.Permissions
	}

	if view.Effective == nil && view.Unresolved == "" {
		// An empty jwt.UserPermissionLimits marshals as "allowed nothing",
		// which is a much stronger claim than "this service never recorded
		// what was granted" — a row backfilled from an undecodable file, or
		// written before Phase 50b's column existed.
		view.Unresolved = "this service has no record of the permissions this user was granted"
	}
	return view, nil
}

// resolveScope returns the scope the issuing account applies to this user's
// signing key, or nil for an unscoped issuer. A non-empty second return means
// the question could not be answered at all.
func (r *UserClaimsReader) resolveScope(ctx context.Context, u User) (*jwt.UserScope, string) {
	if u.IssuerKey == "" {
		return nil, "the key that signed this user was not recorded, so its scope cannot be resolved"
	}
	if u.AccountKey == "" {
		return nil, "the issuing account was not recorded, so this user's scope cannot be resolved"
	}
	if r.accounts == nil {
		return nil, "no account lookup is configured, so this user's scope cannot be resolved"
	}

	claims, err := r.accounts.LookupAccountClaims(ctx, u.AccountKey)
	if err != nil {
		if r.log != nil {
			r.log.Warn("user claims: account lookup failed", "account", u.AccountKey, "err", err)
		}
		return nil, fmt.Sprintf("the issuing account could not be read from the resolver: %v", err)
	}

	// Signed by the account's own identity key: an identity key carries no
	// scope, so the JWT's permissions are what the server enforces.
	if u.IssuerKey == claims.Subject {
		return nil, ""
	}

	scope, ok := claims.SigningKeys.GetScope(u.IssuerKey)
	if !ok {
		// Neither the identity key nor any signing key the account lists
		// today. The account has been rotated since this user was minted,
		// which means the credential no longer verifies at all — worth
		// saying plainly rather than falling through to "unscoped".
		return nil, fmt.Sprintf("%s is not a signing key on account %s — the account has been rotated since this user was minted", u.IssuerKey, u.AccountKey)
	}
	if scope == nil {
		return nil, "" // a plain signing key: no template, the JWT's own grants stand
	}
	us, ok := scope.(*jwt.UserScope)
	if !ok {
		return nil, fmt.Sprintf("unrecognized scope type %T on the signing key that minted this user", scope)
	}
	return us, ""
}
