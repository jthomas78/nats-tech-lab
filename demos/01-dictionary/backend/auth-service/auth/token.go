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
// "public" (Sea Freight Flow) subject surface — rpc.> and notify.> plus
// _INBOX.> for request/reply internals — signed by the tenant account's own
// signing key, never the operator key (same non-negotiable as
// accounts-service's CreateUser, accounts/provisioner.go). No $KV.>, no
// $JS.API.>, no evt.>: unlike accounts-service's service-to-service users
// (unrestricted within their account), a browser-held credential is
// explicitly the read-restricted, write-restricted-to-commands boundary
// described in Main-POC-Plan.md Phase 15.
//
// The subject patterns are NOT parameterized by tenant, and that's
// deliberate, not an oversight. Subjects are shaped
// rpc.{context}.shipping.{entity}.{action}.v1, where {context} is the
// COMPANY / BUSINESS-UNIT scope (domain.ShipSubject's {context} token) — a
// data-partition and KV-bucket qualifier, on a completely different axis
// from tenant. Tenant is the NATS ACCOUNT itself (acme/globex/…) and never
// appears in a subject at all; region is a separate regional deployment and
// likewise never appears. See ARCHITECTURE-COMMUNICATIONS.md § 2.3 for the
// full rule.
//
// So substituting the tenant name into the context slot — e.g.
// "rpc.acme.shipping.>" intending "ACME's traffic" — would be wrong on two
// counts: it conflates the two axes, and it would only match subjects whose
// context value happens to equal "acme", not ACME's traffic generally.
// (This service's own contexts are fleet-named in the shipping demo:
// "global"/"atlantic-fleet"/"pacific-fleet" — a fleet being that demo's
// business-unit instance.)
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
// PENDING PHASE 16b: this grant is rpc.> today, but browser traffic is
// moving to the api.* family (rpc.* becomes service-to-service only). When
// 16b lands, this must grant api.>/notify.> and DROP rpc.> — a browser
// credential must never hold rpc.>, so that a future backend-only rpc.*
// endpoint inside a tenant account can't become browser-reachable by
// accident (ARCHITECTURE-COMMUNICATIONS.md § 2.4).
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
	claims.Permissions.Pub.Allow.Add("rpc.>", "_INBOX.>")
	claims.Permissions.Sub.Allow.Add("rpc.>", "notify.>", "_INBOX.>")
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
