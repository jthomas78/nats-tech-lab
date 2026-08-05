package auth

import (
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// tokenTTL bounds how long a minted browser user JWT is valid. BR-UA03
// (BUSINESS_RULES-ACCOUNTS.md) calls for 15-30 minutes plus a refresh-token
// renewal path in production; this POC has no renewal path yet, so the
// window is deliberately tight — a browser tab open longer than this simply
// reconnects (Phase 15d's useNatsConnection re-authenticates on any auth
// error), matching the short-lived-credential intent without yet building
// the renewal flow.
const tokenTTL = 5 * time.Minute

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
// Sub also includes obs.api.> (Phase 23) — the "supportive" observability
// side-channel that traces api.* request/reply traffic (never on a business
// path, ARCHITECTURE-COMMUNICATIONS.md § 2). This is distinct from rpc.*,
// which stays excluded per the rpc.* discussion above: obs.api.> only
// observes traffic this same credential is already permitted to originate
// (api.>), it doesn't grant reach into any new business subject.
func MintBrowserToken(accountPub, accountSigningKeySeed, tenant, wsURL string) (ConnectInfo, error) {
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
	claims.Permissions.Sub.Allow.Add("api.>", "notify.>", "obs.api.>", "_INBOX.>")
	claims.Expires = time.Now().Add(tokenTTL).Unix()

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
// subscribe-only subject set (notify.accounts.account.> plus the
// REFDATA/RPCTRACE notify.* subjects Phase 23 adds) and, unlike
// MintBrowserToken, no publish grant at all — this connection only watches,
// it never issues commands. Pub is explicitly denied (Deny.Add(">")) rather
// than left unset, because an empty/unset Allow list means "allow
// everything" in NATS permission semantics, not "allow nothing".
func MintAdminToken(accountPub, accountSigningKeySeed, wsURL string) (ConnectInfo, error) {
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
	claims.Permissions.Sub.Allow.Add("notify.accounts.account.>", "notify._platform.refdata.>", "notify._platform.rpctrace.>")
	claims.Expires = time.Now().Add(tokenTTL).Unix()

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
