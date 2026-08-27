package accounts_test

// Phase 50a, BR-AC39 — the registry converges on start for users minted
// outside this service. The bootstrap users (nats/bootstrap-operator.sh's
// `nsc add user`) exist only as .creds files on the shared creds volume, so
// that directory is the only record of them there is.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

type fakeUserSink struct {
	inserted []accounts.NewUser
	err      error
}

func (f *fakeUserSink) InsertUserIfMissing(_ context.Context, u accounts.NewUser) error {
	if f.err != nil {
		return f.err
	}
	f.inserted = append(f.inserted, u)
	return nil
}

var _ = Describe("BackfillCredsDirUsers (BR-AC39)", func() {
	var (
		dir        string
		sink       *fakeUserSink
		accountPub string
		ctx        = context.Background()
		log        = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	)

	writeCreds := func(name string, configure func(*jwt.UserClaims)) {
		accountKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		if accountPub == "" {
			accountPub, err = accountKP.PublicKey()
			Expect(err).NotTo(HaveOccurred())
		}
		userKP, err := nkeys.CreateUser()
		Expect(err).NotTo(HaveOccurred())
		userPub, err := userKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())
		userSeed, err := userKP.Seed()
		Expect(err).NotTo(HaveOccurred())

		claims := jwt.NewUserClaims(userPub)
		claims.Name = name
		claims.IssuerAccount = accountPub
		if configure != nil {
			configure(claims)
		}
		// Signed by the account identity key itself, the way `nsc generate
		// creds` does for a bootstrap user.
		signingKP, err := nkeys.CreateAccount()
		Expect(err).NotTo(HaveOccurred())
		token, err := claims.Encode(signingKP)
		Expect(err).NotTo(HaveOccurred())
		creds, err := jwt.FormatUserConfig(token, userSeed)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(dir, name+".creds"), creds, 0o600)).To(Succeed())
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		accountPub = ""
		sink = &fakeUserSink{}
	})

	It("records every .creds file it can decode as an already-active bootstrap user", func() {
		writeCreds("shipping-admin", nil)
		writeCreds("observability", func(c *jwt.UserClaims) { c.BearerToken = true })

		n, err := accounts.BackfillCredsDirUsers(ctx, sink, dir, map[string]string{accountPub: "platform"}, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(2))

		byName := map[string]accounts.NewUser{}
		for _, u := range sink.inserted {
			byName[u.Name] = u
		}
		Expect(byName).To(HaveKey("shipping-admin"))
		Expect(byName["shipping-admin"].Status).To(Equal(accounts.UserStatusActive),
			"a credential that already exists on disk was minted successfully — it was never pending")
		Expect(byName["shipping-admin"].Source).To(Equal(accounts.UserSourceBootstrap))
		Expect(byName["shipping-admin"].Kind).To(Equal(accounts.UserKindCredential))
		Expect(byName["shipping-admin"].Account).To(Equal("platform"), "the issuing account key resolves to its account name")
		Expect(byName["shipping-admin"].PublicKey).To(HavePrefix("U"))
		Expect(byName["observability"].Bearer).To(BeTrue())
	})

	It("falls back to the issuing account's public key when no account name is known for it", func() {
		writeCreds("stray", nil)

		n, err := accounts.BackfillCredsDirUsers(ctx, sink, dir, map[string]string{}, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(1))
		Expect(sink.inserted[0].Account).To(Equal(accountPub))
	})

	It("skips files it cannot decode and files that are not .creds, without failing the start-up pass", func() {
		writeCreds("good", nil)
		Expect(os.WriteFile(filepath.Join(dir, "broken.creds"), []byte("not a jwt"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o600)).To(Succeed())

		n, err := accounts.BackfillCredsDirUsers(ctx, sink, dir, nil, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(1))
		Expect(sink.inserted).To(HaveLen(1))
		Expect(sink.inserted[0].Name).To(Equal("good"))
	})

	It("is a no-op when the creds directory does not exist", func() {
		n, err := accounts.BackfillCredsDirUsers(ctx, sink, filepath.Join(dir, "nope"), nil, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(BeZero())
	})
})
