package auth_test

import (
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
)

var _ = Describe("MintBrowserToken", func() {
	var accountKP nkeys.KeyPair
	var accountPub, accountSigningSeed string

	BeforeEach(func() {
		var err error
		accountKP, err = nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		accountPub, err = accountKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		signingKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		seed, err := signingKP.Seed()
		Expect(err).NotTo(HaveOccurred())
		accountSigningSeed = string(seed)
	})

	It("mints a JWT signed by the account's signing key, scoped to api.>/notify.> unparameterized by tenant", func() {
		info, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.WSUrl).To(Equal("ws://localhost:9222"))
		Expect(info.Tenant).To(Equal("acme"))
		Expect(info.JWT).NotTo(BeEmpty())
		Expect(info.NKeySeed).NotTo(BeEmpty())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.IssuerAccount).To(Equal(accountPub))

		// Not "api.acme.shipping.>": the {context} token in a real subject
		// (api.{context}.shipping...) is the company/business-unit scope
		// (this demo's values are fleet-named: global/atlantic-fleet/
		// pacific-fleet), never the tenant name — see MintBrowserToken's doc
		// comment. "acme" would never match a real subject, silently
		// breaking every browser call.
		Expect(claims.Permissions.Pub.Allow).To(ConsistOf("api.>", "_INBOX.>"))
		Expect(claims.Permissions.Sub.Allow).To(ConsistOf("api.>", "notify.>", "_INBOX.>"))
		Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(ContainSubstring("evt.")))
		// obs.api.> (Phase 23) is retired (Phase 28g) — it was the RPC
		// panel's old live-tail grant, dead since Phase 28a-28e replaced
		// browserrpc's publishObs call with a natstrace span; the panel's
		// [messages] tab now derives from obs.trace.*/the trace-request-reply KV bucket
		// instead, which this credential is never granted (BR-036:
		// obs.trace.* publishes to PLATFORM only). obs.rpc.> and bare
		// rpc.> stay excluded too — this credential must never observe or
		// reach service-to-service rpc.* traffic.
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("obs.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(Equal("rpc.>")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$KV")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$JS.API")))
	})

	It("stamps the expiry from the ttl argument", func() {
		before := time.Now()
		// A non-default ttl proves the argument flows through to Expires
		// rather than a hardcoded constant.
		info, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 22*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		expiry := time.Unix(claims.Expires, 0)
		Expect(expiry).To(BeTemporally(">", before.Add(21*time.Minute)))
		Expect(expiry).To(BeTemporally("<", before.Add(23*time.Minute)))
	})

	It("produces an NKey seed matching a valid NATS user identity", func() {
		info, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		userKP, err := nkeys.FromSeed([]byte(info.NKeySeed))
		Expect(err).NotTo(HaveOccurred())
		userPub, err := userKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Subject).To(Equal(userPub), "JWT subject must be the same user identity as the returned seed")
	})

	It("gives every tenant the identical api.>/notify.> subject permissions — isolation comes from the account, not the subject", func() {
		infoAcme, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		infoGlobex, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "globex", "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		claimsAcme, err := jwt.DecodeUserClaims(infoAcme.JWT)
		Expect(err).NotTo(HaveOccurred())
		claimsGlobex, err := jwt.DecodeUserClaims(infoGlobex.JWT)
		Expect(err).NotTo(HaveOccurred())

		// Deliberately equal: see MintBrowserToken's doc comment on why the
		// subject pattern is never parameterized by tenant. What actually
		// differs per tenant is IssuerAccount (tested separately below) and,
		// in production, which account's signing key seed the caller passes
		// in — not this JWT's subject permissions.
		Expect(claimsAcme.Permissions.Sub.Allow).To(ConsistOf(claimsGlobex.Permissions.Sub.Allow))
		Expect(claimsAcme.Permissions.Pub.Allow).To(ConsistOf(claimsGlobex.Permissions.Pub.Allow))
	})

	It("returns an error when the signing key seed is invalid", func() {
		_, err := auth.MintBrowserToken(accountPub, "not-a-real-seed", "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("MintAdminToken", func() {
	var accountKP nkeys.KeyPair
	var accountPub, accountSigningSeed string

	BeforeEach(func() {
		var err error
		accountKP, err = nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		accountPub, err = accountKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		signingKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		seed, err := signingKP.Seed()
		Expect(err).NotTo(HaveOccurred())
		accountSigningSeed = string(seed)
	})

	// BR-AC18: subscribe-only, scoped to notify.accounts.account.> plus the
	// REFDATA notify.* subject Phase 23 adds and notify._platform.kv.trace-request-reply.>
	// (Phase 28g) — no publish grant, no $JS.API.>/$KV.>, no tenant-shaped
	// api.>/notify.{tenant}.* access. notify._platform.rpctrace.> (Phase 23)
	// was retired in Phase 28g along with the RPCTRACE stream itself.
	It("mints a sub-only JWT scoped to notify.accounts.account.>, notify._platform.refdata.>, and notify._platform.kv.trace-request-reply.>, with publish denied entirely", func() {
		info, err := auth.MintAdminToken(accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.WSUrl).To(Equal("ws://localhost:9222"))
		Expect(info.Tenant).To(Equal("platform"))
		Expect(info.JWT).NotTo(BeEmpty())
		Expect(info.NKeySeed).NotTo(BeEmpty())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.IssuerAccount).To(Equal(accountPub))

		Expect(claims.Permissions.Sub.Allow).To(ConsistOf(
			"notify.accounts.account.>", "notify._platform.refdata.>", "notify._platform.kv.trace-request-reply.>",
		))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("rpctrace")))
		Expect(claims.Permissions.Pub.Allow).To(BeEmpty())
		Expect(claims.Permissions.Pub.Deny).To(ConsistOf(">"))

		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("api.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("rpc.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$KV")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$JS.API")))
	})

	It("stamps the expiry from the ttl argument", func() {
		before := time.Now()
		info, err := auth.MintAdminToken(accountPub, accountSigningSeed, "ws://localhost:9222", 22*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		expiry := time.Unix(claims.Expires, 0)
		Expect(expiry).To(BeTemporally(">", before.Add(21*time.Minute)))
		Expect(expiry).To(BeTemporally("<", before.Add(23*time.Minute)))
	})

	It("returns an error when the signing key seed is invalid", func() {
		_, err := auth.MintAdminToken(accountPub, "not-a-real-seed", "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(HaveOccurred())
	})
})
