package accounts_test

// BR-AC40 — api._platform.accounts.user.list.v1 / .get.v1.
//
// End-to-end over an embedded NATS server rather than by calling the handler
// funcs directly: the thing under test is a wire contract the Admin UI's
// browser credential talks to, so the subjects, the request shape and the
// reply shape all have to be exercised as they are actually served.

import (
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// startEmbeddedNATS runs a server for the life of one spec. Inline rather
// than shared/natstest, which takes a *testing.T this Ginkgo suite has no
// way to hand it.
func startEmbeddedNATS(name string) *nats.Conn {
	srv, err := server.NewServer(&server.Options{Port: -1})
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name(name))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	return nc
}

var _ = Describe("Users api.* adapter", func() {
	var (
		nc          *nats.Conn
		adapter     *accounts.UsersAdapter
		store       *fakeUserReadStore
		lookup      *fakeAccountLookup
		userPub     string
		accountPub  string
		signingPub  string
		natsRevoker *fakeNATSRevoker
	)

	BeforeEach(func() {
		nc = startEmbeddedNATS("accounts-service")

		accountPub, _ = mustAccountKey()
		signingPub, _ = mustAccountKey()
		userPub = mustUserKey()

		acct := jwt.NewAccountClaims(accountPub)
		acct.Name = "acme"
		acct.SigningKeys.Add(signingPub)

		store = &fakeUserReadStore{users: []accounts.User{{
			PublicKey:   userPub,
			Name:        "acme",
			Account:     "acme",
			AccountKey:  accountPub,
			IssuerKey:   signingPub,
			Permissions: jwtGrants(),
			Kind:        accounts.UserKindCredential,
			Status:      accounts.UserStatusActive,
			Source:      accounts.UserSourceBootstrap,
			IssuedAt:    time.Now(),
		}}}
		lookup = &fakeAccountLookup{claims: map[string]*jwt.AccountClaims{accountPub: acct}}

		natsRevoker = &fakeNATSRevoker{}
		var err error
		adapter, err = accounts.NewUsersAdapter(nc,
			accounts.NewUserClaimsReader(store, lookup, nil),
			accounts.NewUserRevoker(store, natsRevoker, nil),
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })
	})

	request := func(subject string, body any) map[string]any {
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			Expect(err).NotTo(HaveOccurred())
		}
		msg, err := nc.Request(subject, payload, 3*time.Second)
		Expect(err).NotTo(HaveOccurred())
		out := map[string]any{}
		Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
		return out
	}

	Context("BR-AC40 — the subjects are PLATFORM-only", func() {
		It("registers exactly the fixed-literal _platform subjects", func() {
			Expect(accounts.UsersAdapterSubjects()).To(ConsistOf(
				"api._platform.accounts.user.list.v1",
				"api._platform.accounts.user.get.v1",
				"api._platform.accounts.user.revoke.v1",
			))
		})

		It("uses no tenant wildcard token, so a tenant browser cannot reach it by subject", func() {
			// A tenant's browser credential is granted api.> inside its OWN
			// account (MintBrowserToken). These endpoints are mounted on the
			// PLATFORM connection only, and their fixed "_platform" token means
			// there is no per-tenant subject for a tenant to publish on even if
			// they were mounted more widely.
			for _, s := range accounts.UsersAdapterSubjects() {
				Expect(s).NotTo(ContainSubstring("*"))
				Expect(s).To(HavePrefix("api._platform.accounts."))
			}
		})
	})

	Context("BR-AC40 — .list.v1", func() {
		It("returns every recorded user as metadata", func() {
			out := request("api._platform.accounts.user.list.v1", nil)
			users, ok := out["users"].([]any)
			Expect(ok).To(BeTrue())
			Expect(users).To(HaveLen(1))

			row := users[0].(map[string]any)
			Expect(row["publicKey"]).To(Equal(userPub))
			Expect(row["name"]).To(Equal("acme"))
			Expect(row["kind"]).To(Equal("credential"))
			Expect(row["source"]).To(Equal("bootstrap"))
		})

		It("never returns a seed, a creds body, or a signed JWT", func() {
			out := request("api._platform.accounts.user.list.v1", nil)
			raw, err := json.Marshal(out)
			Expect(err).NotTo(HaveOccurred())
			// The registry never holds any of these — this asserts the wire
			// shape can't start leaking one either.
			Expect(string(raw)).NotTo(ContainSubstring("seed"))
			Expect(string(raw)).NotTo(ContainSubstring("creds"))
			Expect(string(raw)).NotTo(ContainSubstring("jwt"))
		})

		It("is cross-tenant — one call covers every account", func() {
			otherPub, _ := mustAccountKey()
			store.users = append(store.users, accounts.User{
				PublicKey: mustUserKey(), Name: "globex", Account: "globex",
				AccountKey: otherPub, Kind: accounts.UserKindCredential,
				Status: accounts.UserStatusActive, IssuedAt: time.Now(),
			})
			out := request("api._platform.accounts.user.list.v1", nil)
			Expect(out["users"].([]any)).To(HaveLen(2))
		})
	})

	Context("BR-AC40 — .get.v1", func() {
		It("returns the claims view for one user", func() {
			out := request("api._platform.accounts.user.get.v1", map[string]string{"publicKey": userPub})
			Expect(out["publicKey"]).To(Equal(userPub))
			Expect(out["scoped"]).To(Equal(false))
			Expect(out["effective"]).NotTo(BeNil())
		})

		It("replies with a notFound error for an unknown key", func() {
			out := request("api._platform.accounts.user.get.v1", map[string]string{"publicKey": mustUserKey()})
			Expect(out["notFound"]).To(Equal(true))
			Expect(out["error"]).NotTo(BeEmpty())
		})

		It("rejects a request with no public key", func() {
			out := request("api._platform.accounts.user.get.v1", map[string]string{})
			Expect(out["error"]).NotTo(BeEmpty())
		})
	})

	// Phase 51b (BR-AC43) — the revoke write, exercised on the wire for the
	// same reason the reads are: the Admin UI talks to this subject directly.
	Context("BR-AC43 — .revoke.v1", func() {
		It("revokes the named credential and returns the stamped row", func() {
			out := request("api._platform.accounts.user.revoke.v1", accounts.UserRevokeRequest{PublicKey: userPub})
			Expect(out).To(HaveKeyWithValue("publicKey", userPub))
			Expect(out["revokedAt"]).NotTo(BeEmpty())
			Expect(natsRevoker.users).To(ConsistOf(userPub))
		})

		It("rejects a request with no public key rather than revoking something arbitrary", func() {
			out := request("api._platform.accounts.user.revoke.v1", accounts.UserRevokeRequest{})
			Expect(out["error"]).NotTo(BeEmpty())
			Expect(natsRevoker.users).To(BeEmpty())
		})

		It("rejects a session, carrying the domain refusal out to the caller", func() {
			store.users[0].Kind = accounts.UserKindSession
			out := request("api._platform.accounts.user.revoke.v1", accounts.UserRevokeRequest{PublicKey: userPub})
			Expect(out["error"]).NotTo(BeEmpty())
			Expect(natsRevoker.users).To(BeEmpty())
		})

		It("returns no seed, creds body or signed JWT (BR-AC40)", func() {
			out := request("api._platform.accounts.user.revoke.v1", accounts.UserRevokeRequest{PublicKey: userPub})
			raw, err := json.Marshal(out)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(raw)).NotTo(ContainSubstring("eyJ"))
			Expect(string(raw)).NotTo(ContainSubstring("-----BEGIN"))
			for _, k := range []string{"seed", "nkeySeed", "creds", "jwt", "permissions"} {
				Expect(out).NotTo(HaveKey(k))
			}
		})
	})
})
