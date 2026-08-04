// Package natsaccounts is Phase 13a's standalone isolation spike
// (.claude/plans/Main-POC-Plan.md, Phase 13): it is never imported by
// application code. It loads the repo's actual nats/nats.conf — the file
// shipped in docker-compose, not a reimplementation — into an embedded
// in-process server, so what's proven here is what the shipped config does.
package natsaccounts

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/gomega"
)

// natsDir is the shipped nats/ directory, relative to this package's
// directory (Go's test runner sets the working directory there).
const natsDir = "../../../../nats"

// credsPath resolves one of nats/creds/*.creds, minted by
// bootstrap-operator.sh — these are checked into the repo (spike-only,
// never production credentials; see that script's header).
func credsPath(name string) string {
	return filepath.Join(natsDir, "creds", name+".creds")
}

// accountPublicKeyFromCreds returns the tenant account identity carried by a
// checked-in user JWT. Account-token exports stamp this value at token 2;
// it is intentionally not a caller-controlled company context.
func accountPublicKeyFromCreds(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(credsPath(name))
	if err != nil {
		t.Fatalf("read %s creds: %v", name, err)
	}
	line := regexp.MustCompile(`(?m)^eyJ[^\n]*$`).Find(raw)
	if len(line) == 0 {
		t.Fatalf("find user JWT in %s creds", name)
	}
	claims, err := jwt.DecodeUserClaims(string(line))
	if err != nil {
		t.Fatalf("decode %s user JWT: %v", name, err)
	}
	if claims.IssuerAccount != "" {
		return claims.IssuerAccount
	}
	// nsc's static service users are signed directly by their account key;
	// in that normal case IssuerAccount is omitted and Issuer is the account.
	return claims.Issuer
}

var resolverDirRE = regexp.MustCompile(`dir:\s*"[^"]*"`)

// newSpikeServer loads the shipped nats/nats.conf into an embedded server —
// the file docker-compose actually mounts, not a reimplementation — proving
// what's tested here is what's shipped. Two absolute paths in the checked-in
// file are docker-only (/etc/nats/operator.jwt, /data/jwt's resolver dir),
// so this rewrites just those two before parsing; every account, JetStream
// limit, and resolver_preload JWT comes from the file unchanged.
func newSpikeServer(t *testing.T) (*server.Server, func()) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(natsDir, "nats.conf"))
	if err != nil {
		t.Fatalf("read nats.conf: %v", err)
	}
	operatorPath, err := filepath.Abs(filepath.Join(natsDir, "operator.jwt"))
	if err != nil {
		t.Fatalf("resolve operator.jwt path: %v", err)
	}
	rewritten := regexp.MustCompile(`operator:\s*\S+`).ReplaceAll(raw, []byte("operator: "+operatorPath))
	rewritten = resolverDirRE.ReplaceAll(rewritten, []byte(`dir: "`+t.TempDir()+`"`))

	confPath := filepath.Join(t.TempDir(), "nats.conf")
	if err := os.WriteFile(confPath, rewritten, 0o600); err != nil {
		t.Fatalf("write rewritten nats.conf: %v", err)
	}

	opts, err := server.ProcessConfigFile(confPath)
	if err != nil {
		t.Fatalf("load rewritten nats.conf: %v", err)
	}
	opts.Port = -1
	opts.HTTPPort = 0
	opts.Websocket.Port = 0
	opts.StoreDir = t.TempDir()

	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	return srv, srv.Shutdown
}

// connectAs dials srv authenticated by the named account's .creds file
// (e.g. "acme", "globex", "platform") — empty name dials with no credentials,
// which operator mode always rejects (Phase 14a removed no_auth_user).
func connectAs(t *testing.T, srv *server.Server, name string) *nats.Conn {
	t.Helper()
	opts := []nats.Option{nats.Name("phase13a-isolation-test")}
	if name != "" {
		opts = append(opts, nats.UserCredentials(credsPath(name)))
	}
	nc, err := nats.Connect(srv.ClientURL(), opts...)
	if err != nil {
		t.Fatalf("connect as %q: %v", name, err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// TestPlatformCredsGetFullJetStreamAccess proves the PLATFORM account (Phase
// 14a's replacement for Phase 13a's no_auth_user) still gets everything
// shipping-service and refdata-service need for their permanent connection
// (shipping-service/cmd/main.go, refdata-service/cmd/main.go) — full
// JetStream access and plain core pub/sub — just authenticated by a .creds
// file now instead of connecting with zero credentials.
func TestPlatformCredsGetFullJetStreamAccess(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	nc := connectAs(t, srv, "platform")
	g.Expect(nc.Opts.Name).NotTo(BeEmpty())

	js, err := jetstream.New(nc)
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "PLATFORM_SANITY",
		Subjects: []string{"spike.platform.>"},
	})
	g.Expect(err).NotTo(HaveOccurred(), "the PLATFORM account's creds must still get full JetStream access")

	received := make(chan struct{}, 1)
	sub, err := nc.Subscribe("spike.platform.ping", func(*nats.Msg) { received <- struct{}{} })
	g.Expect(err).NotTo(HaveOccurred())
	defer sub.Unsubscribe()

	g.Expect(nc.Publish("spike.platform.ping", []byte("hi"))).To(Succeed())
	g.Eventually(received).Should(Receive())
}

// TestUnauthenticatedConnectionRejected proves Phase 14a's no_auth_user
// removal actually took effect: unlike Phase 13a, a connection presenting no
// credentials at all must now be rejected — operator mode has no anonymous
// account.
func TestUnauthenticatedConnectionRejected(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	_, err := nats.Connect(srv.ClientURL())
	g.Expect(err).To(HaveOccurred(), "operator mode must reject a connection with no credentials — no_auth_user no longer exists")
}

// TestWrongCredentialsRejected is a sanity check that credentials are
// actually enforced before trusting any isolation result below. Takes a
// real, valid .creds file and flips one character in its JWT so the
// signature no longer verifies — the seed (and so the file's syntax) stays
// valid, isolating the assertion to "the server rejects a bad JWT signature"
// rather than "a malformed file fails to parse locally."
func TestWrongCredentialsRejected(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	real, err := os.ReadFile(credsPath("acme"))
	g.Expect(err).NotTo(HaveOccurred())
	tampered := tamperJWTSignature(t, real)
	tmp := filepath.Join(t.TempDir(), "bad.creds")
	g.Expect(os.WriteFile(tmp, tampered, 0o600)).To(Succeed())

	_, err = nats.Connect(srv.ClientURL(), nats.UserCredentials(tmp))
	g.Expect(err).To(HaveOccurred())
}

// tamperJWTSignature flips one base64 character well inside a .creds file's
// JWT signature segment (10 characters before the end — clear of the final
// character, which for a 64-byte ed25519 signature encodes only padding
// bits base64 decoders discard, so flipping it wouldn't actually change the
// decoded signature). Invalidates the signature without touching the seed
// or the file's overall structure.
func tamperJWTSignature(t *testing.T, creds []byte) []byte {
	t.Helper()
	s := string(creds)
	lineEnd := regexp.MustCompile(`(?m)^eyJ[^\n]*$`).FindStringIndex(s)
	if lineEnd == nil {
		t.Fatal("could not locate JWT line in creds file")
	}
	line := s[lineEnd[0]:lineEnd[1]]
	pos := len(line) - 10
	swap := byte('A')
	if line[pos] == 'A' {
		swap = 'B'
	}
	tamperedLine := line[:pos] + string(swap) + line[pos+1:]
	return []byte(s[:lineEnd[0]] + tamperedLine + s[lineEnd[1]:])
}

// TestCoreNATSCrossAccountIsolation proves account isolation for plain
// pub/sub: per the NATS docs, this is enforced as invisibility (the subject
// space is not shared), not a rejected publish/subscribe call.
func TestCoreNATSCrossAccountIsolation(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	ncAcme := connectAs(t, srv, "acme")
	ncGlobex := connectAs(t, srv, "globex")

	received := make(chan *nats.Msg, 1)
	sub, err := ncAcme.Subscribe("evt.shared-subject.shipping.ship.SHIP1.arrived", func(m *nats.Msg) { received <- m })
	g.Expect(err).NotTo(HaveOccurred())
	defer sub.Unsubscribe()
	g.Expect(ncAcme.Flush()).To(Succeed())

	g.Expect(ncGlobex.Publish("evt.shared-subject.shipping.ship.SHIP1.arrived", []byte("globex-event"))).To(Succeed())
	g.Expect(ncGlobex.Flush()).To(Succeed())

	g.Consistently(received, 300*time.Millisecond).ShouldNot(Receive(),
		"acme must never see a message published by globex on the identical subject string — accounts are separate subject spaces, not a shared one filtered by prefix")
}

// TestJetStreamStreamIsolation proves JetStream streams are per-account
// resources: the identical stream name in two accounts is two independent
// streams, and neither account can see the other's.
func TestJetStreamStreamIsolation(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	ncAcme := connectAs(t, srv, "acme")
	ncGlobex := connectAs(t, srv, "globex")
	jsAcme, err := jetstream.New(ncAcme)
	g.Expect(err).NotTo(HaveOccurred())
	jsGlobex, err := jetstream.New(ncGlobex)
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Same stream name, same subject, deliberately, in both accounts.
	_, err = jsAcme.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "SPIKE_ISO",
		Subjects: []string{"spike.iso.>"},
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = jsGlobex.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "SPIKE_ISO",
		Subjects: []string{"spike.iso.>"},
	})
	g.Expect(err).NotTo(HaveOccurred(), "an identical stream name in a different account must not collide with acme's")

	acmeMsg := []byte("acme-only")
	_, err = jsAcme.Publish(ctx, "spike.iso.marker", acmeMsg)
	g.Expect(err).NotTo(HaveOccurred())

	acmeStream, err := jsAcme.Stream(ctx, "SPIKE_ISO")
	g.Expect(err).NotTo(HaveOccurred())
	info, err := acmeStream.Info(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(info.State.Msgs).To(BeEquivalentTo(1), "acme's own stream should hold exactly the message acme published")

	globexStream, err := jsGlobex.Stream(ctx, "SPIKE_ISO")
	g.Expect(err).NotTo(HaveOccurred())
	globexInfo, err := globexStream.Info(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(globexInfo.State.Msgs).To(BeEquivalentTo(0), "globex's identically-named stream must not contain acme's message")
}

// TestKVBucketIsolation mirrors the stream test for NATS KV. The demo's
// shipped bucket-naming convention — one bucket per role per tenant (e.g.
// "dict-a", not "dict-a-{context}") — is what Phase 13 argued for, and it is
// now the actual implementation: kvstore.Store holds one bucket per prefix,
// and context is a key prefix inside that bucket ({context}.{entityType}.{id}).
// This test proves the NATS account boundary makes that design safe: two
// tenants with identical bucket names cannot see each other's data.
func TestKVBucketIsolation(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	ncAcme := connectAs(t, srv, "acme")
	ncGlobex := connectAs(t, srv, "globex")
	jsAcme, err := jetstream.New(ncAcme)
	g.Expect(err).NotTo(HaveOccurred())
	jsGlobex, err := jetstream.New(ncGlobex)
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const bucket = "dict-a" // tenant-scoped: account boundary is the isolation layer

	kvAcme, err := jsAcme.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = kvAcme.Put(ctx, "port.SGSIN", []byte(`{"name":"Singapore"}`))
	g.Expect(err).NotTo(HaveOccurred())

	_, err = jsGlobex.KeyValue(ctx, bucket)
	g.Expect(err).To(HaveOccurred(), "globex must not see a bucket that only exists in acme's account, even with an identical name")

	kvGlobex, err := jsGlobex.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket})
	g.Expect(err).NotTo(HaveOccurred(), "globex must be able to create its own bucket of the same name without colliding with acme's")

	_, err = kvGlobex.Get(ctx, "port.SGSIN")
	g.Expect(err).To(HaveOccurred(), "globex's freshly created bucket must not already contain acme's key")
}

func TestTenantRefdataServiceImportStampsItsAccountIdentity(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	platform := connectAs(t, srv, "platform")
	acme := connectAs(t, srv, "acme")
	seen := make(chan string, 1)
	sub, err := platform.Subscribe("rpc.*.refdata.item.get.v1", func(msg *nats.Msg) {
		seen <- msg.Subject
		_ = msg.Respond([]byte("resolved"))
	})
	g.Expect(err).NotTo(HaveOccurred())
	defer sub.Unsubscribe()
	g.Expect(platform.Flush()).To(Succeed())

	msg, err := acme.Request("refdata.item.get.v1", []byte(`{"context":"globex"}`), time.Second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(msg.Data)).To(Equal("resolved"))
	g.Eventually(seen).Should(Receive(Equal("rpc.acme.refdata.item.get.v1")))
}

func TestTenantCannotUseOldStyleCrossContextRefdataSubject(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	acme := connectAs(t, srv, "acme")
	globexPub := accountPublicKeyFromCreds(t, "globex")
	_, err := acme.Request("rpc."+globexPub+".refdata.item.get.v1", nil, 300*time.Millisecond)
	g.Expect(err).To(HaveOccurred(), "acme has no import for a caller-selected globex subject")
}

func TestTenantReceivesAccountLifecycleEventViaStreamImport(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	platform := connectAs(t, srv, "platform")
	acme := connectAs(t, srv, "acme")
	sub, err := acme.SubscribeSync("notify.accounts.account.created")
	g.Expect(err).NotTo(HaveOccurred())
	defer sub.Unsubscribe()
	g.Expect(acme.Flush()).To(Succeed())
	g.Expect(platform.Publish("notify.accounts.account.created", []byte(`{"name":"runtime-tenant"}`))).To(Succeed())
	g.Expect(platform.Flush()).To(Succeed())
	msg, err := sub.NextMsg(time.Second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(msg.Data)).To(MatchJSON(`{"name":"runtime-tenant"}`))
}

func TestShippingAdminCanOnlyUseNarrowOrderedConsumerAccess(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	platform := connectAs(t, srv, "platform")
	platformJS, err := jetstream.New(platform)
	g.Expect(err).NotTo(HaveOccurred())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = platformJS.CreateStream(ctx, jetstream.StreamConfig{Name: "REFDATA", Subjects: []string{"evt.*.refdata.>"}})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = platformJS.Publish(ctx, "evt.acme.refdata.item.changed", []byte("change"))
	g.Expect(err).NotTo(HaveOccurred())

	admin := connectAs(t, srv, "shipping-admin")
	adminJS, err := jetstream.New(admin)
	g.Expect(err).NotTo(HaveOccurred())
	consumer, err := adminJS.OrderedConsumer(ctx, "REFDATA", jetstream.OrderedConsumerConfig{DeliverPolicy: jetstream.DeliverAllPolicy})
	g.Expect(err).NotTo(HaveOccurred(), "shipping-admin needs ordered consumer access to REFDATA")
	msgs, err := consumer.Messages()
	g.Expect(err).NotTo(HaveOccurred())
	defer msgs.Stop()
	msg, err := msgs.Next()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(msg.Data())).To(Equal("change"))

	_, err = adminJS.CreateStream(ctx, jetstream.StreamConfig{Name: "FORBIDDEN", Subjects: []string{"forbidden.>"}})
	g.Expect(err).To(HaveOccurred(), "shipping-admin must not receive blanket JetStream administration")
}
