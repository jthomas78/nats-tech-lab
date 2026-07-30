package accounts_test

// Builds a self-contained operator-mode embedded NATS server for tests —
// mints its own throwaway operator/SYS/signing keys directly via jwt/v2 +
// nkeys (not the nsc CLI nats/bootstrap-operator.sh uses), so these specs
// need no external binary and don't touch the repo's real nats/ artifacts.
// Mirrors the resolver:full + resolver_preload shape
// shipping-service/internal/natsaccounts/isolation_test.go proves against
// the real shipped config; here the config is synthetic on purpose, since
// Provisioner is what generates new accounts at runtime; there is no
// "shipped" resolver_preload entry for those yet.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type operatorTestServer struct {
	Server                 *server.Server
	OperatorSigningKeySeed []byte
	SysCredsPath           string
	ResolverDir            string
}

// tempDirer is the one method these helpers need — satisfied by *testing.T
// and by GinkgoT() (whose FullGinkgoTInterface doesn't satisfy testing.TB
// directly in this ginkgo version, but does have TempDir()).
type tempDirer interface {
	TempDir() string
}

func newOperatorTestServer(t tempDirer) *operatorTestServer {
	GinkgoHelper()

	operatorKP, err := nkeys.CreateOperator()
	Expect(err).NotTo(HaveOccurred())
	operatorPub, err := operatorKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())

	signingKP, err := nkeys.CreateOperator()
	Expect(err).NotTo(HaveOccurred())
	signingPub, err := signingKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	signingSeed, err := signingKP.Seed()
	Expect(err).NotTo(HaveOccurred())

	sysKP, err := nkeys.CreateAccount()
	Expect(err).NotTo(HaveOccurred())
	sysPub, err := sysKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())

	opClaims := jwt.NewOperatorClaims(operatorPub)
	opClaims.SigningKeys.Add(signingPub)
	opClaims.SystemAccount = sysPub
	opJWT, err := opClaims.Encode(operatorKP)
	Expect(err).NotTo(HaveOccurred())

	sysClaims := jwt.NewAccountClaims(sysPub)
	sysClaims.Name = "SYS"
	sysJWT, err := sysClaims.Encode(signingKP)
	Expect(err).NotTo(HaveOccurred())

	sysUserKP, err := nkeys.CreateUser()
	Expect(err).NotTo(HaveOccurred())
	sysUserPub, err := sysUserKP.PublicKey()
	Expect(err).NotTo(HaveOccurred())
	sysUserSeed, err := sysUserKP.Seed()
	Expect(err).NotTo(HaveOccurred())
	sysUserClaims := jwt.NewUserClaims(sysUserPub)
	sysUserClaims.Name = "sys"
	sysUserClaims.IssuerAccount = sysPub
	sysUserJWT, err := sysUserClaims.Encode(sysKP)
	Expect(err).NotTo(HaveOccurred())
	sysCreds, err := jwt.FormatUserConfig(sysUserJWT, sysUserSeed)
	Expect(err).NotTo(HaveOccurred())

	dir := t.TempDir()
	operatorPath := filepath.Join(dir, "operator.jwt")
	Expect(os.WriteFile(operatorPath, []byte(opJWT), 0o600)).To(Succeed())
	sysCredsPath := filepath.Join(dir, "sys.creds")
	Expect(os.WriteFile(sysCredsPath, sysCreds, 0o600)).To(Succeed())
	resolverDir := filepath.Join(dir, "resolver")
	Expect(os.MkdirAll(resolverDir, 0o700)).To(Succeed())

	confPath := filepath.Join(dir, "nats.conf")
	conf := fmt.Sprintf(`
port: -1
http_port: 0
jetstream { store_dir: %q }
operator: %q
system_account: %q
resolver: { type: full, dir: %q, allow_delete: true, interval: "2m" }
resolver_preload: { %s: %q }
`, filepath.Join(dir, "js"), operatorPath, sysPub, resolverDir, sysPub, sysJWT)
	Expect(os.WriteFile(confPath, []byte(conf), 0o600)).To(Succeed())

	opts, err := server.ProcessConfigFile(confPath)
	Expect(err).NotTo(HaveOccurred())

	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	return &operatorTestServer{
		Server:                 srv,
		OperatorSigningKeySeed: signingSeed,
		SysCredsPath:           sysCredsPath,
		ResolverDir:            resolverDir,
	}
}

func (o *operatorTestServer) Shutdown() {
	o.Server.Shutdown()
}

func (o *operatorTestServer) ConnectSys(t tempDirer) *nats.Conn {
	GinkgoHelper()
	nc, err := nats.Connect(o.Server.ClientURL(), nats.Name("test-sys"), nats.UserCredentials(o.SysCredsPath))
	Expect(err).NotTo(HaveOccurred())
	return nc
}

func (o *operatorTestServer) ConnectWithCreds(credsBytes []byte, name string) (*nats.Conn, error) {
	dir, err := os.MkdirTemp("", "creds")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "u.creds")
	if err := os.WriteFile(path, credsBytes, 0o600); err != nil {
		return nil, err
	}
	return nats.Connect(o.Server.ClientURL(), nats.Name(name), nats.UserCredentials(path))
}
