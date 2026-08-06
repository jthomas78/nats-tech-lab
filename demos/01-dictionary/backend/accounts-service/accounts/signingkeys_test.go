package accounts_test

// BR-AC19 specs — adopting bootstrap-operator.sh's per-account signing key
// seeds instead of minting a fresh random one on every wiped boot.

import (
	"os"
	"path/filepath"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// newSigningKey mints an account keypair and returns its seed and public key,
// standing in for one `nsc edit account --sk generate` produced.
func newSigningKey() (seed string, pub string) {
	GinkgoHelper()
	kp, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	rawSeed, err := kp.Seed()
	Expect(err).NotTo(HaveOccurred())
	pub, err = kp.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	return string(rawSeed), pub
}

// claimsTrusting builds an account JWT's claims listing the given signing
// keys, standing in for the resolver JWT under nats/resolver/.
func claimsTrusting(signingKeys ...string) *jwt.AccountClaims {
	GinkgoHelper()
	kp, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	pub, err := kp.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	claims := jwt.NewAccountClaims(pub)
	claims.SigningKeys.Add(signingKeys...)
	return claims
}

var _ = Describe("BR-AC19 — adopting the bootstrap account signing key", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	writeSeed := func(accountName, seed string) {
		GinkgoHelper()
		path := filepath.Join(dir, accounts.SigningKeyFileName(accountName))
		Expect(os.WriteFile(path, []byte(seed+"\n"), 0o600)).To(Succeed())
	}

	Context("when bootstrap-operator.sh exported a seed the resolver JWT trusts", func() {
		It("adopts it, so the account's identity survives a wiped boot", func() {
			seed, pub := newSigningKey()
			writeSeed("globex", seed)

			got, err := accounts.ResolveSeededSigningKey(dir, "globex", claimsTrusting(pub))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(seed))
		})

		It("looks the file up under the lowercase tenant identity", func() {
			seed, pub := newSigningKey()
			writeSeed("globex", seed)

			// BR-AC05's naming note: nsc names the account GLOBEX, but every
			// other artifact is keyed by the lowercase tenant identity.
			got, err := accounts.ResolveSeededSigningKey(dir, "GLOBEX", claimsTrusting(pub))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(seed))
		})
	})

	Context("when there is no exported seed", func() {
		It("reports none for a stack bootstrapped before BR-AC19", func() {
			_, pub := newSigningKey()

			got, err := accounts.ResolveSeededSigningKey(dir, "globex", claimsTrusting(pub))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("reports none when no keys directory is configured", func() {
			got, err := accounts.ResolveSeededSigningKey("", "globex", claimsTrusting("ANYKEY"))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})
	})

	Context("when the exported seed does not match the resolver JWT", func() {
		// The whole point of the rule: a seed the server does not trust would
		// mint user JWTs it rejects. Fail at startup, not at connect time.
		It("refuses a seed absent from the account's signing keys", func() {
			seed, _ := newSigningKey()
			_, otherPub := newSigningKey()
			writeSeed("globex", seed)

			_, err := accounts.ResolveSeededSigningKey(dir, "globex", claimsTrusting(otherPub))

			Expect(err).To(MatchError(ContainSubstring("not listed in globex's resolver JWT")))
		})

		It("refuses a seed that is not an account key", func() {
			userKP, err := nkeys.CreateUser()
			Expect(err).NotTo(HaveOccurred())
			userSeed, err := userKP.Seed()
			Expect(err).NotTo(HaveOccurred())
			writeSeed("globex", string(userSeed))

			_, err = accounts.ResolveSeededSigningKey(dir, "globex", claimsTrusting("ANYKEY"))

			Expect(err).To(MatchError(ContainSubstring("not an account key")))
		})

		It("refuses an unparseable seed", func() {
			writeSeed("globex", "not-a-valid-nkey-seed")

			_, err := accounts.ResolveSeededSigningKey(dir, "globex", claimsTrusting("ANYKEY"))

			Expect(err).To(MatchError(ContainSubstring("parse account signing key")))
		})

		It("refuses to verify a seed with no resolver claims to check against", func() {
			seed, _ := newSigningKey()
			writeSeed("globex", seed)

			_, err := accounts.ResolveSeededSigningKey(dir, "globex", nil)

			Expect(err).To(MatchError(ContainSubstring("no resolver claims")))
		})
	})
})
