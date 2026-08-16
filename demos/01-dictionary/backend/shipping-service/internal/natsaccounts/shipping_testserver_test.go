package natsaccounts

// shippingTestServer builds a self-contained operator-mode embedded NATS
// server for the Phase 13a isolation specs — SYS, PLATFORM, ACME, and GLOBEX
// accounts with the imports/exports the specs exercise, plus the shipping-admin
// user with its restricted subject allowlist, all minted in-process via
// jwt/v2 + nkeys. Zero dependency on nats/nats.conf, nats/creds, or the nsc
// binary.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/onsi/gomega"
)

type shippingTestServer struct {
	srv            *server.Server
	credsDir       string
	accountPubKeys map[string]string // name → NATS account public key
}

func newShippingTestServer(t *testing.T) *shippingTestServer {
	t.Helper()
	g := gomega.NewWithT(t)

	dir := t.TempDir()

	// Operator keypair + one signing key (accounts are signed by the signing key).
	operatorKP, err := nkeys.CreateOperator()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	operatorPub, err := operatorKP.PublicKey()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	signingKP, err := nkeys.CreateOperator()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	signingPub, err := signingKP.PublicKey()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	opClaims := jwt.NewOperatorClaims(operatorPub)
	opClaims.SigningKeys.Add(signingPub)

	// SYS account — required for operator mode; placed in resolver_preload.
	sysKP, err := nkeys.CreateAccount()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	sysPub, err := sysKP.PublicKey()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	opClaims.SystemAccount = sysPub

	opJWT, err := opClaims.Encode(operatorKP)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sysClaims := jwt.NewAccountClaims(sysPub)
	sysClaims.Name = "SYS"
	sysJWT, err := sysClaims.Encode(signingKP)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Helper that returns a JetStream-enabled OperatorLimits value.
	unlimited := func(wildcardExports bool) jwt.OperatorLimits {
		return jwt.OperatorLimits{
			NatsLimits: jwt.NatsLimits{
				Subs: jwt.NoLimit, Data: jwt.NoLimit, Payload: jwt.NoLimit,
			},
			AccountLimits: jwt.AccountLimits{
				Imports: jwt.NoLimit, Exports: jwt.NoLimit,
				WildcardExports: wildcardExports,
				Conn:            jwt.NoLimit, LeafNodeConn: jwt.NoLimit,
			},
			JetStreamLimits: jwt.JetStreamLimits{
				MemoryStorage: jwt.NoLimit,
				DiskStorage:   jwt.NoLimit,
				Streams:       jwt.NoLimit,
				Consumer:      jwt.NoLimit,
			},
		}
	}

	// PLATFORM account — exports the rpc service and notify stream that
	// tenant accounts import.
	platformKP, err := nkeys.CreateAccount()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	platformPub, err := platformKP.PublicKey()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	platformClaims := jwt.NewAccountClaims(platformPub)
	platformClaims.Name = "PLATFORM"
	platformClaims.Limits = unlimited(true) // needs wildcard exports for rpc.*
	platformClaims.Exports = jwt.Exports{
		{
			Name:         "rpc.*.refdata.item.get.v1",
			Subject:      "rpc.*.refdata.item.get.v1",
			Type:         jwt.Service,
			ResponseType: jwt.ResponseTypeSingleton,
		},
		{
			Name:    "notify.accounts.account.*",
			Subject: "notify.accounts.account.*",
			Type:    jwt.Stream,
		},
	}
	platformJWT, err := platformClaims.Encode(signingKP)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// ACME account — imports the rpc service and notify stream from PLATFORM.
	acmeKP, err := nkeys.CreateAccount()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	acmePub, err := acmeKP.PublicKey()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	acmeClaims := jwt.NewAccountClaims(acmePub)
	acmeClaims.Name = "ACME"
	acmeClaims.Limits = unlimited(false)
	acmeClaims.Imports = jwt.Imports{
		{
			Name:         "rpc.acme.refdata.item.get.v1",
			Subject:      jwt.Subject("rpc.acme.refdata.item.get.v1"),
			Account:      platformPub,
			LocalSubject: jwt.RenamingSubject("refdata.item.get.v1"),
			Type:         jwt.Service,
		},
		{
			Name:         "notify.accounts.account.*",
			Subject:      jwt.Subject("notify.accounts.account.*"),
			Account:      platformPub,
			LocalSubject: jwt.RenamingSubject("notify.accounts.account.*"),
			Type:         jwt.Stream,
		},
	}
	acmeJWT, err := acmeClaims.Encode(signingKP)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// GLOBEX account — same import shape as ACME but uses "globex" in the
	// rpc subject so the old-style cross-context address test can distinguish
	// the two.
	globexKP, err := nkeys.CreateAccount()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	globexPub, err := globexKP.PublicKey()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	globexClaims := jwt.NewAccountClaims(globexPub)
	globexClaims.Name = "GLOBEX"
	globexClaims.Limits = unlimited(false)
	globexClaims.Imports = jwt.Imports{
		{
			Name:         "rpc.globex.refdata.item.get.v1",
			Subject:      jwt.Subject("rpc.globex.refdata.item.get.v1"),
			Account:      platformPub,
			LocalSubject: jwt.RenamingSubject("refdata.item.get.v1"),
			Type:         jwt.Service,
		},
		{
			Name:         "notify.accounts.account.*",
			Subject:      jwt.Subject("notify.accounts.account.*"),
			Account:      platformPub,
			LocalSubject: jwt.RenamingSubject("notify.accounts.account.*"),
			Type:         jwt.Stream,
		},
	}
	globexJWT, err := globexClaims.Encode(signingKP)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Write operator JWT and build server config with all four JWTs in
	// resolver_preload so no SYS-connection bootstrap step is needed.
	operatorPath := filepath.Join(dir, "operator.jwt")
	g.Expect(os.WriteFile(operatorPath, []byte(opJWT), 0o600)).To(gomega.Succeed())
	resolverDir := filepath.Join(dir, "resolver")
	g.Expect(os.MkdirAll(resolverDir, 0o700)).To(gomega.Succeed())

	conf := fmt.Sprintf(`
port: -1
http_port: 0
jetstream { store_dir: %q }
operator: %q
system_account: %q
resolver: { type: full, dir: %q, allow_delete: true, interval: "2m" }
resolver_preload: {
  %s: %q
  %s: %q
  %s: %q
  %s: %q
}
`,
		filepath.Join(dir, "js"),
		operatorPath,
		sysPub,
		resolverDir,
		sysPub, sysJWT,
		platformPub, platformJWT,
		acmePub, acmeJWT,
		globexPub, globexJWT,
	)
	confPath := filepath.Join(dir, "nats.conf")
	g.Expect(os.WriteFile(confPath, []byte(conf), 0o600)).To(gomega.Succeed())

	opts, err := server.ProcessConfigFile(confPath)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	srv, err := server.NewServer(opts)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	srv.Start()
	g.Expect(srv.ReadyForConnections(10 * time.Second)).To(gomega.BeTrue())
	t.Cleanup(srv.Shutdown)

	// Mint one user per account and write their .creds files to a temp dir.
	credsDir := t.TempDir()

	mintCreds := func(accountKP nkeys.KeyPair, accountPub, userName string) {
		t.Helper()
		userKP, err := nkeys.CreateUser()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		userPub, err := userKP.PublicKey()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		userSeed, err := userKP.Seed()
		g.Expect(err).NotTo(gomega.HaveOccurred())

		userClaims := jwt.NewUserClaims(userPub)
		userClaims.Name = userName
		userJWT, err := userClaims.Encode(accountKP)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		creds, err := jwt.FormatUserConfig(userJWT, userSeed)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(os.WriteFile(filepath.Join(credsDir, userName+".creds"), creds, 0o600)).To(gomega.Succeed())
	}

	mintCreds(platformKP, platformPub, "platform")
	mintCreds(acmeKP, acmePub, "acme")
	mintCreds(globexKP, globexPub, "globex")

	// shipping-admin is a PLATFORM user with a narrow pub/sub allowlist —
	// consumer API for REFDATA/RPCTRACE only, no stream creation.
	adminKP, err := nkeys.CreateUser()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	adminPub, err := adminKP.PublicKey()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	adminSeed, err := adminKP.Seed()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	adminClaims := jwt.NewUserClaims(adminPub)
	adminClaims.Name = "shipping-admin"
	adminClaims.Permissions.Pub.Allow = jwt.StringList{
		"$JS.API.CONSUMER.CREATE.REFDATA.>",
		"$JS.API.CONSUMER.CREATE.RPCTRACE.>",
		"$JS.API.CONSUMER.DELETE.REFDATA.>",
		"$JS.API.CONSUMER.DELETE.RPCTRACE.>",
		"$JS.API.CONSUMER.INFO.REFDATA.>",
		"$JS.API.CONSUMER.INFO.RPCTRACE.>",
		"$JS.API.CONSUMER.MSG.NEXT.REFDATA.>",
		"$JS.API.CONSUMER.MSG.NEXT.RPCTRACE.>",
		"$SRV.>",
		"notify._platform.>",
	}
	adminClaims.Permissions.Sub.Allow = jwt.StringList{
		"$JS.API.CONSUMER.MSG.NEXT.REFDATA.>",
		"$JS.API.CONSUMER.MSG.NEXT.RPCTRACE.>",
		"$SRV.>",
		"_INBOX.>",
	}
	adminJWT, err := adminClaims.Encode(platformKP)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	adminCreds, err := jwt.FormatUserConfig(adminJWT, adminSeed)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(os.WriteFile(filepath.Join(credsDir, "shipping-admin.creds"), adminCreds, 0o600)).To(gomega.Succeed())

	return &shippingTestServer{
		srv:      srv,
		credsDir: credsDir,
		accountPubKeys: map[string]string{
			"platform": platformPub,
			"acme":     acmePub,
			"globex":   globexPub,
		},
	}
}

// connectAs dials the embedded server authenticated as the named user.
// Passing an empty name dials with no credentials (always rejected in
// operator mode). The connection is closed automatically when t completes.
func (s *shippingTestServer) connectAs(t *testing.T, name string) *nats.Conn {
	t.Helper()
	g := gomega.NewWithT(t)
	opts := []nats.Option{nats.Name("phase13a-isolation-test")}
	if name != "" {
		opts = append(opts, nats.UserCredentials(filepath.Join(s.credsDir, name+".creds")))
	}
	nc, err := nats.Connect(s.srv.ClientURL(), opts...)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	t.Cleanup(nc.Close)
	return nc
}

// credsPath returns the filesystem path to the named user's .creds file.
func (s *shippingTestServer) credsPath(name string) string {
	return filepath.Join(s.credsDir, name+".creds")
}

// accountPubKey returns the NATS account public key for the named account
// (e.g. "globex" → the account's NKey public key). Used by tests that need
// to construct cross-account subject strings.
func (s *shippingTestServer) accountPubKey(name string) string {
	return s.accountPubKeys[name]
}
