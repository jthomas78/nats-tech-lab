package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// requestTimeout bounds every $SYS.REQ.CLAIMS.* round trip — the resolver
// write is local-disk fast; this only guards against an unreachable or
// misconfigured SYS connection.
const requestTimeout = 5 * time.Second

// JSLimits mirrors nats/bootstrap-operator.sh's per-account JetStream
// limits (Phase 14a) — the same four knobs, now settable per new tenant
// instead of hardcoded in a shell script.
type JSLimits struct {
	MaxMem       int64
	MaxFile      int64
	MaxStreams   int64
	MaxConsumers int64
}

// Provisioner mints and revokes NATS accounts at runtime via decentralized
// JWTs, replacing nats/bootstrap-operator.sh's one-shot nsc invocation with
// a live $SYS.REQ.CLAIMS.UPDATE/DELETE round trip (Phase 14b). Holds the
// operator's signing key (never the root operator key — see
// nats/bootstrap-operator.sh's header) and a connection authenticated as
// the SYS account, since $SYS.REQ.CLAIMS.* is only reachable that way.
type Provisioner struct {
	operatorSigningKey nkeys.KeyPair
	sysNC              *nats.Conn
}

// CrossAccountOpts describes the PLATFORM exports a tenant account imports.
type CrossAccountOpts struct {
	PlatformPublicKey string
	// TenantName is the human-readable account name (e.g. "acme") stamped into
	// rpc.{tenantName}.refdata.* remote subjects — readable in logs and traces
	// while still operator-enforced (the import lives in a signed JWT).
	TenantName string
}

// NewProvisioner loads the operator signing key seed (as exported by
// nats/bootstrap-operator.sh to nats/keys/operator-signing-key.nk) and pairs
// it with a NATS connection already authenticated as the SYS account.
func NewProvisioner(operatorSigningKeySeed []byte, sysNC *nats.Conn) (*Provisioner, error) {
	kp, err := nkeys.FromSeed(operatorSigningKeySeed)
	if err != nil {
		return nil, fmt.Errorf("load operator signing key: %w", err)
	}
	if _, err := kp.Seed(); err != nil {
		return nil, fmt.Errorf("operator signing key file does not contain a private seed: %w", err)
	}
	return &Provisioner{operatorSigningKey: kp, sysNC: sysNC}, nil
}

// MintedAccount is a freshly created account: its public key (for
// Store.Insert) and its own signing key seed (needed later to mint users
// for it, or nothing further if the account is only ever addressed by this
// service).
type MintedAccount struct {
	PublicKey      string
	SigningKeySeed string
}

// CreateAccount mints a new account JWT signed by the operator's signing
// key and pushes it to every server's resolver via $SYS.REQ.CLAIMS.UPDATE —
// no nats.conf edit, no server restart (the mechanism nats.conf's resolver
// doc comment describes). The account gets its own signing key so it, in
// turn, can sign user JWTs (CreateUser below) without exposing this
// service's operator-level key to per-tenant credential minting.
func (p *Provisioner) CreateAccount(ctx context.Context, limits JSLimits, tenantName, platformPublicKey string) (MintedAccount, error) {
	accountKP, err := nkeys.CreateAccount()
	if err != nil {
		return MintedAccount{}, fmt.Errorf("generate account key: %w", err)
	}
	accountPub, err := accountKP.PublicKey()
	if err != nil {
		return MintedAccount{}, fmt.Errorf("account public key: %w", err)
	}

	signingKP, err := nkeys.CreateAccount()
	if err != nil {
		return MintedAccount{}, fmt.Errorf("generate account signing key: %w", err)
	}
	signingPub, err := signingKP.PublicKey()
	if err != nil {
		return MintedAccount{}, fmt.Errorf("account signing key public key: %w", err)
	}
	signingSeed, err := signingKP.Seed()
	if err != nil {
		return MintedAccount{}, fmt.Errorf("account signing key seed: %w", err)
	}

	claims := newAccountClaims(accountPub, signingPub, limits, nil, CrossAccountOpts{PlatformPublicKey: platformPublicKey, TenantName: tenantName})
	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return MintedAccount{}, fmt.Errorf("encode account jwt: %w", err)
	}

	if err := p.pushClaimsUpdate(ctx, token); err != nil {
		return MintedAccount{}, err
	}

	return MintedAccount{PublicKey: accountPub, SigningKeySeed: string(signingSeed)}, nil
}

// newAccountClaims builds the account claims shape shared by CreateAccount
// and ReactivateAccount: same subject (accountPub), same JetStream limits
// encoding, and same NatsLimits/AccountLimits defaults. Existing exports and
// imports are copied into every re-push: account JWT updates replace the
// whole claim, so omitting them would silently sever cross-account access.
// signingPub may be empty (seeded pre-existing accounts have no stored
// signing key — see Account.SigningKeySeed's doc comment in store.go), in
// which case the claims simply carry no signing key.
//
// Import recovery: if prior is non-nil but carries no imports (e.g. a stale
// resolver JWT predating the Phase 21 export/import declarations), and
// crossAccount supplies the PLATFORM public key, fall back to tenantImports
// rather than propagating the empty slice. This prevents a stale volume JWT
// from permanently cementing missing imports across any subsequent re-push.
func newAccountClaims(accountPub, signingPub string, limits JSLimits, prior *jwt.AccountClaims, crossAccount CrossAccountOpts) *jwt.AccountClaims {
	claims := jwt.NewAccountClaims(accountPub)
	claims.Name = accountPub
	if signingPub != "" {
		claims.SigningKeys.Add(signingPub)
	}
	claims.Limits.JetStreamLimits = jwt.JetStreamLimits{
		MemoryStorage: limits.MaxMem,
		DiskStorage:   limits.MaxFile,
		Streams:       limits.MaxStreams,
		Consumer:      limits.MaxConsumers,
	}
	switch {
	case prior != nil && len(prior.Imports) > 0:
		// Happy path: preserve existing exports/imports verbatim.
		claims.Exports = append(jwt.Exports(nil), prior.Exports...)
		claims.Imports = append(jwt.Imports(nil), prior.Imports...)
	case crossAccount.PlatformPublicKey != "":
		// Recovery path: prior has no imports (stale pre-Phase-21 JWT or
		// first-time mint) but we have the PLATFORM key — rebuild imports.
		if prior != nil {
			claims.Exports = append(jwt.Exports(nil), prior.Exports...)
		}
		claims.Imports = tenantImports(crossAccount.TenantName, crossAccount.PlatformPublicKey)
	case prior != nil:
		// prior exists but no PLATFORM key — copy whatever is there; caller
		// is operating without cross-account context (e.g. PLATFORM itself).
		claims.Exports = append(jwt.Exports(nil), prior.Exports...)
		claims.Imports = append(jwt.Imports(nil), prior.Imports...)
	}
	return claims
}

// tenantImports is the complete PLATFORM-to-tenant contract. The four
// remapped services make the caller publish only a context-free local subject;
// the import's remote subject uses the human-readable tenant name so subjects
// are readable in logs and the admin UI. Security is preserved: the import
// lives in an operator-signed JWT so a tenant cannot remap to another
// tenant's subject without the operator's private key.
func tenantImports(tenantName, platformPublicKey string) jwt.Imports {
	service := func(remote, local string) *jwt.Import {
		return &jwt.Import{Account: platformPublicKey, Subject: jwt.Subject(remote), LocalSubject: jwt.RenamingSubject(local), Type: jwt.Service}
	}
	stream := func(subject string) *jwt.Import {
		return &jwt.Import{Account: platformPublicKey, Subject: jwt.Subject(subject), Type: jwt.Stream}
	}
	return jwt.Imports{
		service(fmt.Sprintf("rpc.%s.refdata.item.get.v1", tenantName), "refdata.item.get.v1"),
		service(fmt.Sprintf("rpc.%s.refdata.type.list.v1", tenantName), "refdata.type.list.v1"),
		service(fmt.Sprintf("rpc.%s.refdata.item.get-versioned.v1", tenantName), "refdata.item.get-versioned.v1"),
		service(fmt.Sprintf("rpc.%s.refdata.locales.list.v1", tenantName), "refdata.locales.list.v1"),
		service("rpc._platform.refdata.context.list.v1", "rpc._platform.refdata.context.list.v1"),
		stream("notify.accounts.account.*"),
		stream("evt.*.refdata.*.changed"),
	}
}

// ReactivateAccount restores a previously-suspended account under its
// original public key: it re-mints and re-pushes the account JWT (the
// counterpart to DeleteAccount's revocation) using the account's own stored
// signing key seed and JetStream limits, so the account resolves again with
// the exact identity and limits it had before suspension. It does not mint
// a new user — callers that need a fresh usable .creds file call CreateUser
// afterward with the same accountPub/signingKeySeed pair.
func (p *Provisioner) ReactivateAccount(ctx context.Context, accountPub, signingKeySeed string, limits JSLimits, crossAccount CrossAccountOpts, prior *jwt.AccountClaims) error {
	var signingPub string
	if signingKeySeed != "" {
		signingKP, err := nkeys.FromSeed([]byte(signingKeySeed))
		if err != nil {
			return fmt.Errorf("load account signing key: %w", err)
		}
		signingPub, err = signingKP.PublicKey()
		if err != nil {
			return fmt.Errorf("account signing key public key: %w", err)
		}
	}

	if current, err := p.lookupAccountClaims(ctx, accountPub); err == nil {
		prior = current
	}
	claims := newAccountClaims(accountPub, signingPub, limits, prior, crossAccount)
	// Account JWT signing is deterministic (Ed25519, no nonce): claims
	// rebuilt from identical inputs (same pubkey/signing key/limits) encode
	// to byte-identical JWTs. Without this tag, a reactivation whose claims
	// exactly match the original would produce the exact same JWT the
	// account already had before DeleteAccount revoked it — the server's
	// resolver treats a byte-identical update as a no-op ("same claims
	// detected") and never re-runs the account-refresh logic that clears
	// the in-memory expired flag DeleteAccount set, leaving the account
	// stuck rejecting connections even though the resolver's on-disk JWT is
	// technically valid again. The tag guarantees the encoded JWT always
	// differs from whatever the account had before.
	claims.Tags.Add(fmt.Sprintf("reactivated-%d", time.Now().UnixNano()))

	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return fmt.Errorf("encode account jwt: %w", err)
	}

	return p.pushClaimsUpdate(ctx, token)
}

// UpdateAccountLimits re-mints and re-pushes the account JWT with new
// JetStream limits, leaving everything else (public key, signing key,
// identity) unchanged. Status-agnostic — the handler decides whether to
// gate on active/suspended.
func (p *Provisioner) UpdateAccountLimits(ctx context.Context, accountPub, signingKeySeed string, limits JSLimits, crossAccount CrossAccountOpts) error {
	var signingPub string
	if signingKeySeed != "" {
		signingKP, err := nkeys.FromSeed([]byte(signingKeySeed))
		if err != nil {
			return fmt.Errorf("load account signing key: %w", err)
		}
		signingPub, err = signingKP.PublicKey()
		if err != nil {
			return fmt.Errorf("account signing key public key: %w", err)
		}
	}

	prior, err := p.lookupAccountClaims(ctx, accountPub)
	if err != nil {
		return err
	}
	claims := newAccountClaims(accountPub, signingPub, limits, prior, crossAccount)
	claims.Tags.Add(fmt.Sprintf("jslimits-%d", time.Now().UnixNano()))

	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return fmt.Errorf("encode account jwt: %w", err)
	}

	return p.pushClaimsUpdate(ctx, token)
}

// lookupAccountClaims reads the resolver's current complete account JWT.
// Limit updates happen while an account is active, so treating an absent or
// malformed current JWT as an error is safer than overwriting exports/imports.
// Reactivation deliberately falls back to CrossAccountOpts because a revoked
// account has no resolver JWT to look up.
func (p *Provisioner) lookupAccountClaims(_ context.Context, accountPub string) (*jwt.AccountClaims, error) {
	resp, err := p.sysNC.Request(fmt.Sprintf("$SYS.REQ.ACCOUNT.%s.CLAIMS.LOOKUP", accountPub), nil, requestTimeout)
	if err != nil {
		return nil, fmt.Errorf("lookup current account claims: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("lookup current account claims: account %s has no resolver JWT", accountPub)
	}
	claims, err := jwt.DecodeAccountClaims(string(resp.Data))
	if err != nil {
		return nil, fmt.Errorf("decode current account claims: %w", err)
	}
	return claims, nil
}

// pushClaimsUpdate requests $SYS.REQ.CLAIMS.UPDATE with the raw account JWT
// as payload — the exact mechanism nats.conf's resolver comment names, and
// the only thing the resolver's Fetch/Store methods there won't do for us:
// it serves JWTs it's given, it doesn't mint them.
func (p *Provisioner) pushClaimsUpdate(ctx context.Context, accountJWT string) error {
	_ = ctx // request timeout below is independent of ctx cancellation; nats.go's Request has no context-aware variant
	resp, err := p.sysNC.Request("$SYS.REQ.CLAIMS.UPDATE", []byte(accountJWT), requestTimeout)
	if err != nil {
		return fmt.Errorf("$SYS.REQ.CLAIMS.UPDATE request: %w", err)
	}
	var parsed server.ServerAPIClaimUpdateResponse
	if err := json.Unmarshal(resp.Data, &parsed); err != nil {
		return fmt.Errorf("decode claims update response: %w", err)
	}
	if parsed.Error != nil {
		return fmt.Errorf("claims update rejected: %s", parsed.Error.Description)
	}
	return nil
}

// CreateUser mints a user JWT for accountPub, signed by that account's
// signing key seed (never the operator key), and returns a ready-to-use
// .creds file — the same format nats/bootstrap-operator.sh produces via
// `nsc generate creds`, just generated live instead of ahead of time.
func (p *Provisioner) CreateUser(accountPub, accountSigningKeySeed, userName string) ([]byte, error) {
	signingKP, err := nkeys.FromSeed([]byte(accountSigningKeySeed))
	if err != nil {
		return nil, fmt.Errorf("load account signing key: %w", err)
	}

	userKP, err := nkeys.CreateUser()
	if err != nil {
		return nil, fmt.Errorf("generate user key: %w", err)
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("user public key: %w", err)
	}
	userSeed, err := userKP.Seed()
	if err != nil {
		return nil, fmt.Errorf("user seed: %w", err)
	}

	claims := jwt.NewUserClaims(userPub)
	claims.Name = userName
	claims.IssuerAccount = accountPub // this JWT is signed by a signing key, not the account's own identity key

	token, err := claims.Encode(signingKP)
	if err != nil {
		return nil, fmt.Errorf("encode user jwt: %w", err)
	}

	return jwt.FormatUserConfig(token, userSeed)
}

// DeleteAccount revokes accountPub via $SYS.REQ.CLAIMS.DELETE — a
// self-signed request from the operator (or, as here, its signing key)
// naming the account to remove from every server's resolver. The resolver's
// own state removes the account's JWT (see nats.conf's
// resolver.allow_delete: true, required for this to work), and no *new*
// connection can authenticate against it afterwards.
//
// Existing connections are force-evicted too — verified against NATS 2.11
// on the running stack (2026-08-03): every connection on the account drops
// within a couple of seconds and the server reports
// "account authentication expired" to each. An earlier version of this
// comment claimed the opposite; it was wrong, and the claim had already
// propagated into ARCHITECTURE-ACCOUNTS.md § 2t before being corrected
// there too.
//
// Callers should note that eviction alone is not a clean teardown for
// anything holding a per-tenant connection: shipping-service currently
// treats the resulting error as transient and reconnect-loops forever
// against the .creds file suspendAccount has already deleted. See
// ARCHITECTURE-ACCOUNTS.md § 2t-a for the full runtime consequence and the
// proposed notify.accounts.account.suspended fix.
func (p *Provisioner) DeleteAccount(ctx context.Context, accountPub string) error {
	signingPub, err := p.operatorSigningKey.PublicKey()
	if err != nil {
		return fmt.Errorf("operator signing key public key: %w", err)
	}
	claims := jwt.NewGenericClaims(signingPub) // self-signed: subject == issuer, required by the server's delete handler
	claims.Data["accounts"] = []string{accountPub}

	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return fmt.Errorf("encode delete request: %w", err)
	}

	_ = ctx // see pushClaimsUpdate's note on nats.go's Request not being context-aware
	resp, err := p.sysNC.Request("$SYS.REQ.CLAIMS.DELETE", []byte(token), requestTimeout)
	if err != nil {
		return fmt.Errorf("$SYS.REQ.CLAIMS.DELETE request: %w", err)
	}
	var parsed server.ServerAPIClaimUpdateResponse
	if err := json.Unmarshal(resp.Data, &parsed); err != nil {
		return fmt.Errorf("decode claims delete response: %w", err)
	}
	if parsed.Error != nil {
		return fmt.Errorf("claims delete rejected: %s", parsed.Error.Description)
	}
	return nil
}
