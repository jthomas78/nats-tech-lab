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

			// Phase 52 folded the pending sweep into ReapExpiredSessions.
			// A pending row is still reaped the instant its TTL passes,
			// with no retention grace: the retention window exists to keep
			// an EXPLAINABLE row around, and a mint that never produced a
			// credential has nothing to explain.
			n, err := store.ReapExpiredSessions(ctx, 24*time.Hour)
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

	// Phase 51b (BR-AC42) — the registry's mirror of the account JWT's own
	// revocation list. The JWT is the authority; these specs pin the two
	// things the column has to get right on its own.
	Context("BR-AC42 — the registry mirrors a revocation", func() {
		It("stamps revoked_at, leaving status alone so the row keeps its history", func() {
			name := uniqueAccountName("revoke")
			Expect(store.RecordPendingUser(ctx, newUser(name))).To(Succeed())
			Expect(store.MarkUserActive(ctx, "U"+name)).To(Succeed())

			Expect(store.MarkUserRevoked(ctx, "U"+name)).To(Succeed())

			got, err := store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.RevokedAt).NotTo(BeNil())
			// A credential that was active and is now revoked is not the
			// same thing as one whose mint never finished, so status must
			// still say which of the two this was.
			Expect(got.Status).To(Equal(accounts.UserStatusActive))
		})

		It("refuses a second revocation rather than moving the timestamp", func() {
			name := uniqueAccountName("twice")
			Expect(store.RecordPendingUser(ctx, newUser(name))).To(Succeed())
			Expect(store.MarkUserRevoked(ctx, "U"+name)).To(Succeed())

			first, err := store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.MarkUserRevoked(ctx, "U"+name)).To(MatchError(accounts.ErrUserAlreadyRevoked))

			after, err := store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.RevokedAt.Equal(*first.RevokedAt)).To(BeTrue())
		})

		It("reports an unknown key as not found, not as already revoked", func() {
			Expect(store.MarkUserRevoked(ctx, "Unosuchuser")).To(MatchError(accounts.ErrUserNotFound))
		})
	})

	Context("BR-AC44 — an expired session is reaped once it is older than the retention window", func() {
		// activeSession records a session that reached active and expired
		// `ago` in the past, and hands back its public key.
		activeSession := func(label string, ago time.Duration) string {
			name := uniqueAccountName(label)
			expired := time.Now().Add(-ago)
			u := newUser(name)
			u.Kind = accounts.UserKindSession
			u.ExpiresAt = &expired
			Expect(store.RecordPendingUser(ctx, u)).To(Succeed())
			Expect(store.MarkUserActive(ctx, "U"+name)).To(Succeed())
			return "U" + name
		}

		It("keeps a session that expired inside the window and removes one that expired outside it", func() {
			fresh := activeSession("fresh", time.Hour)
			stale := activeSession("stale", 30*time.Hour)

			n, err := store.ReapExpiredSessions(ctx, 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(BeNumerically(">=", 1))

			_, err = store.GetUser(ctx, fresh)
			Expect(err).NotTo(HaveOccurred(),
				"a session that expired an hour ago is exactly the row someone is reading when they ask why their tab dropped")
			_, err = store.GetUser(ctx, stale)
			Expect(err).To(MatchError(accounts.ErrUserNotFound))
		})

		It("never reaps a credential, even one wrongly carrying a long-past expiry", func() {
			name := uniqueAccountName("credexp")
			expired := time.Now().Add(-30 * 24 * time.Hour)
			u := newUser(name) // kind: credential
			u.ExpiresAt = &expired
			Expect(store.RecordPendingUser(ctx, u)).To(Succeed())
			Expect(store.MarkUserActive(ctx, "U"+name)).To(Succeed())

			_, err := store.ReapExpiredSessions(ctx, 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())

			_, err = store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred(),
				"the kind clause is belt-and-braces: a bug that stamps an expiry on a credential must cost a wrong badge, not a deleted credential")
		})

		It("never reaps a session with no expiry at all", func() {
			name := uniqueAccountName("noexp")
			u := newUser(name)
			u.Kind = accounts.UserKindSession
			Expect(store.RecordPendingUser(ctx, u)).To(Succeed())
			Expect(store.MarkUserActive(ctx, "U"+name)).To(Succeed())

			_, err := store.ReapExpiredSessions(ctx, 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred())
		})

		It("never reaps a revoked session, at any age", func() {
			key := activeSession("revoked", 90*24*time.Hour)
			Expect(store.MarkUserRevoked(ctx, key)).To(Succeed())

			_, err := store.ReapExpiredSessions(ctx, 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.GetUser(ctx, key)
			Expect(err).NotTo(HaveOccurred(),
				"the account JWT keeps the NKey in its revocation list forever — reaping the row leaves an operator staring at a revocation nothing can name")
			Expect(got.RevokedAt).NotTo(BeNil())
		})

		It("reports how many rows it removed", func() {
			a := activeSession("counted-a", 30*time.Hour)
			b := activeSession("counted-b", 30*time.Hour)

			n, err := store.ReapExpiredSessions(ctx, 24*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(BeNumerically(">=", 2))

			for _, key := range []string{a, b} {
				_, err := store.GetUser(ctx, key)
				Expect(err).To(MatchError(accounts.ErrUserNotFound))
			}
		})

		It("a zero retention still refuses to reap an unexpired session", func() {
			name := uniqueAccountName("future")
			future := time.Now().Add(time.Hour)
			u := newUser(name)
			u.Kind = accounts.UserKindSession
			u.ExpiresAt = &future
			Expect(store.RecordPendingUser(ctx, u)).To(Succeed())
			Expect(store.MarkUserActive(ctx, "U"+name)).To(Succeed())

			_, err := store.ReapExpiredSessions(ctx, 0)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.GetUser(ctx, "U"+name)
			Expect(err).NotTo(HaveOccurred(), "the predicate is expires_at in the PAST, not merely older than now-retention")
		})
	})
})
