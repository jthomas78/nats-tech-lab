package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// The TTL stamped on minted browser/admin JWTs is no longer a constant here:
// it is a durable, Admin-UI-configurable system setting (BR-AC20,
// accounts.TokenTTLConfig), read at mint time and passed into MintBrowserToken
// / MintAdminToken as the `ttl` argument. Callers (auth/handler.go) resolve it
// from accounts.Store.GetTokenTTLConfig, falling back to
// accounts.DefaultTokenTTLConfig (15 min). This POC still has no in-place
// refresh path — a browser tab open past the TTL simply reconnects (Phase 15d's
// useNatsConnection/connectionFactory re-authenticate on connection close),
// minting a fresh credential — so the setting bounds the reconnect cadence and
// the credential blast radius, within the hard 15–30 minute BR-UA03 envelope.

// ConnectInfo is everything a browser needs to open its own NATS WebSocket
// connection: where to dial, and a short-lived, permission-restricted user
// JWT + matching NKey seed to authenticate with.
type ConnectInfo struct {
	WSUrl    string `json:"wsUrl"`
	JWT      string `json:"jwt"`
	NKeySeed string `json:"nkeySeed"`
	Tenant   string `json:"tenant"`
}

// MintBrowserToken mints an ephemeral NATS user JWT scoped to exactly the
// "public" (Sea Freight Flow) subject surface — api.> and notify.> plus
// _INBOX.> for request/reply internals — signed by the tenant account's own
// signing key, never the operator key (same non-negotiable as
// accounts-service's CreateUser, accounts/provisioner.go). No $KV.>, no
// $JS.API.>, no evt.>, and — as of Phase 16b — no rpc.> either: unlike
// accounts-service's service-to-service users (unrestricted within their
// account), a browser-held credential is explicitly the read-restricted,
// write-restricted-to-commands boundary described in Main-POC-Plan.md
// Phase 15, and it must never be able to reach or observe rpc.* traffic
// (service-to-service only — ARCHITECTURE-COMMUNICATIONS.md § 2.4). Before
// 16b this grant included rpc.> because shipping-service's browser-facing
// endpoints were themselves still on rpc.* (internal/natsrpc); they moved to
// api.* (internal/browserrpc) in that phase specifically so this credential
// could stop needing rpc.> at all — leaving it here would let this browser
// credential reach any future genuine backend-to-backend rpc.* endpoint
// added inside the same tenant account, purely by accident of sharing an
// account, which is exactly the gap dropping it closes.
//
// The subject patterns are NOT parameterized by tenant, and that's
// deliberate, not an oversight. Subjects are shaped
// api.{context}.shipping.{entity}.{action}.v1, where {context} is the
// COMPANY / BUSINESS-UNIT scope (domain.ShipSubject's {context} token) — a
// data-partition and KV-bucket qualifier, on a completely different axis
// from tenant. Tenant is the NATS ACCOUNT itself (acme/globex/…) and never
// appears in a subject at all; region is a separate regional deployment and
// likewise never appears. See ARCHITECTURE-COMMUNICATIONS.md § 2.3 for the
// full rule.
//
// So substituting the tenant name into the context slot — e.g.
// "api.acme.shipping.>" intending "ACME's traffic" — would be wrong on two
// counts: it conflates the two axes, and it would only match subjects whose
// context value happens to equal "acme", not ACME's traffic generally.
// (This service's own contexts are company/business-unit named in the
// shipping demo, Phase 16e: "acme"/"acme-atlantic-fleet"/"acme-pacific-fleet".
// Note the company-wide context is spelled identically to the ACME tenant's
// own account name — that's a coincidence of this being a single-company-
// per-tenant demo (decision 11 in Main-POC-Plan.md § Phase 16), not evidence
// the two axes have merged; a second company sharing ACME's account would
// still need its own, differently-spelled context value.)
//
// Tenant isolation is already fully enforced by which ACCOUNT this JWT
// authenticates into (accountPub/accountSigningKeySeed) — a connection
// authenticated into ACME's account can never observe or reach GLOBEX's
// subjects regardless of subject pattern, because NATS accounts are a hard
// server-side namespace boundary, not a permission filter within one shared
// namespace. The grant therefore only needs to additionally restrict PUB
// vs. SUB and rule out $KV.>/$JS.API.>/evt.> within that already-isolated
// account.
//
// Sub no longer includes obs.api.> (Phase 23 granted it; Phase 28g retires
// it) — that "supportive" observability side-channel was already dead
// before the retirement: Phase 28a-28e replaced browserrpc's publishObs
// call with a natstrace span, so nothing has published to obs.api.> since,
// and the Admin UI's [messages] tab (the sole consumer of this grant) now
// derives from obs.trace.*/the trace-request-reply KV bucket instead (BR-026's Phase
// 28g amendment, BUSINESS_RULES-SHIPPING.md). obs.trace.* itself is never
// granted here — it publishes to the PLATFORM account only, per BR-036.
func MintBrowserToken(ctx context.Context, reg accounts.UserRegistry, accountPub, accountSigningKeySeed, tenant, wsURL string, ttl time.Duration) (ConnectInfo, error) {
	return mintUserToken(ctx, reg, accountPub, accountSigningKeySeed, "browser-"+tenant, tenant, wsURL, ttl, func(claims *jwt.UserClaims) {
		claims.Permissions.Pub.Allow.Add("api.>", "_INBOX.>")
		claims.Permissions.Sub.Allow.Add("api.>", "notify.>", "_INBOX.>")
		// BR-D41 (Phase 32): refdata-service's api.*.refdata.admin.* namespace is
		// corpus/context/type/locale/item/reference/localization administration —
		// never a browser operation. Deny wins over the broader api.> Allow above
		// on both directions, so a browser credential can reach every other
		// api.*.refdata.* subject but never this prefix, regardless of which
		// tenant account it authenticates into.
		claims.Permissions.Pub.Deny.Add("api.*.refdata.admin.>")
		claims.Permissions.Sub.Deny.Add("api.*.refdata.admin.>")
	})
}

// MintAdminToken mints an ephemeral NATS user JWT under PLATFORM for the
// Admin UI's single browser connection (BR-AC18). PLATFORM is not a tenant,
// so this is a separate least-privilege profile rather than MintBrowserToken
// parameterized with tenant=platform. It subscribes to account/refdata and
// centralized observability notifications, plus reply inboxes for exactly
// three read-only refdata requests: type.list, locales.list, and context.list.
// No tenant business api.* or direct obs.* subject is reachable.
//
// The notification set is notify.accounts.account.>, the REFDATA notify.*
// subject, notify._platform.kv.trace-request-reply.> for the
// Admin UI's trace waterfall/RPC panel, Phase 28g, and
// notify._platform.kv.pubsub-messages.> for the Messages panel, Phase 43b —
// internal/kvstore.Store.EnableNotify's existing
// notify.{context}.kv.{bucket}.{key}.changed publish, reused unchanged for
// the trace-request-reply KV bucket rather than a bespoke trace-notify subject) and,
// Unlike MintBrowserToken, it has no broad api.> grant. A non-empty Pub.Allow
// list makes every unlisted publish forbidden in NATS permission semantics.
// notify._platform.rpctrace.> (Phase 23) was retired in Phase
// 28g along with the RPCTRACE stream and its notify bridge
// (eventhandler.RegisterRPCTraceNotify) — nothing publishes there anymore.
func MintAdminToken(ctx context.Context, reg accounts.UserRegistry, accountPub, accountSigningKeySeed, wsURL string, ttl time.Duration) (ConnectInfo, error) {
	return mintUserToken(ctx, reg, accountPub, accountSigningKeySeed, "admin-app", "platform", wsURL, ttl, func(claims *jwt.UserClaims) {
		claims.Permissions.Pub.Allow.Add(
			"api._platform.refdata.type.list.v1",
			"api._platform.refdata.locales.list.v1",
			"api._platform.refdata.context.list.v1",
		)
		// Phase 50b (BR-AC40) — the Users panel's subjects, served by this
		// service's own api.* adapter on the PLATFORM connection. Enumerated
		// as individual subjects, not an api._platform.accounts.> prefix: the
		// Pub.Allow list above is an explicit allowlist precisely so a future
		// accounts endpoint is not reachable by a browser credential merely
		// by being named consistently.
		//
		// Phase 51b adds user.revoke.v1, which makes this set no longer
		// read-only. That is intended — the Users panel performs the
		// revocation itself — but it does mean the set now grows straight
		// from UsersAdapterSubjects(), so a new endpoint IS granted to the
		// Admin UI the moment it is registered. token_test.go's exact
		// ConsistOf is what forces that to be a deliberate decision: adding
		// an endpoint breaks it, and the fix is to look at the subject and
		// decide, not to paste it in.
		claims.Permissions.Pub.Allow.Add(accounts.UsersAdapterSubjects()...)
		claims.Permissions.Sub.Allow.Add("notify.accounts.account.>", "notify._platform.refdata.>", "notify._platform.kv.trace-request-reply.>",
			// Phase 43b (BR-047): the Messages panel's live feed, the same
			// bucket-notify shape as trace-request-reply above. This grants the
			// KV-change notify only — obs.pubsub.> itself is still never granted
			// to a browser credential (BR-AC34).
			"notify._platform.kv.pubsub-messages.>", "_INBOX.>")
	})
}

// MintRefdataAdminToken mints an ephemeral NATS user JWT under the PLATFORM
// account for the refdata admin UI's (frontend/refdata) cross-tenant
// operator connection (Phase 32, BR-D40/BR-D41 amendment). frontend/refdata
// has no tenant/account concept of its own — like the Admin UI, it is a
// platform-operator tool, not a Sea Freight Flow-style tenant app — so it
// gets its own mint function under the SAME PLATFORM account MintAdminToken
// uses, rather than a MintBrowserToken variant scoped to one tenant: a
// tenant token would either need the api.*.refdata.admin.> deny lifted
// (conflating refdata-admin rights with tenant membership, so any tenant
// could edit shared _platform standards) or would have no natural tenant to
// authenticate as in the first place.
//
// Unlike MintAdminToken's three read-only subjects, this credential drives
// refdata-service's full api.*.refdata.> business AND
// admin endpoints (corpus draft/publish/rollback, item/type/locale/
// reference/localization registration, business reads alike) — mounted on
// refdata-service's PLATFORM connection precisely so this credential can
// reach them (refdata/composition.go's browserrpc-on-PLATFORM mount). The
// grant is scoped to exactly api.*.refdata.> (refdata's own second subject
// token) — not api.> — so this credential cannot reach any other service's
// api.* surface, mirroring the least-privilege principle BR-D41's
// browser-token deny already establishes in the other direction.
//
// Sub additionally allows notify._platform.refdata.> — the BR-D42 notify
// bridge frontend/refdata subscribes to in place of the retired
// /api/refdata-watch SSE stream, the same subject MintAdminToken already
// grants the Admin UI for the identical reason.
func MintRefdataAdminToken(ctx context.Context, reg accounts.UserRegistry, accountPub, accountSigningKeySeed, wsURL string, ttl time.Duration) (ConnectInfo, error) {
	return mintUserToken(ctx, reg, accountPub, accountSigningKeySeed, "operator-app", "platform", wsURL, ttl, func(claims *jwt.UserClaims) {
		claims.Permissions.Pub.Allow.Add("api.*.refdata.>", "_INBOX.>")
		claims.Permissions.Sub.Allow.Add("api.*.refdata.>", "notify._platform.refdata.>", "_INBOX.>")
	})
}

// mintUserToken holds the key-generation/claims/encode boilerplate shared by
// every Mint*Token above; configure applies the profile-specific permission
// grants (and any other claims) before the claims are signed.
//
// It also records the session in the user registry, in the BR-AC38 order:
// keypair, pending row, sign, active. A browser session is a NATS user like
// any other — thousands of them over a stack's life — and the Users panel
// has no other way to know one existed, since a user JWT is never stored
// server-side and /connz only knows who is connected right now. The registry
// is not optional here (ErrNoUserRegistry): every caller on this path
// already reads Postgres for the account's signing key seed and the TTL
// config before reaching this function, so recording costs no new
// dependency, and a session credential nothing recorded is one nothing can
// later account for.
//
// Unlike a service credential, a session carries an expiry — which is what
// lets a row stuck at pending be swept once its TTL passes
// (Store.ReapExpiredSessions, Phase 52) instead of waiting for an operator.
func mintUserToken(ctx context.Context, reg accounts.UserRegistry, accountPub, accountSigningKeySeed, name, tenant, wsURL string, ttl time.Duration, configure func(*jwt.UserClaims)) (ConnectInfo, error) {
	if reg == nil {
		return ConnectInfo{}, accounts.ErrNoUserRegistry
	}
	signingKP, err := nkeys.FromSeed([]byte(accountSigningKeySeed))
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("load account signing key: %w", err)
	}

	userKP, err := nkeys.CreateUser()
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("generate user key: %w", err)
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("user public key: %w", err)
	}
	userSeed, err := userKP.Seed()
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("user seed: %w", err)
	}

	claims := jwt.NewUserClaims(userPub)
	claims.Name = name
	claims.IssuerAccount = accountPub
	configure(claims)
	expires := time.Now().Add(ttl)
	claims.Expires = expires.Unix()

	// The signing key's public key is the row's issuer (BR-AC41) — the
	// account key alone can't tell the Users panel whether these permissions
	// are the ones the server enforces or a scope's template replaced them.
	signingPub, err := signingKP.PublicKey()
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("account signing key public key: %w", err)
	}
	perms := claims.UserPermissionLimits

	if err := reg.RecordPendingUser(ctx, accounts.NewUser{
		PublicKey:   userPub,
		Name:        name,
		Account:     tenant,
		AccountKey:  accountPub,
		IssuerKey:   signingPub,
		Permissions: &perms,
		Kind:        accounts.UserKindSession,
		Bearer:      claims.BearerToken,
		ExpiresAt:   &expires,
		Source:      accounts.UserSourceService,
	}); err != nil {
		return ConnectInfo{}, err
	}

	token, err := claims.Encode(signingKP)
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("encode user jwt: %w", err)
	}

	if err := reg.MarkUserActive(ctx, userPub); err != nil {
		return ConnectInfo{}, err
	}

	return ConnectInfo{
		WSUrl:    wsURL,
		JWT:      token,
		NKeySeed: string(userSeed),
		Tenant:   tenant,
	}, nil
}
