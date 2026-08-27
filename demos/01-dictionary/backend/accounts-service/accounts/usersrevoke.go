package accounts

// Phase 51b — credential revocation (BR-AC42/BR-AC43).
//
// Phase 50 made NATS users enumerable but not actionable. Store.DeleteUser
// existed, had no caller outside tests, and its own header said it is "not a
// revocation" — so the only real answer to "this credential must stop
// working" was to edit the account by hand with nsc. This file is that
// missing capability.
//
// The mechanism lives in Provisioner.RevokeUser; what lives here is the
// policy around it, which is the part with rules attached:
//
//   - Credentials only. Revoking a 15-minute browser session is pointless —
//     it dies on its own TTL faster than an operator can find the row.
//   - Terminal. There is no un-revoke. jwt.RevocationList.ClearRevocation
//     exists and would reuse this exact path, and it is deliberately not
//     exposed: recovery from a mis-revocation is minting a REPLACEMENT
//     credential. The known cost is accepted knowingly — revoking a service
//     credential by mistake takes that service down until a new one is
//     minted and mounted — and the whole-tenant lifecycle keeps its own
//     reversible path (suspend/reactivate), which this does not replace.
//   - No actor. There is no operator identity in this POC; the Admin UI
//     connects as the shared admin-app credential. A revoked_by column would
//     record the same worthless string every time and imply an audit trail
//     that does not exist.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// ErrRevokeNotCredential rejects a revocation aimed at anything other than a
// long-lived credential (BR-AC43).
var ErrRevokeNotCredential = errors.New("only a credential can be revoked; a session expires on its own TTL")

// ErrRevokeAccountUnknown is returned for a row with no recorded account
// key. Revocation is an amendment to a specific account's JWT, so without
// one there is no JWT to amend — and guessing would either be a no-op or,
// worse, write a revocation into the wrong account.
var ErrRevokeAccountUnknown = errors.New("this user's issuing account was not recorded, so its credential cannot be revoked")

// NATSUserRevoker is the one thing revocation needs from NATS.
// *Provisioner satisfies it; taking the interface keeps the policy above
// testable without an operator key or a live server.
type NATSUserRevoker interface {
	RevokeUser(ctx context.Context, accountPub, userPub string) error
}

// UserRevokeStore is the registry half: read the row, then mirror the
// outcome onto it. *Store satisfies it.
type UserRevokeStore interface {
	GetUser(ctx context.Context, publicKey string) (User, error)
	MarkUserRevoked(ctx context.Context, publicKey string) error
}

// UserRevoker serves api._platform.accounts.user.revoke.v1.
type UserRevoker struct {
	store UserRevokeStore
	nats  NATSUserRevoker
	log   *slog.Logger
}

// NewUserRevoker wires the registry to the NATS revocation path. Unlike
// NewUserClaimsReader's optional account lookup, nats is REQUIRED: a
// revocation that cannot reach the server is not a degraded revocation, it
// is a lie told to an operator who is about to stop worrying about a
// credential. Revoke refuses rather than half-succeeding.
func NewUserRevoker(store UserRevokeStore, revoker NATSUserRevoker, log *slog.Logger) *UserRevoker {
	return &UserRevoker{store: store, nats: revoker, log: log}
}

// RevokeResult is what the caller gets back: enough to redraw the row
// without a second read, and nothing that is not already on it.
type RevokeResult struct {
	PublicKey string `json:"publicKey"`
	Name      string `json:"name"`
	Account   string `json:"account"`
	// RevokedAt is the registry's mirror of the account JWT's own entry.
	RevokedAt string `json:"revokedAt,omitempty"`
}

// Revoke pushes the revocation and then mirrors it onto the registry row.
//
// The ORDER is the whole design. The claims update goes first, and the row
// is stamped only once the server has accepted it. The other ordering fails
// the dangerous way round: a row marked revoked while the credential still
// works tells an operator a credential is dead when it is live, which is
// precisely the failure this capability exists to prevent. This way round,
// the worst case is a live credential correctly refused by the server and a
// registry that has not caught up — visible as drift, and recoverable.
func (r *UserRevoker) Revoke(ctx context.Context, publicKey string) (RevokeResult, error) {
	if publicKey == "" {
		return RevokeResult{}, ErrPublicKeyRequired
	}
	if r.nats == nil {
		return RevokeResult{}, errors.New("no NATS revocation path is configured, so this credential cannot be revoked")
	}

	u, err := r.store.GetUser(ctx, publicKey)
	if err != nil {
		return RevokeResult{}, err
	}
	if u.Kind != UserKindCredential {
		return RevokeResult{}, fmt.Errorf("%w (this user is a %s)", ErrRevokeNotCredential, u.Kind)
	}
	// Checked before the push, so a second press does not re-sign and
	// re-push an account JWT to every server in the deployment for a
	// credential that is already dead.
	if u.RevokedAt != nil {
		return RevokeResult{}, ErrUserAlreadyRevoked
	}
	if u.AccountKey == "" {
		return RevokeResult{}, ErrRevokeAccountUnknown
	}

	if err := r.nats.RevokeUser(ctx, u.AccountKey, u.PublicKey); err != nil {
		return RevokeResult{}, err
	}

	if err := r.store.MarkUserRevoked(ctx, publicKey); err != nil {
		// The credential IS revoked at this point — the server accepted the
		// claims update. Report the failure so nothing pretends otherwise,
		// but say plainly which half succeeded, because the remedy is to
		// reconcile the row and not to revoke again.
		if r.log != nil {
			r.log.Error("user revoked on the server but the registry row was not stamped",
				"user", publicKey, "account", u.AccountKey, "err", err)
		}
		return RevokeResult{}, fmt.Errorf("credential revoked on the server, but the registry row was not updated: %w", err)
	}

	after, err := r.store.GetUser(ctx, publicKey)
	if err != nil {
		// Both writes landed; only the read-back failed. The revocation
		// stands, so report it as done rather than as an error.
		return RevokeResult{PublicKey: u.PublicKey, Name: u.Name, Account: u.Account}, nil
	}
	out := RevokeResult{PublicKey: after.PublicKey, Name: after.Name, Account: after.Account}
	if after.RevokedAt != nil {
		out.RevokedAt = after.RevokedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return out, nil
}
