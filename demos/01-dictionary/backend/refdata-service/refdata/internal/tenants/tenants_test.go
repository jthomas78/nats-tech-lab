package tenants

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/browserrpc"
)

// --- test fixtures ---

// writeTestCreds writes a syntactically valid (but otherwise throwaway,
// unregistered) NATS .creds file — sufficient for nats.Connect against a
// plain, no-auth-configured embedded server (BR-D40's tests never need a
// real operator/JWT trust chain, only a creds file nats.go's client-side
// parser accepts).
func writeTestCreds(t *testing.T, dir, name string) string {
	t.Helper()
	accountKP, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatalf("create account keypair: %v", err)
	}
	accountPub, err := accountKP.PublicKey()
	if err != nil {
		t.Fatalf("account public key: %v", err)
	}
	userKP, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("create user keypair: %v", err)
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		t.Fatalf("user public key: %v", err)
	}
	userSeed, err := userKP.Seed()
	if err != nil {
		t.Fatalf("user seed: %v", err)
	}

	claims := jwt.NewUserClaims(userPub)
	claims.IssuerAccount = accountPub
	token, err := claims.Encode(accountKP)
	if err != nil {
		t.Fatalf("encode user jwt: %v", err)
	}

	contents := "-----BEGIN NATS USER JWT-----\n" + token + "\n------END NATS USER JWT------\n\n" +
		"************************* IMPORTANT *************************\n" +
		"NKEY Seed printed below can be used to sign and prove identity.\n" +
		"NKEYS are sensitive and should be treated as securely as a password.\n" +
		"-----BEGIN USER NKEY SEED-----\n" + string(userSeed) + "\n------END USER NKEY SEED------\n\n" +
		"*************************************************************\n"

	path := filepath.Join(dir, name+".creds")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}
	return path
}

// newTestServer starts a plain, no-auth embedded NATS server — mirrors
// internal/natstrace's own newTestConn helper. No auth configured means the
// server accepts any syntactically valid connect attempt, so
// writeTestCreds's throwaway (unregistered) creds are sufficient.
func newTestServer(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := &natsserver.Options{Port: -1}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("start embedded nats server: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("embedded nats server never became ready")
	}
	return srv
}

// --- Discover ---

func TestDiscoverExcludesNonTenantCredsFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"platform", "shipping-admin", "sys", "observability", "acme"} {
		if err := os.WriteFile(filepath.Join(dir, name+".creds"), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s.creds: %v", name, err)
		}
	}
	// Non-.creds files must be ignored regardless of name.
	if err := os.WriteFile(filepath.Join(dir, "acme.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write acme.txt: %v", err)
	}

	found, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("Discover found %d tenants, want 1 (acme only): %v", len(found), found)
	}
	if _, ok := found["acme"]; !ok {
		t.Fatalf("Discover did not find acme: %v", found)
	}
	for _, excluded := range []string{"platform", "shipping-admin", "sys", "observability"} {
		if _, ok := found[excluded]; ok {
			t.Errorf("Discover treated %q as a tenant — nonTenantCredsFiles is not excluding it", excluded)
		}
	}
}

func TestDiscoverIsCaseInsensitiveForExclusions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Platform.creds"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write Platform.creds: %v", err)
	}
	found, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if _, ok := found["Platform"]; ok {
		t.Errorf("Discover did not exclude a differently-cased non-tenant creds file")
	}
}

// --- Manager / ensure ---

func TestEnsureAllConnectsWithServiceNameAndAdapter(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	mgr := NewManager(srv.ClientURL(), dir, nil, browserrpc.Deps{})
	defer mgr.Close()

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}

	mgr.mu.RLock()
	res, ok := mgr.resources["acme"]
	mgr.mu.RUnlock()
	if !ok {
		t.Fatal("EnsureAll did not provision resources for acme")
	}
	// BR-D40/CLAUDE.md: every nats.Connect call must set nats.Name — asserted
	// on every connection this package opens, matching the PLATFORM
	// connection's own name (cmd/main.go) so a caller distinguishes them by
	// account, not by name.
	if got := res.nc.Opts.Name; got != "refdata-service" {
		t.Errorf("tenant connection nats.Name = %q, want %q", got, "refdata-service")
	}
	if res.adapter == nil {
		t.Error("EnsureAll did not register a browserrpc.Adapter for acme")
	}
}

func TestEnsureByNameNoOpWhenTenantNotYetVisible(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir() // empty — "missing" is not (yet) discoverable

	mgr := NewManager(srv.ClientURL(), dir, nil, browserrpc.Deps{})
	defer mgr.Close()

	if err := mgr.EnsureByName(context.Background(), "missing"); err != nil {
		t.Fatalf("EnsureByName on an undiscoverable tenant must be a no-op, not an error: %v", err)
	}
	mgr.mu.RLock()
	_, ok := mgr.resources["missing"]
	mgr.mu.RUnlock()
	if ok {
		t.Error("EnsureByName provisioned resources for a tenant that was never discoverable")
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	mgr := NewManager(srv.ClientURL(), dir, nil, browserrpc.Deps{})
	defer mgr.Close()

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}
	mgr.mu.RLock()
	first := mgr.resources["acme"]
	mgr.mu.RUnlock()

	// A second Ensure (e.g. EnsureByName reacting to a duplicate/racing
	// notify.accounts.account.created delivery) must return the exact same
	// resources, not reconnect or re-register.
	if err := mgr.EnsureByName(context.Background(), "acme"); err != nil {
		t.Fatalf("EnsureByName (second call): %v", err)
	}
	mgr.mu.RLock()
	second := mgr.resources["acme"]
	mgr.mu.RUnlock()
	if first != second {
		t.Error("a second Ensure for an already-provisioned tenant created new resources instead of returning the existing ones")
	}
}

func TestTeardownByNameIsIdempotentAndCloses(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	mgr := NewManager(srv.ClientURL(), dir, nil, browserrpc.Deps{})
	defer mgr.Close()

	// Never provisioned — must be a no-op, not an error (BR-D40).
	if err := mgr.TeardownByName(context.Background(), "never-seen"); err != nil {
		t.Fatalf("TeardownByName on an unprovisioned tenant must be a no-op: %v", err)
	}

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}
	mgr.mu.RLock()
	nc := mgr.resources["acme"].nc
	mgr.mu.RUnlock()

	if err := mgr.TeardownByName(context.Background(), "acme"); err != nil {
		t.Fatalf("TeardownByName: %v", err)
	}
	mgr.mu.RLock()
	_, stillPresent := mgr.resources["acme"]
	mgr.mu.RUnlock()
	if stillPresent {
		t.Error("TeardownByName left the tenant in the resources map")
	}
	if !nc.IsClosed() {
		t.Error("TeardownByName did not close the tenant's connection")
	}

	// Idempotent: tearing down an already-torn-down tenant is a no-op too.
	if err := mgr.TeardownByName(context.Background(), "acme"); err != nil {
		t.Fatalf("second TeardownByName must also be a no-op: %v", err)
	}
}

func TestCloseStopsEveryTenant(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")
	writeTestCreds(t, dir, "globex")

	mgr := NewManager(srv.ClientURL(), dir, nil, browserrpc.Deps{})
	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}

	mgr.mu.RLock()
	conns := []*nats.Conn{mgr.resources["acme"].nc, mgr.resources["globex"].nc}
	mgr.mu.RUnlock()

	mgr.Close()

	for i, nc := range conns {
		if !nc.IsClosed() {
			t.Errorf("connection %d not closed after Manager.Close()", i)
		}
	}
	mgr.mu.RLock()
	remaining := len(mgr.resources)
	mgr.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("Close left %d tenants in the resources map, want 0", remaining)
	}
}
