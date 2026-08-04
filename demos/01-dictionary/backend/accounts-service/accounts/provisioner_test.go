package accounts_test

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

var _ = Describe("Provisioner", func() {
	var (
		ots         *operatorTestServer
		provisioner *accounts.Provisioner
		ctx         context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		ots = newOperatorTestServer(GinkgoT())
		DeferCleanup(ots.Shutdown)

		nc := ots.ConnectSys(GinkgoT())
		DeferCleanup(nc.Close)

		var err error
		provisioner, err = accounts.NewProvisioner(ots.OperatorSigningKeySeed, nc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("mints an account whose creds can connect and get exactly its configured JetStream limits", func() {
		limits := accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}
		minted, err := provisioner.CreateAccount(ctx, limits, "tenant", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(minted.PublicKey).NotTo(BeEmpty())
		Expect(minted.SigningKeySeed).NotTo(BeEmpty())

		credsBytes, err := provisioner.CreateUser(minted.PublicKey, minted.SigningKeySeed, "tenant-user")
		Expect(err).NotTo(HaveOccurred())
		Expect(credsBytes).To(ContainSubstring("BEGIN NATS USER JWT"))

		nc, err := ots.ConnectWithCreds(credsBytes, "minted-account-test")
		Expect(err).NotTo(HaveOccurred())
		defer nc.Close()

		js, err := jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())

		tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = js.CreateStream(tctx, jetstream.StreamConfig{Name: "PROBE", Subjects: []string{"probe.>"}})
		Expect(err).NotTo(HaveOccurred(), "the minted account must get real JetStream access, not just a valid connection")

		accInfo, err := js.AccountInfo(tctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(accInfo.Limits.MaxMemory).To(BeEquivalentTo(limits.MaxMem))
		Expect(accInfo.Limits.MaxStore).To(BeEquivalentTo(limits.MaxFile))
		Expect(accInfo.Limits.MaxStreams).To(BeEquivalentTo(limits.MaxStreams))
		Expect(accInfo.Limits.MaxConsumers).To(BeEquivalentTo(limits.MaxConsumers))
	})

	It("isolates two minted accounts from each other exactly like the static Phase 13a accounts", func() {
		mintedA, err := provisioner.CreateAccount(ctx, accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "tenant-a", "")
		Expect(err).NotTo(HaveOccurred())
		credsA, err := provisioner.CreateUser(mintedA.PublicKey, mintedA.SigningKeySeed, "a")
		Expect(err).NotTo(HaveOccurred())

		mintedB, err := provisioner.CreateAccount(ctx, accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "tenant-b", "")
		Expect(err).NotTo(HaveOccurred())
		credsB, err := provisioner.CreateUser(mintedB.PublicKey, mintedB.SigningKeySeed, "b")
		Expect(err).NotTo(HaveOccurred())

		ncA, err := ots.ConnectWithCreds(credsA, "a")
		Expect(err).NotTo(HaveOccurred())
		defer ncA.Close()
		ncB, err := ots.ConnectWithCreds(credsB, "b")
		Expect(err).NotTo(HaveOccurred())
		defer ncB.Close()

		// Core pub/sub isolation between dynamically-minted accounts is
		// already proven structurally (separate accounts, no exports —
		// CreateAccount never grants any) and exhaustively covered for the
		// static Phase 13a accounts in
		// shipping-service/internal/natsaccounts/isolation_test.go. What's
		// specific to *this* test is JetStream: prove the same
		// identically-named-stream isolation holds for accounts minted at
		// runtime, not just accounts baked into nats.conf.
		jsA, err := jetstream.New(ncA)
		Expect(err).NotTo(HaveOccurred())
		jsB, err := jetstream.New(ncB)
		Expect(err).NotTo(HaveOccurred())

		tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = jsA.CreateStream(tctx, jetstream.StreamConfig{Name: "SAME_NAME", Subjects: []string{"same.>"}})
		Expect(err).NotTo(HaveOccurred())
		_, err = jsB.CreateStream(tctx, jetstream.StreamConfig{Name: "SAME_NAME", Subjects: []string{"same.>"}})
		Expect(err).NotTo(HaveOccurred(), "an identically-named stream in a different minted account must not collide")

		_, err = jsA.Publish(tctx, "same.marker", []byte("a-only"))
		Expect(err).NotTo(HaveOccurred())

		streamB, err := jsB.Stream(tctx, "SAME_NAME")
		Expect(err).NotTo(HaveOccurred())
		infoB, err := streamB.Info(tctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(infoB.State.Msgs).To(BeZero(), "account B's identically-named stream must not see account A's message")
	})

	It("revokes an account so a subsequent connection attempt is rejected", func() {
		minted, err := provisioner.CreateAccount(ctx, accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}, "tenant", "")
		Expect(err).NotTo(HaveOccurred())
		credsBytes, err := provisioner.CreateUser(minted.PublicKey, minted.SigningKeySeed, "tenant-user")
		Expect(err).NotTo(HaveOccurred())

		nc, err := ots.ConnectWithCreds(credsBytes, "pre-revoke")
		Expect(err).NotTo(HaveOccurred())
		nc.Close()

		Expect(provisioner.DeleteAccount(ctx, minted.PublicKey)).To(Succeed())

		_, err = ots.ConnectWithCreds(credsBytes, "post-revoke")
		Expect(err).To(HaveOccurred(), "a connection attempt under a revoked account must be rejected")
	})

	It("reactivates a revoked account under its original public key and limits, restoring connectivity", func() {
		limits := accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}
		minted, err := provisioner.CreateAccount(ctx, limits, "tenant", "")
		Expect(err).NotTo(HaveOccurred())

		Expect(provisioner.DeleteAccount(ctx, minted.PublicKey)).To(Succeed())

		Expect(provisioner.ReactivateAccount(ctx, minted.PublicKey, minted.SigningKeySeed, limits, accounts.CrossAccountOpts{}, nil)).To(Succeed())

		credsBytes, err := provisioner.CreateUser(minted.PublicKey, minted.SigningKeySeed, "tenant-user")
		Expect(err).NotTo(HaveOccurred(), "the account's own signing key must still be valid post-reactivation, since the public key was reused")

		nc, err := ots.ConnectWithCreds(credsBytes, "post-reactivate")
		Expect(err).NotTo(HaveOccurred(), "a reactivated account must accept new connections again")
		defer nc.Close()

		js, err := jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())
		tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		accInfo, err := js.AccountInfo(tctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(accInfo.Limits.MaxMemory).To(BeEquivalentTo(limits.MaxMem), "reactivation must restore the account's original JetStream limits, not defaults")
	})
})
