package accounts_test

// Phase 50a — the user registry (BR-AC38/BR-AC39). Integration coverage
// against the same disposable Postgres store_test.go spins up: nothing in
// the stack records a NATS user today (the resolver holds account JWTs
// only, and a user JWT is verified by signature and never stored
// server-side), so "list users" has to read a registry this service writes
// itself.

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

var _ = Describe("Store user registry", func() {
	var store *accounts.Store
	ctx := context.Background()

	BeforeEach(func() {
		if storeTestUnavailable != "" {
			Skip("docker unavailable for Postgres integration test: " + storeTestUnavailable)
		}
		store = accounts.NewStore(storeTestDB)
	})

	newUser := func(name string) accounts.NewUser {
		return accounts.NewUser{
			PublicKey:  "U" + name,
			Name:       name,
			Account:    "acme",
			AccountKey: "A" + name,
			Kind:       accounts.UserKindCredential,
			Source:     accounts.UserSourceService,
		}
	}

	Context("BR-AC38 — a user is recorded before it is signed", func() {
		It("records a pending row carrying the public NKey, then flips it to active", func() {
			name := uniqueAccountName("reg")
			Expect(store.RecordPendingUser(ctx, newUser(name))).To(Succeed())

			got, err := store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(accounts.UserStatusPending))
			Expect(got.Name).To(Equal(name))
			Expect(got.Kind).To(Equal(accounts.UserKindCredential))
			Expect(got.ActivatedAt).To(BeNil())

			Expect(store.MarkUserActive(ctx, "U"+name)).To(Succeed())
			got, err = store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(accounts.UserStatusActive))
			Expect(got.ActivatedAt).NotTo(BeNil())
		})

		It("presents a mint whose row never reached active as pending rather than hiding it", func() {
			name := uniqueAccountName("stuck")
			Expect(store.RecordPendingUser(ctx, newUser(name))).To(Succeed())

			all, err := store.ListUsers(ctx)
			Expect(err).NotTo(HaveOccurred())
			var found *accounts.User
			for i := range all {
				if all[i].PublicKey == "U"+name {
					found = &all[i]
				}
			}
			Expect(found).NotTo(BeNil(), "a pending row must still be listed — an unknown mint outcome is the thing an operator needs to see")
			Expect(found.Status).To(Equal(accounts.UserStatusPending))
		})

		It("reports ErrUserNotFound when marking a key that was never recorded", func() {
			Expect(store.MarkUserActive(ctx, "U"+uniqueAccountName("ghost"))).To(MatchError(accounts.ErrUserNotFound))
		})

		It("re-recording the same public key is rejected — an NKey identifies exactly one user", func() {
			name := uniqueAccountName("dupe")
			Expect(store.RecordPendingUser(ctx, newUser(name))).To(Succeed())
			Expect(store.RecordPendingUser(ctx, newUser(name))).NotTo(Succeed())
		})

		It("sweeps a pending session whose TTL has passed, and never sweeps a pending credential", func() {
			sessionName := uniqueAccountName("session")
			expired := time.Now().Add(-time.Minute)
			session := newUser(sessionName)
			session.Kind = accounts.UserKindSession
			session.ExpiresAt = &expired
			Expect(store.RecordPendingUser(ctx, session)).To(Succeed())

			credName := uniqueAccountName("cred")
			Expect(store.RecordPendingUser(ctx, newUser(credName))).To(Succeed())

			n, err := store.SweepExpiredPendingUsers(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(BeNumerically(">=", 1))

			_, err = store.GetUser(ctx, "U"+sessionName)
			Expect(err).To(MatchError(accounts.ErrUserNotFound))
			_, err = store.GetUser(ctx, "U"+credName)
			Expect(err).NotTo(HaveOccurred(), "a credential has no expiry to sweep on — it persists until an operator resolves it")
		})
	})

	Context("BR-AC39 — convergence on start for users minted outside this service", func() {
		It("InsertUserIfMissing writes once and never overwrites on a re-run", func() {
			name := uniqueAccountName("bootstrap")
			u := newUser(name)
			u.Source = accounts.UserSourceBootstrap
			u.Status = accounts.UserStatusActive
			Expect(store.InsertUserIfMissing(ctx, u)).To(Succeed())

			u.Name = "renamed-by-a-second-run"
			Expect(store.InsertUserIfMissing(ctx, u)).To(Succeed())

			got, err := store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal(name), "a converge pass must not rewrite a row it already wrote")
			Expect(got.Source).To(Equal(accounts.UserSourceBootstrap))
			Expect(got.Status).To(Equal(accounts.UserStatusActive))
		})
	})
})
