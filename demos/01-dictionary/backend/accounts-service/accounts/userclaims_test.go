package accounts_test

// BR-AC41 — .get.v1 returns EFFECTIVE permissions.
//
// The rule exists because a user JWT's own grants are not necessarily what
// the server enforces. When the account signed the user with a SCOPED
// signing key, the server applies that key's template and discards
// everything in the user's own claims. A claims table rendering the JWT
// verbatim would therefore show an operator access the user does not have —
// and, worse, would make a credential minted with the wrong permissions look
// identical to a correct one. So: effective first, the JWT's discarded
// grants returned separately and explicitly flagged as not enforced.

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// fakeUserReadStore is the read side of the registry, in memory.
type fakeUserReadStore struct {
	users  []accounts.User
	getErr error
}

func (f *fakeUserReadStore) ListUsers(_ context.Context) ([]accounts.User, error) {
	return f.users, nil
}

func (f *fakeUserReadStore) GetUser(_ context.Context, publicKey string) (accounts.User, error) {
	if f.getErr != nil {
		return accounts.User{}, f.getErr
	}
	for _, u := range f.users {
		if u.PublicKey == publicKey {
			return u, nil
		}
	}
	return accounts.User{}, accounts.ErrUserNotFound
}

// fakeAccountLookup stands in for $SYS.REQ.ACCOUNT.<pub>.CLAIMS.LOOKUP.
type fakeAccountLookup struct {
	claims map[string]*jwt.AccountClaims
	err    error
}

func (f *fakeAccountLookup) LookupAccountClaims(_ context.Context, accountPub string) (*jwt.AccountClaims, error) {
	if f.err != nil {
		return nil, f.err
	}
	c, ok := f.claims[accountPub]
	if !ok {
		return nil, errors.New("account not found in resolver")
	}
	return c, nil
}

func mustAccountKey() (string, string) {
	kp, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	pub, err := kp.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	seed, err := kp.Seed()
	Expect(err).NotTo(HaveOccurred())
	return pub, string(seed)
}

func mustUserKey() string {
	kp, err := nkeys.CreateUser()
	Expect(err).NotTo(HaveOccurred())
	pub, err := kp.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	return pub
}

// jwtGrants is what a mint recorded on the row — deliberately broader than
// the scope template below, so "effective" and "the JWT's own" can never be
// confused for one another in an assertion.
func jwtGrants() *jwt.UserPermissionLimits {
	var p jwt.UserPermissionLimits
	p.Pub.Allow.Add("api.>", "_INBOX.>")
	p.Sub.Allow.Add("api.>", "notify.>", "_INBOX.>")
	p.Subs = 100
	return &p
}

var _ = Describe("User claims view", func() {
	var (
		ctx        context.Context
		accountPub string
		signingPub string
		userPub    string
		store      *fakeUserReadStore
		lookup     *fakeAccountLookup
		acct       *jwt.AccountClaims
	)

	BeforeEach(func() {
		ctx = context.Background()
		accountPub, _ = mustAccountKey()
		signingPub, _ = mustAccountKey()
		userPub = mustUserKey()

		acct = jwt.NewAccountClaims(accountPub)
		acct.Name = "acme"

		store = &fakeUserReadStore{users: []accounts.User{{
			PublicKey:   userPub,
			Name:        "browser-acme",
			Account:     "acme",
			AccountKey:  accountPub,
			IssuerKey:   signingPub,
			Permissions: jwtGrants(),
			Kind:        accounts.UserKindSession,
			Status:      accounts.UserStatusActive,
			IssuedAt:    time.Now(),
		}}}
		lookup = &fakeAccountLookup{claims: map[string]*jwt.AccountClaims{accountPub: acct}}
	})

	Context("BR-AC41 — when the issuer is a plain (unscoped) signing key", func() {
		BeforeEach(func() {
			acct.SigningKeys.Add(signingPub)
		})

		It("reports the JWT's own permissions as effective, with nothing struck through", func() {
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)
			Expect(err).NotTo(HaveOccurred())

			Expect(view.Scoped).To(BeFalse())
			Expect(view.Effective).NotTo(BeNil())
			Expect([]string(view.Effective.Pub.Allow)).To(ConsistOf("api.>", "_INBOX.>"))
			// Nothing was discarded, so there is nothing to show struck
			// through — an empty JWTGrants is what the panel keys off.
			Expect(view.JWTGrants).To(BeNil())
			Expect(view.Unresolved).To(BeEmpty())
		})
	})

	Context("BR-AC41 — when the issuer is a scoped signing key", func() {
		BeforeEach(func() {
			scope := jwt.NewUserScope()
			scope.Key = signingPub
			scope.Role = "browser"
			scope.Template.Pub.Allow.Add("api._platform.>")
			scope.Template.Sub.Allow.Add("_INBOX.>")
			acct.SigningKeys.AddScopedSigner(scope)
		})

		It("makes the scope's template authoritative", func() {
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)
			Expect(err).NotTo(HaveOccurred())

			Expect(view.Scoped).To(BeTrue())
			Expect(view.ScopeRole).To(Equal("browser"))
			Expect([]string(view.Effective.Pub.Allow)).To(ConsistOf("api._platform.>"))
			Expect([]string(view.Effective.Sub.Allow)).To(ConsistOf("_INBOX.>"))
		})

		It("returns the JWT's own grants separately, so a wrongly-minted credential is still visible", func() {
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)
			Expect(err).NotTo(HaveOccurred())

			Expect(view.JWTGrants).NotTo(BeNil())
			Expect([]string(view.JWTGrants.Pub.Allow)).To(ConsistOf("api.>", "_INBOX.>"))
			// The whole point: what the JWT asked for is NOT what is enforced.
			Expect([]string(view.Effective.Pub.Allow)).NotTo(ContainElement("api.>"))
		})
	})

	Context("BR-AC41 — when the issuer is the account's own identity key", func() {
		BeforeEach(func() {
			store.users[0].IssuerKey = accountPub
		})

		It("treats the JWT's permissions as enforced — an identity key carries no scope", func() {
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)
			Expect(err).NotTo(HaveOccurred())

			Expect(view.Scoped).To(BeFalse())
			Expect(view.Unresolved).To(BeEmpty())
			Expect([]string(view.Effective.Pub.Allow)).To(ConsistOf("api.>", "_INBOX.>"))
		})
	})

	Context("BR-AC41 — when the account cannot be resolved", func() {
		It("says so rather than presenting the JWT's grants as effective", func() {
			lookup.err = errors.New("resolver timeout")
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)

			// A failed lookup is not a failed read: the row itself is still
			// worth returning. What must not happen is silently labelling the
			// JWT's grants "effective" when nothing checked the scope.
			Expect(err).NotTo(HaveOccurred())
			Expect(view.Unresolved).To(ContainSubstring("resolver timeout"))
			Expect(view.Scoped).To(BeFalse())
		})

		It("says so when the issuer key was never recorded", func() {
			store.users[0].IssuerKey = ""
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)
			Expect(err).NotTo(HaveOccurred())
			Expect(view.Unresolved).NotTo(BeEmpty())
		})

		It("says so when the issuer is not a signing key on the account at all", func() {
			// No SigningKeys.Add — the recorded issuer is neither the identity
			// key nor a listed signing key, which means the account has been
			// rotated since the mint and the credential no longer verifies.
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)
			Expect(err).NotTo(HaveOccurred())
			Expect(view.Unresolved).To(ContainSubstring("not a signing key"))
		})
	})

	Context("BR-AC41 — when no permissions were recorded", func() {
		It("reports unknown rather than an empty permission set", func() {
			acct.SigningKeys.Add(signingPub)
			store.users[0].Permissions = nil
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			view, err := reader.Get(ctx, userPub)
			Expect(err).NotTo(HaveOccurred())

			// An empty jwt.UserPermissionLimits reads as "allowed nothing",
			// which is a different and much more alarming claim than "this
			// service never saw what it was granted".
			Expect(view.Effective).To(BeNil())
			Expect(view.Unresolved).NotTo(BeEmpty())
		})
	})

	Context("BR-AC40 — the list is metadata only", func() {
		It("never carries permissions, a seed, or a credential body", func() {
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			users, err := reader.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(users).To(HaveLen(1))
			Expect(users[0].PublicKey).To(Equal(userPub))
			Expect(users[0].Account).To(Equal("acme"))
			Expect(users[0].Kind).To(Equal(accounts.UserKindSession))
		})
	})

	Context("an unknown user", func() {
		It("is ErrUserNotFound, not an empty view", func() {
			reader := accounts.NewUserClaimsReader(store, lookup, nil)
			_, err := reader.Get(ctx, mustUserKey())
			Expect(err).To(MatchError(accounts.ErrUserNotFound))
		})
	})
})
