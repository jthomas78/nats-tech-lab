package accounts_test

import (
	"context"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
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

	// BR-AC30 (Phase 28f amendment): CreateAccount must re-sign PLATFORM's
	// own claims to import each new tenant's obs.trace.> export — the one
	// leg of the cross-account trace pipeline that touches an account
	// CreateAccount didn't itself just mint.
	It("re-signs PLATFORM's own claims to import each new tenant's obs.trace.>, without disturbing other tenants' imports or duplicating on retry", func() {
		platformKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		platformPub, err := platformKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		sysNC := ots.ConnectSys(GinkgoT())
		defer sysNC.Close()
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, jwt.NewAccountClaims(platformPub))

		limits := accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}

		mintedA, err := provisioner.CreateAccount(ctx, limits, "acme", platformPub)
		Expect(err).NotTo(HaveOccurred())

		// Filtered to obs.trace.> imports specifically — CreateAccount also
		// mints a $SRV.> Service import per tenant now (BR-AC31), so a raw
		// Imports-length assertion would couple this test to that unrelated
		// rule; see the "$SRV.> service discovery" spec below for that one.
		traceImportsFor := func(claims *jwt.AccountClaims) []*jwt.Import {
			var out []*jwt.Import
			for _, imp := range claims.Imports {
				if imp.Type == jwt.Stream && string(imp.Subject) == "obs.trace.>" {
					out = append(out, imp)
				}
			}
			return out
		}

		afterFirst, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		firstTrace := traceImportsFor(afterFirst)
		Expect(firstTrace).To(HaveLen(1))
		Expect(firstTrace[0].Account).To(Equal(mintedA.PublicKey))
		Expect(firstTrace[0].AllowTrace).To(BeTrue(), "AllowTrace must be set on PLATFORM's Stream import — that's the flag that actually enables trace propagation")

		mintedB, err := provisioner.CreateAccount(ctx, limits, "globex", platformPub)
		Expect(err).NotTo(HaveOccurred())

		afterSecond, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		secondTrace := traceImportsFor(afterSecond)
		Expect(secondTrace).To(HaveLen(2), "a second tenant's import must be added alongside the first, not replace it")
		accountsImported := map[string]bool{}
		for _, imp := range secondTrace {
			accountsImported[imp.Account] = true
		}
		Expect(accountsImported).To(HaveKey(mintedA.PublicKey))
		Expect(accountsImported).To(HaveKey(mintedB.PublicKey))

		// A repeated call for a tenant PLATFORM already imports (simulating
		// a caller retry after some unrelated transient failure) must be a
		// no-op, not append a duplicate entry.
		Expect(provisioner.AddPlatformTraceImportForTest(ctx, platformPub, mintedA.PublicKey, "acme")).To(Succeed())
		afterRetry, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		Expect(traceImportsFor(afterRetry)).To(HaveLen(2), "re-registering an already-imported tenant must not duplicate the import")
	})

	// BR-AC36 (Phase 48a): the obs.trace.> import above gains the same
	// per-tenant LocalSubject remap BR-AC34 gave obs.pubsub.>. BR-AC34
	// deliberately left the trace import alone — the Traces panel showed
	// only a coarse PLATFORM/TENANT split, so a remap would have changed a
	// shipped pipeline for no gain. Once the panel names the tenant
	// (BR-054), the argument that forced the pubsub remap applies here
	// unchanged: every tenant exports the identical literal "obs.trace.>",
	// so the local subject is the only thing on the wire that says which
	// account a span came from.
	It("re-signs PLATFORM's own claims to import each new tenant's obs.trace.> export under a tenant-scoped local subject", func() {
		platformKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		platformPub, err := platformKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		sysNC := ots.ConnectSys(GinkgoT())
		defer sysNC.Close()
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, jwt.NewAccountClaims(platformPub))

		limits := accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}

		mintedA, err := provisioner.CreateAccount(ctx, limits, "acme", platformPub)
		Expect(err).NotTo(HaveOccurred())
		mintedB, err := provisioner.CreateAccount(ctx, limits, "globex", platformPub)
		Expect(err).NotTo(HaveOccurred())

		claims, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		byAccount := map[string]*jwt.Import{}
		for _, imp := range claims.Imports {
			if string(imp.Subject) == "obs.trace.>" {
				byAccount[imp.Account] = imp
			}
		}
		Expect(byAccount).To(HaveLen(2), "a second tenant's obs.trace.> import must be added alongside the first, not replace it")
		Expect(byAccount[mintedA.PublicKey].Type).To(Equal(jwt.Stream))
		Expect(byAccount[mintedA.PublicKey].AllowTrace).To(BeTrue(), "the remap must not cost the AllowTrace flag the import already carried")
		Expect(string(byAccount[mintedA.PublicKey].LocalSubject)).To(Equal("monitor.acme.trace.>"),
			"without the remap both tenants land on one identical local subject and the panel cannot name the origin account")
		Expect(string(byAccount[mintedB.PublicKey].LocalSubject)).To(Equal("monitor.globex.trace.>"))

		// Idempotent on retry, same contract as the pubsub and $SRV.> imports.
		Expect(provisioner.AddPlatformTraceImportForTest(ctx, platformPub, mintedA.PublicKey, "acme")).To(Succeed())
		afterRetry, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		retryCount := 0
		for _, imp := range afterRetry.Imports {
			if string(imp.Subject) == "obs.trace.>" {
				retryCount++
			}
		}
		Expect(retryCount).To(Equal(2), "re-registering an already-imported tenant must not duplicate the import")
	})

	// BR-AC36's rollout clause — "already-minted accounts keep the
	// un-remapped import until they are re-provisioned" — is only true if
	// re-provisioning actually changes something. An idempotency scan that
	// matches on (Account, Subject) alone, which is what every other import
	// in this file does, would report success and leave a pre-BR-AC36
	// account's un-remapped import exactly as it found it. That failure is
	// silent in the worst way: provisioning succeeds, the panel keeps
	// attributing every tenant's spans to one bucket, and the only remaining
	// fix is a full wipe and reseed.
	It("corrects an obs.trace.> import that already exists without the remap, rather than reporting a no-op success", func() {
		platformKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		platformPub, err := platformKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		sysNC := ots.ConnectSys(GinkgoT())
		defer sysNC.Close()

		tenantKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		tenantPub, err := tenantKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		// Stand up PLATFORM exactly as a pre-BR-AC36 deployment left it: the
		// trace import present, correct in every respect except the remap.
		legacy := jwt.NewAccountClaims(platformPub)
		legacy.Imports.Add(&jwt.Import{
			Account:    tenantPub,
			Subject:    jwt.Subject("obs.trace.>"),
			Type:       jwt.Stream,
			AllowTrace: true,
		})
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, legacy)

		Expect(provisioner.AddPlatformTraceImportForTest(ctx, platformPub, tenantPub, "acme")).To(Succeed())

		after, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		var traceImports []*jwt.Import
		for _, imp := range after.Imports {
			if string(imp.Subject) == "obs.trace.>" {
				traceImports = append(traceImports, imp)
			}
		}
		Expect(traceImports).To(HaveLen(1), "correcting the remap must update the existing import in place, not add a second one")
		Expect(string(traceImports[0].LocalSubject)).To(Equal("monitor.acme.trace.>"))
		Expect(traceImports[0].AllowTrace).To(BeTrue(), "correcting the remap must not drop the flag the legacy import carried")
	})

	// BR-AC31 (Phase 30a): CreateAccount must also re-sign PLATFORM's own
	// claims to import each new tenant's $SRV.> service export — the
	// cross-account service-discovery counterpart to BR-AC30's trace import
	// above, minted the same way (per-tenant claims update, since NATS has
	// no wildcard cross-account import).
	It("re-signs PLATFORM's own claims to import each new tenant's $SRV.> service discovery, without disturbing other tenants' imports or duplicating on retry", func() {
		platformKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		platformPub, err := platformKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		sysNC := ots.ConnectSys(GinkgoT())
		defer sysNC.Close()
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, jwt.NewAccountClaims(platformPub))

		limits := accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}

		mintedA, err := provisioner.CreateAccount(ctx, limits, "acme", platformPub)
		Expect(err).NotTo(HaveOccurred())

		afterFirst, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		var srvImport *jwt.Import
		for _, imp := range afterFirst.Imports {
			if imp.Type == jwt.Service && imp.Account == mintedA.PublicKey {
				srvImport = imp
			}
		}
		Expect(srvImport).NotTo(BeNil(), "expected a Service import for acme's $SRV.> export alongside the trace import")
		Expect(string(srvImport.Subject)).To(Equal("$SRV.>"))
		Expect(string(srvImport.LocalSubject)).To(Equal("monitor.acme.srv.>"), "must remap to a tenant-scoped local subject so a second tenant's import can't collide with this one")

		mintedB, err := provisioner.CreateAccount(ctx, limits, "globex", platformPub)
		Expect(err).NotTo(HaveOccurred())

		afterSecond, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		srvImportsByAccount := map[string]*jwt.Import{}
		for _, imp := range afterSecond.Imports {
			if imp.Type == jwt.Service && string(imp.Subject) == "$SRV.>" {
				srvImportsByAccount[imp.Account] = imp
			}
		}
		Expect(srvImportsByAccount).To(HaveLen(2), "a second tenant's $SRV.> import must be added alongside the first, not replace it")
		Expect(srvImportsByAccount).To(HaveKey(mintedA.PublicKey))
		Expect(srvImportsByAccount).To(HaveKey(mintedB.PublicKey))
		Expect(string(srvImportsByAccount[mintedB.PublicKey].LocalSubject)).To(Equal("monitor.globex.srv.>"))

		// A repeated call for a tenant PLATFORM already imports (simulating a
		// caller retry after some unrelated transient failure) must be a
		// no-op, not append a duplicate entry.
		Expect(provisioner.AddPlatformMonitorImportForTest(ctx, platformPub, mintedA.PublicKey, "acme")).To(Succeed())
		afterRetry, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		retryCount := 0
		for _, imp := range afterRetry.Imports {
			if imp.Type == jwt.Service && string(imp.Subject) == "$SRV.>" {
				retryCount++
			}
		}
		Expect(retryCount).To(Equal(2), "re-registering an already-imported tenant must not duplicate the import")
	})

	// BR-AC34 (Phase 43a): CreateAccount must also re-sign PLATFORM's own
	// claims to import each new tenant's obs.pubsub.> export — BR-AC30's
	// trace import with one deliberate difference, a per-tenant LocalSubject
	// remap (ADR-047 amendment A1). The trace import needs none because the
	// Traces panel only ever shows a coarse PLATFORM/TENANT split; the
	// Messages panel names the tenant, and the only thing on the wire that
	// can tell it which tenant published a message is the local subject it
	// arrived on.
	It("re-signs PLATFORM's own claims to import each new tenant's obs.pubsub.> export under a tenant-scoped local subject", func() {
		platformKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		platformPub, err := platformKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		sysNC := ots.ConnectSys(GinkgoT())
		defer sysNC.Close()
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, jwt.NewAccountClaims(platformPub))

		limits := accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}

		mintedA, err := provisioner.CreateAccount(ctx, limits, "acme", platformPub)
		Expect(err).NotTo(HaveOccurred())
		mintedB, err := provisioner.CreateAccount(ctx, limits, "globex", platformPub)
		Expect(err).NotTo(HaveOccurred())

		claims, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		byAccount := map[string]*jwt.Import{}
		for _, imp := range claims.Imports {
			if string(imp.Subject) == "obs.pubsub.>" {
				byAccount[imp.Account] = imp
			}
		}
		Expect(byAccount).To(HaveLen(2), "a second tenant's obs.pubsub.> import must be added alongside the first, not replace it")
		Expect(byAccount[mintedA.PublicKey].Type).To(Equal(jwt.Stream))
		Expect(string(byAccount[mintedA.PublicKey].LocalSubject)).To(Equal("monitor.acme.pubsub.>"),
			"without the remap both tenants land on one identical local subject and the panel cannot name the publisher")
		Expect(string(byAccount[mintedB.PublicKey].LocalSubject)).To(Equal("monitor.globex.pubsub.>"))

		// Idempotent on retry, same contract as the trace and $SRV.> imports.
		Expect(provisioner.AddPlatformPubsubImportForTest(ctx, platformPub, mintedA.PublicKey, "acme")).To(Succeed())
		afterRetry, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		retryCount := 0
		for _, imp := range afterRetry.Imports {
			if string(imp.Subject) == "obs.pubsub.>" {
				retryCount++
			}
		}
		Expect(retryCount).To(Equal(2), "re-registering an already-imported tenant must not duplicate the import")
	})

	// BR-AC32 (Phase 30b/30i): CreateAccount must also re-sign PLATFORM's
	// own claims to import each new tenant's seven $JS.API introspection
	// exports — the JetStream/KV-introspection counterpart to BR-AC30's
	// trace import and BR-AC31's $SRV.> import above, minted the same way.
	It("re-signs PLATFORM's own claims to import each new tenant's $JS.API introspection subjects, without disturbing other tenants' imports or duplicating on retry", func() {
		wantSubjects := []string{
			"$JS.API.STREAM.LIST",
			"$JS.API.STREAM.INFO.*",
			"$JS.API.CONSUMER.CREATE.*",
			"$JS.API.CONSUMER.CREATE.*.*",
			"$JS.API.CONSUMER.CREATE.*.*.>",
			"$JS.API.CONSUMER.MSG.NEXT.*.*",
			"$JS.API.CONSUMER.DELETE.*.*",
		}
		wantLocal := func(tenantName, subject string) string {
			return "monitor." + tenantName + ".js." + subject[len("$JS.API."):]
		}

		platformKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		platformPub, err := platformKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())

		sysNC := ots.ConnectSys(GinkgoT())
		defer sysNC.Close()
		ots.PushAccountClaims(sysNC, ots.OperatorSigningKeySeed, jwt.NewAccountClaims(platformPub))

		limits := accounts.JSLimits{MaxMem: 64 << 20, MaxFile: 128 << 20, MaxStreams: 3, MaxConsumers: 5}

		jsAPIImportsFor := func(claims *jwt.AccountClaims, account string) map[string]*jwt.Import {
			out := map[string]*jwt.Import{}
			for _, imp := range claims.Imports {
				if imp.Type == jwt.Service && imp.Account == account {
					for _, want := range wantSubjects {
						if string(imp.Subject) == want {
							out[want] = imp
						}
					}
				}
			}
			return out
		}

		mintedA, err := provisioner.CreateAccount(ctx, limits, "acme", platformPub)
		Expect(err).NotTo(HaveOccurred())

		afterFirst, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		importsA := jsAPIImportsFor(afterFirst, mintedA.PublicKey)
		Expect(importsA).To(HaveLen(len(wantSubjects)), "expected all seven $JS.API imports for acme alongside the trace/$SRV imports")
		for _, subject := range wantSubjects {
			imp, ok := importsA[subject]
			Expect(ok).To(BeTrue(), "missing import for %s", subject)
			Expect(string(imp.LocalSubject)).To(Equal(wantLocal("acme", subject)))
		}

		mintedB, err := provisioner.CreateAccount(ctx, limits, "globex", platformPub)
		Expect(err).NotTo(HaveOccurred())

		afterSecond, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		importsA2 := jsAPIImportsFor(afterSecond, mintedA.PublicKey)
		importsB := jsAPIImportsFor(afterSecond, mintedB.PublicKey)
		Expect(importsA2).To(HaveLen(len(wantSubjects)), "acme's imports must survive globex's own CreateAccount call")
		Expect(importsB).To(HaveLen(len(wantSubjects)), "a second tenant's imports must be added alongside the first, not replace it")
		Expect(string(importsB["$JS.API.STREAM.LIST"].LocalSubject)).To(Equal("monitor.globex.js.STREAM.LIST"))

		// A repeated call for a tenant PLATFORM already imports (simulating a
		// caller retry after some unrelated transient failure) must be a
		// no-op, not append duplicate entries.
		Expect(provisioner.AddPlatformJSAPIImportForTest(ctx, platformPub, mintedA.PublicKey, "acme")).To(Succeed())
		afterRetry, err := provisioner.LookupAccountClaims(ctx, platformPub)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsAPIImportsFor(afterRetry, mintedA.PublicKey)).To(HaveLen(len(wantSubjects)), "re-registering an already-imported tenant must not duplicate any of the seven imports")
	})
})
