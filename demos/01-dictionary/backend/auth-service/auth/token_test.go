package auth_test

import (
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/auth-service/auth"
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

	It("mints a JWT signed by the account's signing key, scoped to rpc.>/notify.> unparameterized by tenant", func() {
		info, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222")
		Expect(err).NotTo(HaveOccurred())
		Expect(info.WSUrl).To(Equal("ws://localhost:9222"))
		Expect(info.Tenant).To(Equal("acme"))
		Expect(info.JWT).NotTo(BeEmpty())
		Expect(info.NKeySeed).NotTo(BeEmpty())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.IssuerAccount).To(Equal(accountPub))

		// Not "rpc.acme.shipping.>": the {context} token in a real subject
		// (rpc.{fleetContext}.shipping...) is the fleet qualifier
		// (global/atlantic-fleet/pacific-fleet), never the tenant name — see
		// MintBrowserToken's doc comment. "acme" would never match a real
		// subject, silently breaking every browser call.
		Expect(claims.Permissions.Pub.Allow).To(ConsistOf("rpc.>", "_INBOX.>"))
		Expect(claims.Permissions.Sub.Allow).To(ConsistOf("rpc.>", "notify.>", "_INBOX.>"))
		Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(ContainSubstring("evt.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$KV")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$JS.API")))
	})

	It("sets a short expiry in the future", func() {
		before := time.Now()
		info, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222")
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		expiry := time.Unix(claims.Expires, 0)
		Expect(expiry).To(BeTemporally(">", before))
		Expect(expiry).To(BeTemporally("<", before.Add(10*time.Minute)))
	})

	It("produces an NKey seed matching a valid NATS user identity", func() {
		info, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222")
		Expect(err).NotTo(HaveOccurred())

		userKP, err := nkeys.FromSeed([]byte(info.NKeySeed))
		Expect(err).NotTo(HaveOccurred())
		userPub, err := userKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Subject).To(Equal(userPub), "JWT subject must be the same user identity as the returned seed")
	})

	It("gives every tenant the identical rpc.>/notify.> subject permissions — isolation comes from the account, not the subject", func() {
		infoAcme, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "acme", "ws://localhost:9222")
		Expect(err).NotTo(HaveOccurred())
		infoGlobex, err := auth.MintBrowserToken(accountPub, accountSigningSeed, "globex", "ws://localhost:9222")
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
		_, err := auth.MintBrowserToken(accountPub, "not-a-real-seed", "acme", "ws://localhost:9222")
		Expect(err).To(HaveOccurred())
	})
})
