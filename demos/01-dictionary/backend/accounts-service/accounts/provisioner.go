package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

	// Phase 28f: PLATFORM's own account claims are a separate JWT that no
	// tenant-side push can touch — the new tenant's obs.trace.> export
	// (just wired into claims.Exports above via newAccountClaims, gated on
	// the same platformPublicKey != "" condition) is inert until
	// PLATFORM's claims are re-signed with a matching Stream import naming
	// this account. See addPlatformTraceImport's doc comment. Skipped
	// entirely when platformPublicKey is empty — the caller has opted out
	// of cross-account wiring altogether (e.g. low-level provisioner tests
	// that mint bare, unconnected accounts).
	if platformPublicKey != "" {
		if err := p.addPlatformTraceImport(ctx, platformPublicKey, accountPub, tenantName); err != nil {
			return MintedAccount{}, fmt.Errorf("register platform trace import for new tenant: %w", err)
		}
		// BR-AC34 (Phase 43a): same re-sign, same gate, for the new
		// tenant's obs.pubsub.> export.
		if err := p.addPlatformPubsubImport(ctx, platformPublicKey, accountPub, tenantName); err != nil {
			return MintedAccount{}, fmt.Errorf("register platform pubsub import for new tenant: %w", err)
		}
		// BR-AC31 (Phase 30a): same re-sign, same gate, for the new
		// tenant's $SRV.> service-discovery export.
		if err := p.addPlatformMonitorImport(ctx, platformPublicKey, accountPub, tenantName); err != nil {
			return MintedAccount{}, fmt.Errorf("register platform monitor import for new tenant: %w", err)
		}
		// BR-AC32 (Phase 30b): same re-sign, same gate, for the new
		// tenant's six $JS.API introspection exports.
		if err := p.addPlatformJSAPIImport(ctx, platformPublicKey, accountPub, tenantName); err != nil {
			return MintedAccount{}, fmt.Errorf("register platform js api import for new tenant: %w", err)
		}
	}

	return MintedAccount{PublicKey: accountPub, SigningKeySeed: string(signingSeed)}, nil
}

// newAccountClaims builds the account claims shape shared by CreateAccount
// and ReactivateAccount: same subject (accountPub), same JetStream limits
// encoding, and same NatsLimits/AccountLimits defaults. Existing exports,
// imports, and signing keys are copied into every re-push: account JWT
// updates replace the whole claim, so omitting them would silently sever
// cross-account access (BR-AC14) or invalidate every credential already
// signed by a prior signing key (BR-AC19).
// signingPub may be empty (seeded pre-existing accounts have no stored
// signing key — see Account.SigningKeySeed's doc comment in store.go), in
// which case the claims simply carry no signing key of their own.
//
// Signing keys accumulate rather than rotate: re-signing a claim is not a
// revocation operation, and a caller that re-signs for an unrelated reason
// (establishing a browser signing key at startup, changing JetStream limits)
// must not silently invalidate credentials that were valid a moment earlier.
// Revoking an account's credentials is BR-AC03's suspend
// ($SYS.REQ.CLAIMS.DELETE), which removes the account JWT outright.
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
	if prior != nil {
		for key, scope := range prior.SigningKeys {
			if scope != nil {
				claims.SigningKeys.AddScopedSigner(scope)
				continue
			}
			claims.SigningKeys.Add(key)
		}
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
		claims.Exports.Add(tenantExports()...)
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

// traceExportSubject is the one subject a tenant exports back to PLATFORM
// (Phase 28f) — the reverse leg of tenantImports's PLATFORM-to-tenant
// contract, needed so PLATFORM's cross-account trace store
// (dictionary/composition.go's TRACES consumer) can subscribe to this
// tenant's own obs.trace.* spans. Declared once so tenantExports and
// addPlatformTraceImport's idempotency check can't drift apart.
const traceExportSubject = "obs.trace.>"

// pubsubExportSubject is the third tenant-to-PLATFORM export (BR-AC34,
// Phase 43a) — the reverse leg needed so the Admin UI's Messages panel can
// see every tenant's evt.*/notify.* publish traffic as obs.pubsub.* spans.
// A Stream export, the same shape as traceExportSubject above; declared once
// so tenantExports and addPlatformPubsubImport's idempotency check can't
// drift apart.
const pubsubExportSubject = "obs.pubsub.>"

// pubsubLocalSubjectTmpl is PLATFORM's per-tenant remap for that import
// (BR-AC34, ADR-047 amendment A1). The obs.trace.> import took no remap when
// this was written and gained one in BR-AC36 (see traceLocalSubjectTmpl
// below); the argument was always the same: every tenant exports the identical
// literal "obs.pubsub.>", and the local subject is the only thing on the
// wire that tells a PLATFORM subscriber which account a message came from.
// The account boundary disambiguates *delivery*, not *provenance*.
const pubsubLocalSubjectTmpl = "monitor.%s.pubsub.>"

// traceLocalSubjectTmpl is the same remap for the obs.trace.> import
// (BR-AC36, Phase 48a). It was added a phase later than its pubsub sibling
// above, and the reason is worth keeping: the trace import genuinely did not
// need a remap while the Traces panel showed only a coarse PLATFORM/TENANT
// split, so BR-AC34 deliberately left it alone rather than changing a shipped
// pipeline in passing. Once the panel names the tenant (BR-054), the same
// argument that forced the pubsub remap applies here unchanged — every tenant
// exports the identical literal "obs.trace.>", so the local subject is the
// only thing on the wire that says which account a span came from.
//
// The token is trustworthy precisely because the NATS server inserts it at
// the account boundary. A tenant cannot write its own account name into a
// span payload and be believed; it cannot publish onto another tenant's
// monitor.* subject either, because it never sees one.
const traceLocalSubjectTmpl = "monitor.%s.trace.>"

// srvExportSubject is the second tenant-to-PLATFORM export (BR-AC31,
// Phase 30a) — the reverse leg needed so cross-account service discovery
// (observability-service's Services panel, Phase 30f) can reach every
// tenant's registered nats.go/micro services, the same $SRV.PING/INFO/STATS
// control protocol `nats micro stats` uses. Declared once so tenantExports
// and addPlatformMonitorImport's idempotency check can't drift apart, same
// convention as traceExportSubject above.
const srvExportSubject = "$SRV.>"

// monitorLocalSubjectTmpl is the tenant-scoped local subject PLATFORM's
// $SRV.> import remaps to (BR-AC31) — every tenant exports the identical
// literal "$SRV.>" subject, so without a per-tenant remap PLATFORM's import
// of a second tenant would collide with the first. Mirrors tenantImports'
// service() helper's remap direction, just reversed (tenant exports,
// PLATFORM imports, vs. PLATFORM exports, tenant imports).
const monitorLocalSubjectTmpl = "monitor.%s.srv.>"

// jsAPIExportPrefix is stripped from each jsAPIExportSubjects entry to build
// its tenant-scoped local remap (jsAPILocalSubjectTmpl) — declared once so
// tenantExports, addPlatformJSAPIImport, and their tests can't drift apart.
const jsAPIExportPrefix = "$JS.API."

// jsAPILocalSubjectTmpl is PLATFORM's per-tenant remap for each
// jsAPIExportSubjects entry (BR-AC32) — %s/%s is (tenantName, subject minus
// jsAPIExportPrefix). Every tenant exports the identical literal $JS.API.*
// subjects, so without this remap a second tenant's import would collide
// with the first, same rationale as monitorLocalSubjectTmpl above.
const jsAPILocalSubjectTmpl = "monitor.%s.js.%s"

// jsAPIExportSubjects is BR-AC32's seven-subject list for read-oriented
// JetStream/KV introspection — traced directly against the exact $JS.API
// calls dictionary/internal/rest/{kv,replay}.go makes (Main-POC-Plan.md's
// Phase 30 Design section has the full call-chain trace against the pinned
// nats.go@v1.52.0), not a blanket $JS.API.> export (ARCHITECTURE-ACCOUNTS.md:
// the full namespace grants stream management — create, delete, purge — not
// just visibility). Seven separate wildcard arities, not one pattern,
// because STREAM.LIST/STREAM.INFO.*/CONSUMER.CREATE.*/CONSUMER.CREATE.*.*/
// CONSUMER.CREATE.*.*.>/CONSUMER.MSG.NEXT.*.*/CONSUMER.DELETE.*.* can't be
// merged into fewer patterns without either overreaching or excluding one
// of them.
//
// CONSUMER.CREATE.*.*.> (Phase 30i live-verification fix) is distinct from
// CONSUMER.CREATE.*.* above, not redundant with it: nats.go's
// CreateOrUpdateConsumer embeds a FilterSubject directly into the published
// $JS.API subject when one is set
// (apiConsumerCreateWithFilterSubjectT = "CONSUMER.CREATE.%s.%s.%s") rather
// than putting it in the request body — jetstream.KeyValue.WatchAll (the KV
// Buckets panel's live-entries view, kv.go's kvBucketEntriesOnce) always
// creates its ephemeral consumer this way, filtered to the bucket's own
// $KV.<bucket>.> subject, so the wire subject is literally
// $JS.API.CONSUMER.CREATE.<stream>.<random-name>.$KV.<bucket>.> — a
// variable-length tail no fixed two-wildcard pattern can match, caught only
// once this ran against a real multi-account deployment (unit tests never
// exercise real NATS subject permissions). CONSUMER.CREATE.*.* (no filter)
// stays separate and necessary on its own: replay.go's OrderedConsumer
// call sets no FilterSubject, so it still publishes the plain two-token
// form.
//
// responseType is not uniform: CONSUMER.MSG.NEXT.*.* needs Stream, the same
// reason $SRV.> (BR-AC31) does — a single pull request with a batch size
// greater than one yields multiple individual replies, not one. Every other
// entry is a plain single-reply Singleton (list page / stream info /
// created-consumer info / delete ack).
var jsAPIExportSubjects = []struct {
	subject      string
	responseType jwt.ResponseType
}{
	{jsAPIExportPrefix + "STREAM.LIST", jwt.ResponseTypeSingleton},
	{jsAPIExportPrefix + "STREAM.INFO.*", jwt.ResponseTypeSingleton},
	{jsAPIExportPrefix + "CONSUMER.CREATE.*", jwt.ResponseTypeSingleton},
	{jsAPIExportPrefix + "CONSUMER.CREATE.*.*", jwt.ResponseTypeSingleton},
	{jsAPIExportPrefix + "CONSUMER.CREATE.*.*.>", jwt.ResponseTypeSingleton},
	{jsAPIExportPrefix + "CONSUMER.MSG.NEXT.*.*", jwt.ResponseTypeStream},
	{jsAPIExportPrefix + "CONSUMER.DELETE.*.*", jwt.ResponseTypeSingleton},
}

// tenantExports is the tenant-to-PLATFORM export declaration. The obs.trace.>
// Stream export's AllowTrace is deliberately omitted here — jwt.Export.
// Validate rejects it on anything but a Service-type export; the AllowTrace
// flag that actually matters for that pipeline belongs on PLATFORM's Stream
// *import* of it instead (addPlatformTraceImport below). The $SRV.> and
// $JS.API.* Service exports set ResponseType explicitly per BR-AC31/BR-AC32's
// design notes rather than leaving the library default unstated.
func tenantExports() jwt.Exports {
	exports := jwt.Exports{
		{Subject: jwt.Subject(traceExportSubject), Type: jwt.Stream},
		{Subject: jwt.Subject(pubsubExportSubject), Type: jwt.Stream},
		{Subject: jwt.Subject(srvExportSubject), Type: jwt.Service, ResponseType: jwt.ResponseTypeStream},
	}
	for _, e := range jsAPIExportSubjects {
		exports = append(exports, &jwt.Export{Subject: jwt.Subject(e.subject), Type: jwt.Service, ResponseType: e.responseType})
	}
	return exports
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

	if current, err := p.LookupAccountClaims(ctx, accountPub); err == nil {
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

	prior, err := p.LookupAccountClaims(ctx, accountPub)
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

// LookupAccountClaims reads the resolver's current complete account JWT.
// Limit updates happen while an account is active, so treating an absent or
// malformed current JWT as an error is safer than overwriting exports/imports.
// Reactivation deliberately falls back to CrossAccountOpts because a revoked
// account has no resolver JWT to look up.
func (p *Provisioner) LookupAccountClaims(_ context.Context, accountPub string) (*jwt.AccountClaims, error) {
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

// addPlatformTraceImport re-signs and re-pushes PLATFORM's own account JWT
// so it imports the newly-minted tenant's obs.trace.> export (Phase 28f).
// This is the one leg tenantImports's "complete PLATFORM-to-tenant contract"
// comment doesn't cover: everything else in this file grants a tenant
// account access to PLATFORM's exports, but a cross-account trace store
// needs the opposite direction too — PLATFORM importing a stream *from*
// each tenant. There's no wildcard cross-account import in NATS's
// decentralized JWT model, so this has to be a live per-tenant claims
// update, mirroring the same $SYS.REQ.CLAIMS.UPDATE mechanism the rest of
// this file already uses (see pushClaimsUpdate). Idempotent: if PLATFORM's
// claims already import tenantAccountPub's obs.trace.>, this is a no-op —
// safe to call unconditionally from CreateAccount without checking whether
// a prior attempt already succeeded.
func (p *Provisioner) addPlatformTraceImport(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	claims, err := p.LookupAccountClaims(ctx, platformPublicKey)
	if err != nil {
		return fmt.Errorf("lookup platform account claims: %w", err)
	}
	want := jwt.RenamingSubject(fmt.Sprintf(traceLocalSubjectTmpl, tenantName))
	for _, imp := range claims.Imports {
		if imp.Account != tenantAccountPub || imp.Subject != jwt.Subject(traceExportSubject) {
			continue
		}
		// Converge rather than merely dedupe. An account minted before
		// BR-AC36 already carries this import WITHOUT the remap, so a scan
		// that matched on (Account, Subject) alone — which is all the
		// pubsub sibling below does — would report success and change
		// nothing, and BR-AC36's stated rollout ("already-minted accounts
		// keep the un-remapped import until they are re-provisioned") would
		// be unachievable: re-provisioning would be a no-op and the only
		// path left would be a full wipe and reseed. Correcting a wrong
		// LocalSubject in place is what makes 48d's re-provision pass a real
		// operation.
		if imp.LocalSubject == want {
			return nil
		}
		imp.LocalSubject = want
		token, err := claims.Encode(p.operatorSigningKey)
		if err != nil {
			return fmt.Errorf("encode platform account jwt: %w", err)
		}
		return p.pushClaimsUpdate(ctx, token)
	}
	claims.Imports.Add(&jwt.Import{
		Account:      tenantAccountPub,
		Subject:      jwt.Subject(traceExportSubject),
		LocalSubject: want,
		Type:         jwt.Stream,
		AllowTrace:   true,
	})
	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return fmt.Errorf("encode platform account jwt: %w", err)
	}
	return p.pushClaimsUpdate(ctx, token)
}

// addPlatformPubsubImport re-signs and re-pushes PLATFORM's own account JWT
// so it imports the newly-minted tenant's obs.pubsub.> export (BR-AC34,
// Phase 43a) — addPlatformTraceImport's sibling, same mechanism, same
// idempotency contract (safe to call unconditionally from CreateAccount).
//
// Both carry a LocalSubject remap (ADR-047 amendment A1) for the same reason:
// the local subject an imported message arrives on is the only thing that
// carries provenance, and without it every tenant's stream lands on one
// identical local subject. This one got the remap first, in Phase 43a,
// because the Messages panel named the publishing tenant while the Traces
// panel still showed a coarse PLATFORM/TENANT split; BR-AC36 (Phase 48a)
// closed that gap, so the two imports now differ only in subject.
//
// Unlike its trace sibling, this one still only dedupes on
// (Account, Subject) — it does not correct a LocalSubject that is present
// but wrong. Nothing has ever needed it to, since this import has carried
// its remap from the day it was introduced, but see addPlatformTraceImport's
// convergence note for what that costs when a remap has to change.
func (p *Provisioner) addPlatformPubsubImport(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	claims, err := p.LookupAccountClaims(ctx, platformPublicKey)
	if err != nil {
		return fmt.Errorf("lookup platform account claims: %w", err)
	}
	for _, imp := range claims.Imports {
		if imp.Account == tenantAccountPub && imp.Subject == jwt.Subject(pubsubExportSubject) {
			return nil
		}
	}
	claims.Imports.Add(&jwt.Import{
		Account:      tenantAccountPub,
		Subject:      jwt.Subject(pubsubExportSubject),
		LocalSubject: jwt.RenamingSubject(fmt.Sprintf(pubsubLocalSubjectTmpl, tenantName)),
		Type:         jwt.Stream,
		AllowTrace:   true,
	})
	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return fmt.Errorf("encode platform account jwt: %w", err)
	}
	return p.pushClaimsUpdate(ctx, token)
}

// addPlatformMonitorImport re-signs and re-pushes PLATFORM's own account JWT
// so it imports the newly-minted tenant's $SRV.> export (BR-AC31, Phase
// 30a) — the service-discovery counterpart to addPlatformTraceImport above,
// same mechanism, same idempotency contract (safe to call unconditionally
// from CreateAccount). Unlike the trace import, this is a Service import and
// needs a tenant-scoped LocalSubject remap (monitorLocalSubjectTmpl): every
// tenant exports the identical literal "$SRV.>" subject, so PLATFORM's
// import of a second tenant would otherwise collide with the first.
func (p *Provisioner) addPlatformMonitorImport(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	claims, err := p.LookupAccountClaims(ctx, platformPublicKey)
	if err != nil {
		return fmt.Errorf("lookup platform account claims: %w", err)
	}
	for _, imp := range claims.Imports {
		if imp.Account == tenantAccountPub && imp.Subject == jwt.Subject(srvExportSubject) {
			return nil
		}
	}
	claims.Imports.Add(&jwt.Import{
		Account:      tenantAccountPub,
		Subject:      jwt.Subject(srvExportSubject),
		LocalSubject: jwt.RenamingSubject(fmt.Sprintf(monitorLocalSubjectTmpl, tenantName)),
		Type:         jwt.Service,
	})
	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return fmt.Errorf("encode platform account jwt: %w", err)
	}
	return p.pushClaimsUpdate(ctx, token)
}

// addPlatformJSAPIImport re-signs and re-pushes PLATFORM's own account JWT
// so it imports the newly-minted tenant's six $JS.API exports (BR-AC32,
// Phase 30b) — the JetStream/KV-introspection counterpart to
// addPlatformMonitorImport above, same mechanism. Unlike that function this
// checks each of the six subjects independently (not one all-or-nothing
// flag) so a prior partial failure — some of the six already imported,
// others not — is recovered by adding only what's missing, and issues at
// most one re-sign/push for whatever is newly needed rather than one round
// trip per subject.
func (p *Provisioner) addPlatformJSAPIImport(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	claims, err := p.LookupAccountClaims(ctx, platformPublicKey)
	if err != nil {
		return fmt.Errorf("lookup platform account claims: %w", err)
	}
	existing := map[string]bool{}
	for _, imp := range claims.Imports {
		if imp.Account == tenantAccountPub {
			existing[string(imp.Subject)] = true
		}
	}
	added := false
	for _, e := range jsAPIExportSubjects {
		if existing[e.subject] {
			continue
		}
		suffix := strings.TrimPrefix(e.subject, jsAPIExportPrefix)
		claims.Imports.Add(&jwt.Import{
			Account:      tenantAccountPub,
			Subject:      jwt.Subject(e.subject),
			LocalSubject: jwt.RenamingSubject(fmt.Sprintf(jsAPILocalSubjectTmpl, tenantName, suffix)),
			Type:         jwt.Service,
		})
		added = true
	}
	if !added {
		return nil
	}
	token, err := claims.Encode(p.operatorSigningKey)
	if err != nil {
		return fmt.Errorf("encode platform account jwt: %w", err)
	}
	return p.pushClaimsUpdate(ctx, token)
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
