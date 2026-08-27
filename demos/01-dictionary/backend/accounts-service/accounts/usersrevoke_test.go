package accounts_test

// BR-AC42 / BR-AC43 — credential revocation.
//
// Against fakes rather than an embedded server: the mechanism (re-signing an
// account JWT with the operator key and pushing $SYS.REQ.CLAIMS.UPDATE) needs
// an operator, and what has rules attached is the policy around it — which
// users may be revoked, in what order the two writes happen, and what an
// operator is told when only one of them lands.

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// fakeNATSRevoker stands in for Provisioner.RevokeUser — the account-JWT
// amendment and its push to the server.
type fakeNATSRevoker struct {
	err      error
	accounts []string
	users    []string
}

func (f *fakeNATSRevoker) RevokeUser(_ context.Context, accountPub, userPub string) error {
	if f.err != nil {
		return f.err
	}
	f.accounts = append(f.accounts, accountPub)
	f.users = append(f.users, userPub)
	return nil
}

var _ = Describe("Credential revocation", func() {
	var (
		store   *fakeUserReadStore
		nats    *fakeNATSRevoker
		revoker *accounts.UserRevoker
		userPub string
	)

	BeforeEach(func() {
		userPub = mustUserKey()
		accountPub, _ := mustAccountKey()
		store = &fakeUserReadStore{users: []accounts.User{{
			PublicKey:  userPub,
			Name:       "acme",
			Account:    "acme",
			AccountKey: accountPub,
			Kind:       accounts.UserKindCredential,
			Status:     accounts.UserStatusActive,
			Source:     accounts.UserSourceBootstrap,
			IssuedAt:   time.Now(),
		}}}
		nats = &fakeNATSRevoker{}
		revoker = accounts.NewUserRevoker(store, nats, nil)
	})

	Context("BR-AC43 — only a long-lived credential can be revoked", func() {
		It("revokes a credential, writing the user key into its own account's JWT", func() {
			out, err := revoker.Revoke(context.Background(), userPub)
			Expect(err).NotTo(HaveOccurred())
			Expect(out.PublicKey).To(Equal(userPub))
			Expect(nats.users).To(ConsistOf(userPub))
			Expect(nats.accounts).To(ConsistOf(store.users[0].AccountKey))
		})

		It("refuses a session, which expires on its own TTL", func() {
			store.users[0].Kind = accounts.UserKindSession
			_, err := revoker.Revoke(context.Background(), userPub)
			Expect(err).To(MatchError(accounts.ErrRevokeNotCredential))
			// Nothing was pushed — a refused revocation must not re-sign and
			// redistribute an account JWT for no reason.
			Expect(nats.users).To(BeEmpty())
			Expect(store.marked).To(BeEmpty())
		})

		It("refuses a row whose issuing account was never recorded", func() {
			store.users[0].AccountKey = ""
			_, err := revoker.Revoke(context.Background(), userPub)
			Expect(err).To(MatchError(accounts.ErrRevokeAccountUnknown))
			Expect(nats.users).To(BeEmpty())
		})

		It("requires a public key — a name is not unique enough to revoke by", func() {
			_, err := revoker.Revoke(context.Background(), "")
			Expect(err).To(MatchError(accounts.ErrPublicKeyRequired))
		})

		It("reports an unknown user as not found", func() {
			_, err := revoker.Revoke(context.Background(), mustUserKey())
			Expect(err).To(MatchError(accounts.ErrUserNotFound))
		})
	})

	Context("BR-AC43 — revocation is terminal and not repeatable", func() {
		It("refuses a second revocation without re-pushing the account JWT", func() {
			_, err := revoker.Revoke(context.Background(), userPub)
			Expect(err).NotTo(HaveOccurred())

			_, err = revoker.Revoke(context.Background(), userPub)
			Expect(err).To(MatchError(accounts.ErrUserAlreadyRevoked))
			Expect(nats.users).To(HaveLen(1))
		})

		It("exposes no way back — the registry keeps one revocation timestamp", func() {
			before, err := revoker.Revoke(context.Background(), userPub)
			Expect(err).NotTo(HaveOccurred())
			Expect(before.RevokedAt).NotTo(BeEmpty())

			// Recovery from a mis-revocation is minting a REPLACEMENT
			// credential, so the stamped row must not be reachable back to an
			// unrevoked state through this path.
			Expect(store.users[0].RevokedAt).NotTo(BeNil())
			_, err = revoker.Revoke(context.Background(), userPub)
			Expect(err).To(HaveOccurred())
			Expect(store.users[0].RevokedAt).NotTo(BeNil())
		})
	})

	Context("BR-AC42 — the registry mirrors the JWT, and never leads it", func() {
		It("stamps the registry row only after the server has accepted the push", func() {
			nats.err = errors.New("claims update timed out")
			_, err := revoker.Revoke(context.Background(), userPub)
			Expect(err).To(MatchError(ContainSubstring("claims update timed out")))
			// The credential still works, so the row must not claim otherwise.
			Expect(store.marked).To(BeEmpty())
			Expect(store.users[0].RevokedAt).To(BeNil())
		})

		It("says which half succeeded when the push lands but the row does not", func() {
			store.markErr = errors.New("connection refused")
			_, err := revoker.Revoke(context.Background(), userPub)
			Expect(err).To(HaveOccurred())
			// The operator must be able to tell "the credential is dead and
			// the registry lags" from "nothing happened" — the remedy for the
			// first is to reconcile, not to revoke again.
			Expect(err.Error()).To(ContainSubstring("revoked on the server"))
			Expect(nats.users).To(ConsistOf(userPub))
		})

		It("refuses outright when no NATS revocation path is configured", func() {
			offline := accounts.NewUserRevoker(store, nil, nil)
			_, err := offline.Revoke(context.Background(), userPub)
			Expect(err).To(HaveOccurred())
			Expect(store.marked).To(BeEmpty())
		})
	})
})
