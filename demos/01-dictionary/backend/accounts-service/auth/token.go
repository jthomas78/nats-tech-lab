package auth

import (
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
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
func MintBrowserToken(accountPub, accountSigningKeySeed, tenant, wsURL string, ttl time.Duration) (ConnectInfo, error) {
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
	claims.Name = "browser-" + tenant
	claims.IssuerAccount = accountPub
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
	claims.Expires = time.Now().Add(ttl).Unix()

	token, err := claims.Encode(signingKP)
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("encode user jwt: %w", err)
	}

	return ConnectInfo{
		WSUrl:    wsURL,
		JWT:      token,
		NKeySeed: string(userSeed),
		Tenant:   tenant,
	}, nil
}

// MintAdminToken mints an ephemeral NATS user JWT under the PLATFORM
// account for the Admin UI's own connection-health/observability surface
// (Main-POC-Plan.md Phase 23, BR-AC18) — a distinct profile from
// MintBrowserToken's tenant-shaped one, not a parameterization of it.
// PLATFORM is not a tenant (no Status/Suspend/Reactivate lifecycle), so this
// deliberately isn't "MintBrowserToken with tenant=platform": it has its own
// subscribe-only subject set (notify.accounts.account.> plus the REFDATA
// notify.* subject Phase 23 adds, plus notify._platform.kv.trace-request-reply.> for the
// Admin UI's trace waterfall/messages panel, Phase 28g —
// internal/kvstore.Store.EnableNotify's existing
// notify.{context}.kv.{bucket}.{key}.changed publish, reused unchanged for
// the trace-request-reply KV bucket rather than a bespoke trace-notify subject) and,
// unlike MintBrowserToken, no publish grant at all — this connection only
// watches, it never issues commands. Pub is explicitly denied
// (Deny.Add(">")) rather than left unset, because an empty/unset Allow list
// means "allow everything" in NATS permission semantics, not "allow
// nothing". notify._platform.rpctrace.> (Phase 23) was retired in Phase
// 28g along with the RPCTRACE stream and its notify bridge
// (eventhandler.RegisterRPCTraceNotify) — nothing publishes there anymore.
func MintAdminToken(accountPub, accountSigningKeySeed, wsURL string, ttl time.Duration) (ConnectInfo, error) {
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
	claims.Name = "admin-platform"
	claims.IssuerAccount = accountPub
	claims.Permissions.Pub.Deny.Add(">")
	claims.Permissions.Sub.Allow.Add("notify.accounts.account.>", "notify._platform.refdata.>", "notify._platform.kv.trace-request-reply.>")
	claims.Expires = time.Now().Add(ttl).Unix()

	token, err := claims.Encode(signingKP)
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("encode user jwt: %w", err)
	}

	return ConnectInfo{
		WSUrl:    wsURL,
		JWT:      token,
		NKeySeed: string(userSeed),
		Tenant:   "platform",
	}, nil
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
// Unlike MintAdminToken (subscribe-only, no publish grant at all — that
// connection only watches), this credential DOES publish: it is the one
// that actually drives refdata-service's api.*.refdata.> business AND
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
func MintRefdataAdminToken(accountPub, accountSigningKeySeed, wsURL string, ttl time.Duration) (ConnectInfo, error) {
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
	claims.Name = "refdata-admin-platform"
	claims.IssuerAccount = accountPub
	claims.Permissions.Pub.Allow.Add("api.*.refdata.>", "_INBOX.>")
	claims.Permissions.Sub.Allow.Add("api.*.refdata.>", "notify._platform.refdata.>", "_INBOX.>")
	claims.Expires = time.Now().Add(ttl).Unix()

	token, err := claims.Encode(signingKP)
	if err != nil {
		return ConnectInfo{}, fmt.Errorf("encode user jwt: %w", err)
	}

	return ConnectInfo{
		WSUrl:    wsURL,
		JWT:      token,
		NKeySeed: string(userSeed),
		Tenant:   "platform",
	}, nil
}
