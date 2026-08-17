// Package natsaccounts is Phase 13a's standalone isolation spike
// (.claude/plans/Main-POC-Plan.md, Phase 13): it is never imported by
// application code. Phase 24a migrated it off the repo's checked-in
// nats/nats.conf and nats/creds/ artifacts onto a fully synthetic
// operator-mode server (see shipping_testserver_test.go), so the specs
// now stand alone from whatever bootstrap-operator.sh last produced.
package natsaccounts

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/gomega"
)

// TestPlatformCredsGetFullJetStreamAccess proves the PLATFORM account still
// gets everything shipping-service and refdata-service need for their
// permanent connection — full JetStream access and plain core pub/sub.
func TestPlatformCredsGetFullJetStreamAccess(t *testing.T) {
	g := NewWithT(t)
	s := newShippingTestServer(t)

	nc := s.connectAs(t, "platform")
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
// removal actually took effect: a connection presenting no credentials at
// all must be rejected — operator mode has no anonymous account.
func TestUnauthenticatedConnectionRejected(t *testing.T) {
	g := NewWithT(t)
	s := newShippingTestServer(t)

	_, err := nats.Connect(s.srv.ClientURL())
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
	s := newShippingTestServer(t)

	acmeCredsPath := s.credsPath("acme")
	real, err := os.ReadFile(acmeCredsPath)
	g.Expect(err).NotTo(HaveOccurred())
	tampered := tamperJWTSignature(t, real)
	tmp := t.TempDir() + "/bad.creds"
	g.Expect(os.WriteFile(tmp, tampered, 0o600)).To(Succeed())

	_, err = nats.Connect(s.srv.ClientURL(), nats.UserCredentials(tmp))
	g.Expect(err).To(HaveOccurred())
}

// tamperJWTSignature flips one base64 character well inside a .creds file's
// JWT signature segment (10 characters before the end — clear of the final
// character, which for a 64-byte ed25519 signature encodes only padding bits
// base64 decoders discard, so flipping it wouldn't actually change the
// decoded signature). Invalidates the signature without touching the seed or
// the file's overall structure.
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
	s := newShippingTestServer(t)

	ncAcme := s.connectAs(t, "acme")
	ncGlobex := s.connectAs(t, "globex")

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
	s := newShippingTestServer(t)

	ncAcme := s.connectAs(t, "acme")
	ncGlobex := s.connectAs(t, "globex")
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
// "ships", not "ships-{context}") — is what Phase 13 argued for, and it is
// now the actual implementation: kvstore.Store holds one bucket per prefix,
// and context is a key prefix inside that bucket ({context}.{entityType}.{id}).
// This test proves the NATS account boundary makes that design safe: two
// tenants with identical bucket names cannot see each other's data.
func TestKVBucketIsolation(t *testing.T) {
	g := NewWithT(t)
	s := newShippingTestServer(t)

	ncAcme := s.connectAs(t, "acme")
	ncGlobex := s.connectAs(t, "globex")
	jsAcme, err := jetstream.New(ncAcme)
	g.Expect(err).NotTo(HaveOccurred())
	jsGlobex, err := jetstream.New(ncGlobex)
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const bucket = "ships" // tenant-scoped: account boundary is the isolation layer

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
	s := newShippingTestServer(t)

	platform := s.connectAs(t, "platform")
	acme := s.connectAs(t, "acme")
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
	s := newShippingTestServer(t)

	acme := s.connectAs(t, "acme")
	globexAccountPub := s.accountPubKey("globex")
	_, err := acme.Request("rpc."+globexAccountPub+".refdata.item.get.v1", nil, 300*time.Millisecond)
	g.Expect(err).To(HaveOccurred(), "acme has no import for a caller-selected globex subject")
}

func TestTenantReceivesAccountLifecycleEventViaStreamImport(t *testing.T) {
	g := NewWithT(t)
	s := newShippingTestServer(t)

	platform := s.connectAs(t, "platform")
	acme := s.connectAs(t, "acme")
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
	s := newShippingTestServer(t)

	platform := s.connectAs(t, "platform")
	platformJS, err := jetstream.New(platform)
	g.Expect(err).NotTo(HaveOccurred())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = platformJS.CreateStream(ctx, jetstream.StreamConfig{Name: "REFDATA", Subjects: []string{"evt.*.refdata.>"}})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = platformJS.Publish(ctx, "evt.acme.refdata.item.changed", []byte("change"))
	g.Expect(err).NotTo(HaveOccurred())

	admin := s.connectAs(t, "shipping-admin")
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
