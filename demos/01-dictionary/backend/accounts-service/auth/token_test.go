package auth_test

import (
	"context"
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
		info, err := auth.MintBrowserToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
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

	// BR-D41 (Phase 32): refdata-service's admin namespace is corpus and
	// governance administration, never a browser operation. The broad
	// api.> Allow above would otherwise reach it, since refdata-service's
	// api.* adapter runs inside every tenant account (BR-D40) — so the deny
	// is what keeps the namespace split a real, server-enforced boundary
	// rather than a naming convention.
	It("BR-D41: denies api.*.refdata.admin.> on both pub and sub while leaving the business subjects reachable", func() {
		info, err := auth.MintBrowserToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())

		Expect(claims.Permissions.Pub.Deny).To(ContainElement("api.*.refdata.admin.>"))
		Expect(claims.Permissions.Sub.Deny).To(ContainElement("api.*.refdata.admin.>"))

		// The business half must stay reachable — a deny broad enough to
		// also cover api.*.refdata.item.get.v1 would silently break every
		// label/locale read the frontends now make over NATS.
		Expect(claims.Permissions.Pub.Allow).To(ContainElement("api.>"))
		Expect(claims.Permissions.Pub.Deny).NotTo(ContainElement("api.*.refdata.>"))
		Expect(claims.Permissions.Sub.Deny).NotTo(ContainElement("api.*.refdata.>"))
	})

	It("stamps the expiry from the ttl argument", func() {
		before := time.Now()
		// A non-default ttl proves the argument flows through to Expires
		// rather than a hardcoded constant.
		info, err := auth.MintBrowserToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 22*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		expiry := time.Unix(claims.Expires, 0)
		Expect(expiry).To(BeTemporally(">", before.Add(21*time.Minute)))
		Expect(expiry).To(BeTemporally("<", before.Add(23*time.Minute)))
	})

	It("produces an NKey seed matching a valid NATS user identity", func() {
		info, err := auth.MintBrowserToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
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
		infoAcme, err := auth.MintBrowserToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "acme", "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		infoGlobex, err := auth.MintBrowserToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "globex", "ws://localhost:9222", 15*time.Minute)
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
		_, err := auth.MintBrowserToken(context.Background(), &fakeSessionRegistry{}, accountPub, "not-a-real-seed", "acme", "ws://localhost:9222", 15*time.Minute)
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

	// BR-AC18: one PLATFORM credential for centralized notifications and the
	// three read-only refdata calls Admin needs for UI copy/context bootstrap.
	// No broad api.>, tenant notify.*, obs.*, $JS.API.>, or $KV.> access.
	It("mints one PLATFORM JWT scoped to centralized notifications and exact read-only refdata requests", func() {
		info, err := auth.MintAdminToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.WSUrl).To(Equal("ws://localhost:9222"))
		Expect(info.Tenant).To(Equal("platform"))
		Expect(info.JWT).NotTo(BeEmpty())
		Expect(info.NKeySeed).NotTo(BeEmpty())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Name).To(Equal("admin-app"))
		Expect(claims.IssuerAccount).To(Equal(accountPub))

		// Phase 43b (BR-047) adds the second KV-notify subject, for the
		// Messages panel's live feed. obs.pubsub.> itself is deliberately
		// absent — a browser credential is never granted it (BR-AC34), it
		// reads the projected bucket over HTTP and only *watches* for changes
		// here.
		Expect(claims.Permissions.Sub.Allow).To(ConsistOf(
			"notify.accounts.account.>", "notify._platform.refdata.>",
			"notify._platform.kv.trace-request-reply.>", "notify._platform.kv.pubsub-messages.>",
			"_INBOX.>",
		))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("obs.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("rpctrace")))
		// Phase 50b (BR-AC40) adds the user-registry subjects; Phase 51b
		// (BR-AC43) adds the revoke write to them. They are enumerated here
		// rather than matched as an api._platform.accounts.> prefix on
		// purpose: MintAdminToken builds this set from
		// UsersAdapterSubjects(), so a new accounts endpoint would otherwise
		// reach the browser silently. Failing this assertion is the intended
		// behaviour — it forces a decision about the new subject.
		Expect(claims.Permissions.Pub.Allow).To(ConsistOf(
			"api._platform.refdata.type.list.v1",
			"api._platform.refdata.locales.list.v1",
			"api._platform.refdata.context.list.v1",
			"api._platform.accounts.user.list.v1",
			"api._platform.accounts.user.get.v1",
			"api._platform.accounts.user.revoke.v1",
			// Phase 4 (BR-AS31) — the registry curation surface, moved off
			// REST. Phase 7b (BR-AS38) adds the trusted-publishers table to
			// it. Operator-scoped: these are granted here and to no other
			// credential, and MintShellToken below must never carry them.
			"api._platform.registry.entries.curated.v1",
			"api._platform.registry.entries.upsert.v1",
			"api._platform.registry.entries.set-enabled.v1",
			"api._platform.registry.audit.list.v1",
			"api._platform.registry.publishers.list.v1",
			"api._platform.registry.publishers.write.v1",
		))
		Expect(claims.Permissions.Pub.Deny).To(BeEmpty())

		Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(Equal("api.>")))
		Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(ContainSubstring("shipping")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("rpc.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$KV")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$JS.API")))
	})

	It("stamps the expiry from the ttl argument", func() {
		before := time.Now()
		info, err := auth.MintAdminToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 22*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		expiry := time.Unix(claims.Expires, 0)
		Expect(expiry).To(BeTemporally(">", before.Add(21*time.Minute)))
		Expect(expiry).To(BeTemporally("<", before.Add(23*time.Minute)))
	})

	It("returns an error when the signing key seed is invalid", func() {
		_, err := auth.MintAdminToken(context.Background(), &fakeSessionRegistry{}, accountPub, "not-a-real-seed", "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(HaveOccurred())
	})
})

// BR-AS27 — the lab shell's own credential.
//
// The narrowest profile in this repo, and deliberately so. Federated plugin
// code executes inside the shell's JS realm, so this credential is not "the
// shell's" in any way a plugin is excluded from: whatever it can reach, every
// loaded plugin can reach. That is why the shell gets its own profile instead
// of reusing operator-refdata-platform, and why the assertion below is an
// exact ConsistOf rather than a check that some particular write subject is
// absent — the failure to catch is the subject nobody thought about, added a
// year from now by someone who saw a credential that already existed and
// looked convenient.
var _ = Describe("MintShellToken", func() {
	var accountPub, accountSigningSeed string

	BeforeEach(func() {
		accountKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		accountPub, err = accountKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		signingKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		seed, err := signingKP.Seed()
		Expect(err).NotTo(HaveOccurred())
		accountSigningSeed = string(seed)
	})

	It("mints a PLATFORM JWT that can read the registry and do nothing else", func() {
		info, err := auth.MintShellToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Tenant).To(Equal("platform"))
		Expect(info.JWT).NotTo(BeEmpty())
		Expect(info.NKeySeed).NotTo(BeEmpty())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		// Named for its holder, spelled as the process's nats.Name()
		// (CLAUDE.md, credential naming).
		Expect(claims.Name).To(Equal("lab-shell"))
		Expect(claims.IssuerAccount).To(Equal(accountPub))

		// One read subject and its reply inbox. Nothing else.
		Expect(claims.Permissions.Pub.Allow).To(ConsistOf(
			"api._platform.registry.frontend-plugins.read.v1",
			"_INBOX.>",
		))
		// One notification to hear, plus the inbox. The notify subject is
		// subscribe-only by construction: it is not in Pub.Allow, so a plugin
		// cannot forge a change notification to its own shell (BR-AS28 makes
		// the notification a hint, and this is what keeps the hint honest).
		Expect(claims.Permissions.Sub.Allow).To(ConsistOf(
			"notify._platform.registry.frontend-plugins.changed",
			"_INBOX.>",
		))
	})

	It("carries no curation subject — BR-AS27, and BR-AS21 from inside the browser", func() {
		info, err := auth.MintShellToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())

		// Stated separately from the ConsistOf above because these are the
		// claims that would still be true if someone "fixed" the exact match
		// by widening it. A plugin that could reach any of these would be
		// curating the registry that curates it.
		for _, forbidden := range []string{"upsert", "set-enabled", "entries", "audit"} {
			Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(ContainSubstring(forbidden)))
			Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring(forbidden)))
		}
		// No prefix grants of any kind: api.> would reach every other
		// service's surface, and rpc.> is never granted to a browser
		// credential at all (CLAUDE.md).
		Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(Equal("api.>")))
		Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(ContainSubstring("rpc.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("rpc.")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(Equal("notify.>")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$KV")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$JS.API")))
	})

	It("is not the admin credential wearing a different name", func() {
		// The reuse this profile exists to prevent, asserted as a difference
		// rather than as a list: if these two ever converge, one of them has
		// been widened into the other.
		shell, err := auth.MintShellToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		admin, err := auth.MintAdminToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		shellClaims, err := jwt.DecodeUserClaims(shell.JWT)
		Expect(err).NotTo(HaveOccurred())
		adminClaims, err := jwt.DecodeUserClaims(admin.JWT)
		Expect(err).NotTo(HaveOccurred())

		Expect(shellClaims.Name).NotTo(Equal(adminClaims.Name))
		for _, subject := range adminClaims.Permissions.Pub.Allow {
			if subject == "_INBOX.>" {
				continue
			}
			Expect(shellClaims.Permissions.Pub.Allow).NotTo(ContainElement(subject),
				"the shell must hold no subject the operator credential holds")
		}
	})

	It("stamps the expiry from the ttl argument", func() {
		info, err := auth.MintShellToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 22*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Expires - claims.IssuedAt).To(BeNumerically("~", (22 * time.Minute).Seconds(), 2))
	})

	It("returns an error when the signing key seed is invalid", func() {
		_, err := auth.MintShellToken(context.Background(), &fakeSessionRegistry{}, accountPub, "not-a-real-seed", "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("MintRefdataAdminToken", func() {
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

	// Phase 32: unlike MintAdminToken's three read-only subjects, this
	// credential drives refdata-service's full api.*.refdata.> business AND
	// admin endpoints for frontend/refdata's cross-tenant operator UI. The
	// grant is scoped to exactly api.*.refdata.>, never bare api.> — this
	// credential must not be able to reach any other service's api.*
	// surface (pricing, organizations, shipping), which a broader grant
	// would silently allow purely because it shares the PLATFORM account
	// with MintAdminToken's narrow read profile.
	It("mints a JWT scoped to api.*.refdata.> (pub+sub) and notify._platform.refdata.> (sub), with no broader api.> or notify.> grant", func() {
		info, err := auth.MintRefdataAdminToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 15*time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.WSUrl).To(Equal("ws://localhost:9222"))
		Expect(info.Tenant).To(Equal("platform"))
		Expect(info.JWT).NotTo(BeEmpty())
		Expect(info.NKeySeed).NotTo(BeEmpty())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Name).To(Equal("operator-app"))
		Expect(claims.IssuerAccount).To(Equal(accountPub))

		Expect(claims.Permissions.Pub.Allow).To(ConsistOf("api.*.refdata.>", "_INBOX.>"))
		Expect(claims.Permissions.Sub.Allow).To(ConsistOf("api.*.refdata.>", "notify._platform.refdata.>", "_INBOX.>"))

		// Must never be broadened to bare api.>/notify.> — that would reach
		// every other service's api.* surface and every tenant's notify.*
		// feed, not just refdata's.
		Expect(claims.Permissions.Pub.Allow).NotTo(ContainElement(Equal("api.>")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(Equal("notify.>")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$KV")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(ContainSubstring("$JS.API")))
		Expect(claims.Permissions.Sub.Allow).NotTo(ContainElement(Equal("rpc.>")))
	})

	It("stamps the expiry from the ttl argument", func() {
		before := time.Now()
		info, err := auth.MintRefdataAdminToken(context.Background(), &fakeSessionRegistry{}, accountPub, accountSigningSeed, "ws://localhost:9222", 22*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		claims, err := jwt.DecodeUserClaims(info.JWT)
		Expect(err).NotTo(HaveOccurred())
		expiry := time.Unix(claims.Expires, 0)
		Expect(expiry).To(BeTemporally(">", before.Add(21*time.Minute)))
		Expect(expiry).To(BeTemporally("<", before.Add(23*time.Minute)))
	})

	It("returns an error when the signing key seed is invalid", func() {
		_, err := auth.MintRefdataAdminToken(context.Background(), &fakeSessionRegistry{}, accountPub, "not-a-real-seed", "ws://localhost:9222", 15*time.Minute)
		Expect(err).To(HaveOccurred())
	})
})
