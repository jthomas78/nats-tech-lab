package natstenants

// Contract tests for shared/natstenants (BR-D40), extracted in Phase 35 from
// four near-identical per-service copies (see tenants.go's package doc) —
// refdata-service's own tenants_test.go had the only embedded-NATS-server
// coverage of these four; this generalizes it to the shared package's own
// resource-type parameter so every consumer benefits, not just refdata.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
// shared/natstrace's own newTestConn helper. No auth configured means the
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

// testResource is a minimal stand-in for a service's own per-tenant
// resource bundle (a browserrpc.Adapter in the real services) — just enough
// to prove provision/deprovision run at the right times.
type testResource struct {
	tenant  string
	stopped bool
	extra   string
}

func newManager(t *testing.T, srv *natsserver.Server, credsDir string) (*Manager[*testResource], *int32) {
	t.Helper()
	var provisionCount int32
	mgr := NewManager(srv.ClientURL(), credsDir, "test-service", nil,
		func(_ context.Context, nc *nats.Conn, tenant string) (*testResource, error) {
			atomic.AddInt32(&provisionCount, 1)
			return &testResource{tenant: tenant}, nil
		},
		func(_ string, res *testResource) error {
			res.stopped = true
			return nil
		},
	)
	t.Cleanup(mgr.Close)
	return mgr, &provisionCount
}

// --- Discover ---

func TestDiscoverExcludesNonTenantCredsFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"platform", "shipping-admin", "sys", "observability", "mfe-registry-service", "acme"} {
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
	for _, excluded := range []string{"platform", "shipping-admin", "sys", "observability", "mfe-registry-service"} {
		if _, ok := found[excluded]; ok {
			t.Errorf("Discover treated %q as a tenant — NonTenantCredsFiles is not excluding it", excluded)
		}
	}
}

func TestNatstenantsGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NATS Tenants Business Rules Suite")
}

var _ = Describe("Tenant credential discovery", func() {
	Context("BR-D40 — plugin credentials are excluded by directory, never by name", func() {
		It("does not descend into the plugins credential family directory", func() {
			dir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(dir, "acme.creds"), []byte("x"), 0o600)).To(Succeed())
			plugins := filepath.Join(dir, "plugins")
			Expect(os.Mkdir(plugins, 0o700)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(plugins, "example-plugin.creds"), []byte("x"), 0o600)).To(Succeed())

			found, err := Discover(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(HaveKey("acme"))
			Expect(found).To(HaveLen(1))
			Expect(NonTenantCredsSuffixes).To(BeEmpty())
		})

		It("still treats a top-level plugin-shaped credential as a tenant", func() {
			dir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(dir, "example-plugin.creds"), []byte("x"), 0o600)).To(Succeed())

			found, err := Discover(dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(HaveKey("example-plugin"))
		})
	})
})

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

func TestEnsureAllConnectsWithServiceNameAndProvisionsResource(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	mgr, _ := newManager(t, srv, dir)

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}

	res, ok := mgr.Resource("acme")
	if !ok {
		t.Fatal("EnsureAll did not provision a resource for acme")
	}
	if res.tenant != "acme" {
		t.Errorf("provisioned resource tenant = %q, want %q", res.tenant, "acme")
	}

	mgr.mu.RLock()
	nc := mgr.resources["acme"].nc
	mgr.mu.RUnlock()
	// CLAUDE.md: every nats.Connect call must set nats.Name — asserted here
	// so a caller distinguishes this connection by account, not by name.
	if got := nc.Opts.Name; got != "test-service" {
		t.Errorf("tenant connection nats.Name = %q, want %q", got, "test-service")
	}
}

func TestEnsureByNameNoOpWhenTenantNotYetVisible(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir() // empty — "missing" is not (yet) discoverable

	mgr, _ := newManager(t, srv, dir)

	if err := mgr.EnsureByName(context.Background(), "missing"); err != nil {
		t.Fatalf("EnsureByName on an undiscoverable tenant must be a no-op, not an error: %v", err)
	}
	if _, ok := mgr.Resource("missing"); ok {
		t.Error("EnsureByName provisioned a resource for a tenant that was never discoverable")
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	mgr, provisionCount := newManager(t, srv, dir)

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}
	first, _ := mgr.Resource("acme")

	// A second Ensure (e.g. EnsureByName reacting to a duplicate/racing
	// notify.accounts.account.created delivery) must return the exact same
	// resource, not reconnect or re-provision.
	if err := mgr.EnsureByName(context.Background(), "acme"); err != nil {
		t.Fatalf("EnsureByName (second call): %v", err)
	}
	second, _ := mgr.Resource("acme")
	if first != second {
		t.Error("a second Ensure for an already-provisioned tenant created a new resource instead of returning the existing one")
	}
	if atomic.LoadInt32(provisionCount) != 1 {
		t.Errorf("provision called %d times, want 1", atomic.LoadInt32(provisionCount))
	}
}

func TestTeardownByNameIsIdempotentClosesAndDeprovisions(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	mgr, _ := newManager(t, srv, dir)

	// Never provisioned — must be a no-op, not an error (BR-D40).
	if err := mgr.TeardownByName(context.Background(), "never-seen"); err != nil {
		t.Fatalf("TeardownByName on an unprovisioned tenant must be a no-op: %v", err)
	}

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}
	res, _ := mgr.Resource("acme")
	mgr.mu.RLock()
	nc := mgr.resources["acme"].nc
	mgr.mu.RUnlock()

	if err := mgr.TeardownByName(context.Background(), "acme"); err != nil {
		t.Fatalf("TeardownByName: %v", err)
	}
	if _, stillPresent := mgr.Resource("acme"); stillPresent {
		t.Error("TeardownByName left the tenant in the resources map")
	}
	if !nc.IsClosed() {
		t.Error("TeardownByName did not close the tenant's connection")
	}
	if !res.stopped {
		t.Error("TeardownByName did not call deprovision on the tenant's resource")
	}

	// Idempotent: tearing down an already-torn-down tenant is a no-op too.
	if err := mgr.TeardownByName(context.Background(), "acme"); err != nil {
		t.Fatalf("second TeardownByName must also be a no-op: %v", err)
	}
}

func TestCloseDeprovisionsAndClosesEveryTenant(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")
	writeTestCreds(t, dir, "globex")

	mgr, _ := newManager(t, srv, dir)
	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}

	acme, _ := mgr.Resource("acme")
	globex, _ := mgr.Resource("globex")
	mgr.mu.RLock()
	conns := []*nats.Conn{mgr.resources["acme"].nc, mgr.resources["globex"].nc}
	mgr.mu.RUnlock()

	mgr.Close()

	for i, nc := range conns {
		if !nc.IsClosed() {
			t.Errorf("connection %d not closed after Manager.Close()", i)
		}
	}
	if !acme.stopped || !globex.stopped {
		t.Error("Close did not deprovision every tenant's resource")
	}
	mgr.mu.RLock()
	remaining := len(mgr.resources)
	mgr.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("Close left %d tenants in the resources map, want 0", remaining)
	}
}

func TestRangeVisitsEveryTenantWithNoLockHeld(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")
	writeTestCreds(t, dir, "globex")

	mgr, _ := newManager(t, srv, dir)
	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}

	seen := map[string]bool{}
	mgr.Range(func(tenant string, nc *nats.Conn, res *testResource) {
		seen[tenant] = true
		// Proves Range holds no lock while calling fn: a call back into
		// Manager from inside fn (Resource takes RLock) would deadlock on a
		// non-reentrant sync.RWMutex if Range still held it here.
		if _, ok := mgr.Resource(tenant); !ok {
			t.Errorf("Resource(%q) unavailable from inside Range's callback", tenant)
		}
	})
	if len(seen) != 2 || !seen["acme"] || !seen["globex"] {
		t.Errorf("Range visited %v, want both acme and globex", seen)
	}
}

func TestUpdateReplacesResourceInPlace(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	mgr, _ := newManager(t, srv, dir)
	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}

	err := mgr.Update("acme", func(nc *nats.Conn, cur *testResource) (*testResource, error) {
		cur.extra = "mounted"
		return cur, nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	res, _ := mgr.Resource("acme")
	if res.extra != "mounted" {
		t.Errorf("Update did not apply fn's result, extra = %q", res.extra)
	}
}

func TestUpdateNoOpForUnconnectedTenant(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()

	mgr, _ := newManager(t, srv, dir)

	called := false
	err := mgr.Update("missing", func(nc *nats.Conn, cur *testResource) (*testResource, error) {
		called = true
		return cur, nil
	})
	if err != nil {
		t.Fatalf("Update on an unconnected tenant must be a no-op, not an error: %v", err)
	}
	if called {
		t.Error("Update called fn for a tenant with no connection")
	}
}

// --- SubscribeLifecycle ---

func TestSubscribeLifecycleDispatchesCreatedSuspendedReactivated(t *testing.T) {
	srv := newTestServer(t)
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("test"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	var mu sync.Mutex
	var ensured, torndown []string

	h := LifecycleHandlers{
		Ensure: func(_ context.Context, tenant string) error {
			mu.Lock()
			ensured = append(ensured, tenant)
			mu.Unlock()
			return nil
		},
		Teardown: func(_ context.Context, tenant string) error {
			mu.Lock()
			torndown = append(torndown, tenant)
			mu.Unlock()
			return nil
		},
	}
	if err := SubscribeLifecycle(context.Background(), nc, nil, h); err != nil {
		t.Fatalf("SubscribeLifecycle: %v", err)
	}

	pub, err := nats.Connect(srv.ClientURL(), nats.Name("publisher"))
	if err != nil {
		t.Fatalf("connect publisher: %v", err)
	}
	defer pub.Close()

	if err := pub.Publish("notify.accounts.account.created", []byte(`{"name":"acme"}`)); err != nil {
		t.Fatalf("publish created: %v", err)
	}
	if err := pub.Publish("notify.accounts.account.reactivated", []byte(`{"name":"globex"}`)); err != nil {
		t.Fatalf("publish reactivated: %v", err)
	}
	if err := pub.Publish("notify.accounts.account.suspended", []byte(`{"name":"acme"}`)); err != nil {
		t.Fatalf("publish suspended: %v", err)
	}
	if err := pub.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		done := len(ensured) == 2 && len(torndown) == 1
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for lifecycle dispatch: ensured=%v torndown=%v", ensured, torndown)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if !(contains(ensured, "acme") && contains(ensured, "globex")) {
		t.Errorf("ensured = %v, want both acme (created) and globex (reactivated)", ensured)
	}
	if !contains(torndown, "acme") {
		t.Errorf("torndown = %v, want acme (suspended)", torndown)
	}
}

func TestSubscribeLifecycleFailsSpanWithoutBlockingOnHandlerError(t *testing.T) {
	srv := newTestServer(t)
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("test"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	called := make(chan struct{}, 1)
	h := LifecycleHandlers{
		Ensure: func(_ context.Context, tenant string) error {
			called <- struct{}{}
			return errors.New("boom")
		},
		Teardown: func(_ context.Context, tenant string) error { return nil },
	}
	if err := SubscribeLifecycle(context.Background(), nc, nil, h); err != nil {
		t.Fatalf("SubscribeLifecycle: %v", err)
	}

	pub, err := nats.Connect(srv.ClientURL(), nats.Name("publisher"))
	if err != nil {
		t.Fatalf("connect publisher: %v", err)
	}
	defer pub.Close()
	if err := pub.Publish("notify.accounts.account.created", []byte(`{"name":"acme"}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := pub.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("Ensure handler was never invoked for a failing call — an error must not block dispatch")
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
