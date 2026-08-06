package dictionary

// tenantSwitchServer is a minimal synthetic operator-mode NATS server for
// the Phase 13b tenant-switch specs. Mints SYS, ACME, and GLOBEX accounts
// in-process via jwt/v2 + nkeys, writes tenant .creds files to a temp dir
// so rest.Deps.CredsDir and the test's connectAs closure can both point
// there. No dependency on nats/nats.conf, nats/creds, or nsc.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type tenantSwitchServer struct {
	srv      *server.Server
	CredsDir string
}

// newTenantSwitchServer creates a synthetic NATS server with ACME and GLOBEX
// tenant accounts (full JetStream access, no imports/exports needed for the
// tenant-switch specs). The server and its temp dir are registered with
// Ginkgo's DeferCleanup so callers need not shut anything down themselves.
// Call from within a BeforeEach or It block.
func newTenantSwitchServer() *tenantSwitchServer {
	GinkgoHelper()

	dir := GinkgoT().TempDir()

	// Operator keypair + signing key.
	operatorKP, err := nkeys.CreateOperator()
	Expect(err).NotTo(HaveOccurred())
	operatorPub, err := operatorKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())

	signingKP, err := nkeys.CreateOperator()
	Expect(err).NotTo(HaveOccurred())
	signingPub, err := signingKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())

	opClaims := jwt.NewOperatorClaims(operatorPub)
	opClaims.SigningKeys.Add(signingPub)

	// SYS account.
	sysKP, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	sysPub, err := sysKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	opClaims.SystemAccount = sysPub

	opJWT, err := opClaims.Encode(operatorKP)
	Expect(err).NotTo(HaveOccurred())

	sysClaims := jwt.NewAccountClaims(sysPub)
	sysClaims.Name = "SYS"
	sysJWT, err := sysClaims.Encode(signingKP)
	Expect(err).NotTo(HaveOccurred())

	// JetStream-enabled limits for tenant accounts.
	tenantLimits := jwt.OperatorLimits{
		NatsLimits: jwt.NatsLimits{
			Subs: jwt.NoLimit, Data: jwt.NoLimit, Payload: jwt.NoLimit,
		},
		AccountLimits: jwt.AccountLimits{
			Imports: jwt.NoLimit, Exports: jwt.NoLimit,
			Conn: jwt.NoLimit, LeafNodeConn: jwt.NoLimit,
		},
		JetStreamLimits: jwt.JetStreamLimits{
			MemoryStorage: jwt.NoLimit,
			DiskStorage:   jwt.NoLimit,
			Streams:       jwt.NoLimit,
			Consumer:      jwt.NoLimit,
		},
	}

	// ACME account.
	acmeKP, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	acmePub, err := acmeKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())

	acmeClaims := jwt.NewAccountClaims(acmePub)
	acmeClaims.Name = "ACME"
	acmeClaims.Limits = tenantLimits
	acmeJWT, err := acmeClaims.Encode(signingKP)
	Expect(err).NotTo(HaveOccurred())

	// GLOBEX account.
	globexKP, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	globexPub, err := globexKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())

	globexClaims := jwt.NewAccountClaims(globexPub)
	globexClaims.Name = "GLOBEX"
	globexClaims.Limits = tenantLimits
	globexJWT, err := globexClaims.Encode(signingKP)
	Expect(err).NotTo(HaveOccurred())

	// Write operator JWT and server config.
	operatorPath := filepath.Join(dir, "operator.jwt")
	Expect(os.WriteFile(operatorPath, []byte(opJWT), 0o600)).To(Succeed())
	resolverDir := filepath.Join(dir, "resolver")
	Expect(os.MkdirAll(resolverDir, 0o700)).To(Succeed())

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
}
`,
		filepath.Join(dir, "js"),
		operatorPath,
		sysPub,
		resolverDir,
		sysPub, sysJWT,
		acmePub, acmeJWT,
		globexPub, globexJWT,
	)
	confPath := filepath.Join(dir, "nats.conf")
	Expect(os.WriteFile(confPath, []byte(conf), 0o600)).To(Succeed())

	opts, err := server.ProcessConfigFile(confPath)
	Expect(err).NotTo(HaveOccurred())

	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())
	DeferCleanup(srv.Shutdown)

	// Mint one user per tenant account and write .creds files.
	credsDir := GinkgoT().TempDir()

	mintTenantCreds := func(accountKP nkeys.KeyPair, tenantName string) {
		GinkgoHelper()
		userKP, err := nkeys.CreateUser()
		Expect(err).NotTo(HaveOccurred())
		userPub, err := userKP.PublicKey()
		Expect(err).NotTo(HaveOccurred())
		userSeed, err := userKP.Seed()
		Expect(err).NotTo(HaveOccurred())

		userClaims := jwt.NewUserClaims(userPub)
		userClaims.Name = tenantName
		userJWT, err := userClaims.Encode(accountKP)
		Expect(err).NotTo(HaveOccurred())
		creds, err := jwt.FormatUserConfig(userJWT, userSeed)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(credsDir, tenantName+".creds"), creds, 0o600)).To(Succeed())
	}

	mintTenantCreds(acmeKP, "acme")
	mintTenantCreds(globexKP, "globex")

	return &tenantSwitchServer{srv: srv, CredsDir: credsDir}
}

// connectAs dials the embedded server authenticated as the named tenant.
// The connection and its JetStream context are closed automatically when
// the enclosing Ginkgo node completes.
func (s *tenantSwitchServer) connectAs(name string) (*nats.Conn, jetstream.JetStream) {
	GinkgoHelper()
	nc, err := nats.Connect(
		s.srv.ClientURL(),
		nats.Name("tenant-switch-test"),
		nats.UserCredentials(filepath.Join(s.CredsDir, name+".creds")),
	)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	return nc, js
}
