package accounts_test

// Phase 50a, BR-AC38 — CreateUser records the user before it signs it.

import (
	"context"

	"github.com/nats-io/jwt/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

var _ = Describe("Provisioner.CreateUser registry recording (BR-AC38)", func() {
	var (
		ots      *operatorTestServer
		registry *fakeUserRegistry
		ctx      context.Context
		minted   accounts.MintedAccount
		newProv  func() *accounts.Provisioner
	)

	BeforeEach(func() {
		ctx = context.Background()
		ots = newOperatorTestServer(GinkgoT())
		DeferCleanup(ots.Shutdown)
		nc := ots.ConnectSys(GinkgoT())
		DeferCleanup(nc.Close)

		registry = &fakeUserRegistry{}
		newProv = func() *accounts.Provisioner {
			p, err := accounts.NewProvisioner(ots.OperatorSigningKeySeed, nc, registry)
			Expect(err).NotTo(HaveOccurred())
			return p
		}

		var err error
		minted, err = newProv().CreateAccount(ctx, accounts.JSLimits{MaxMem: 1 << 20, MaxFile: 1 << 20, MaxStreams: 1, MaxConsumers: 1}, "acme", "")
		Expect(err).NotTo(HaveOccurred())
	})

	req := func(m accounts.MintedAccount) accounts.NewUserRequest {
		return accounts.NewUserRequest{
			AccountName:    "acme",
			AccountPub:     m.PublicKey,
			SigningKeySeed: m.SigningKeySeed,
			UserName:       "acme",
		}
	}

	It("records a pending row for the credential's own NKey, then marks it active", func() {
		credsBytes, err := newProv().CreateUser(ctx, req(minted))
		Expect(err).NotTo(HaveOccurred())

		token, err := jwt.ParseDecoratedJWT(credsBytes)
		Expect(err).NotTo(HaveOccurred())
		claims, err := jwt.DecodeUserClaims(token)
		Expect(err).NotTo(HaveOccurred())

		calls, recorded := registry.snapshot()
		Expect(calls).To(Equal([]string{"record:" + claims.Subject, "active:" + claims.Subject}),
			"the pending row must be written before the JWT is signed, and flipped only after")
		Expect(recorded).To(HaveLen(1))
		Expect(recorded[0].Name).To(Equal("acme"))
		Expect(recorded[0].Account).To(Equal("acme"))
		Expect(recorded[0].AccountKey).To(Equal(minted.PublicKey))
		Expect(recorded[0].Kind).To(Equal(accounts.UserKindCredential))
		Expect(recorded[0].Source).To(Equal(accounts.UserSourceService))
		Expect(recorded[0].ExpiresAt).To(BeNil(), "a service credential has no expiry — that is what distinguishes it from a session")
	})

	It("returns no credential at all when the pending row cannot be written", func() {
		registry.recordErr = errRegistryDown
		credsBytes, err := newProv().CreateUser(ctx, req(minted))
		Expect(err).To(MatchError(errRegistryDown))
		Expect(credsBytes).To(BeEmpty(), "an unrecorded credential is one nothing can ever account for")
	})

	It("records nothing when the signing key is unusable — the failure precedes key generation", func() {
		in := req(minted)
		in.SigningKeySeed = "not-a-real-seed"
		_, err := newProv().CreateUser(ctx, in)
		Expect(err).To(HaveOccurred())

		calls, _ := registry.snapshot()
		Expect(calls).To(BeEmpty())
	})

	It("reports a failure to activate, leaving the row pending — the outcome of that mint is genuinely unknown", func() {
		registry.activateErr = errRegistryDown
		_, err := newProv().CreateUser(ctx, req(minted))
		Expect(err).To(MatchError(errRegistryDown))

		calls, _ := registry.snapshot()
		Expect(calls).To(HaveLen(1))
		Expect(calls[0]).To(HavePrefix("record:"))
	})

	It("refuses to mint at all without a registry", func() {
		p, err := accounts.NewProvisioner(ots.OperatorSigningKeySeed, ots.ConnectSys(GinkgoT()), nil)
		Expect(err).To(MatchError(accounts.ErrNoUserRegistry))
		Expect(p).To(BeNil())
	})
})
