// Package natsaccounts is Phase 18a's standalone isolation spike
// (.claude/plans/Main-POC-Plan.md, Phase 18): it is never imported by
// application code. It loads the repo's actual nats/nats.conf — the file
// shipped in docker-compose, not a reimplementation — into an embedded
// in-process server, so what's proven here is what the shipped config does.
package natsaccounts

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/gomega"
)

// natsConfPath is the shipped config, relative to this package's directory
// (Go's test runner sets the working directory there).
const natsConfPath = "../../../../nats/nats.conf"

// newSpikeServer loads nats/nats.conf into an embedded server, overriding
// only what would otherwise collide with a real docker-compose stack
// running on the same machine (fixed ports, the shared /data store dir).
// Everything relevant to isolation — accounts, users, no_auth_user,
// per-account jetstream — comes from the file unchanged.
func newSpikeServer(t *testing.T) (*server.Server, func()) {
	t.Helper()
	opts, err := server.ProcessConfigFile(natsConfPath)
	if err != nil {
		t.Fatalf("load %s: %v", natsConfPath, err)
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

func connectAs(t *testing.T, srv *server.Server, user, password string) *nats.Conn {
	t.Helper()
	opts := []nats.Option{nats.Name("phase18a-isolation-test")}
	if user != "" {
		opts = append(opts, nats.UserInfo(user, password))
	}
	nc, err := nats.Connect(srv.ClientURL(), opts...)
	if err != nil {
		t.Fatalf("connect as %q: %v", user, err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// TestNoAuthUserPreservesTodaysBehavior proves the additive claim: every
// existing service and frontend connects with zero credentials today
// (shipping-service/cmd/main.go:74, refdata-service/cmd/main.go:125), and
// must keep working unchanged once accounts exist.
func TestNoAuthUserPreservesTodaysBehavior(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	nc := connectAs(t, srv, "", "")
	g.Expect(nc.Opts.Name).NotTo(BeEmpty())

	js, err := jetstream.New(nc)
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "DEFAULT_SANITY",
		Subjects: []string{"spike.default.>"},
	})
	g.Expect(err).NotTo(HaveOccurred(), "an unauthenticated connection must still get full JetStream access via no_auth_user")

	received := make(chan struct{}, 1)
	sub, err := nc.Subscribe("spike.default.ping", func(*nats.Msg) { received <- struct{}{} })
	g.Expect(err).NotTo(HaveOccurred())
	defer sub.Unsubscribe()

	g.Expect(nc.Publish("spike.default.ping", []byte("hi"))).To(Succeed())
	g.Eventually(received).Should(Receive())
}

// TestWrongCredentialsRejected is a sanity check that credentials are
// actually enforced before trusting any isolation result below.
func TestWrongCredentialsRejected(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	_, err := nats.Connect(srv.ClientURL(), nats.UserInfo("acme", "not-the-real-password"))
	g.Expect(err).To(HaveOccurred())
}

// TestCoreNATSCrossAccountIsolation proves account isolation for plain
// pub/sub: per the NATS docs, this is enforced as invisibility (the subject
// space is not shared), not a rejected publish/subscribe call.
func TestCoreNATSCrossAccountIsolation(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	ncAcme := connectAs(t, srv, "acme", "acme-spike-pass")
	ncGlobex := connectAs(t, srv, "globex", "globex-spike-pass")

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

	ncAcme := connectAs(t, srv, "acme", "acme-spike-pass")
	ncGlobex := connectAs(t, srv, "globex", "globex-spike-pass")
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

// TestKVBucketIsolation mirrors the stream test for NATS KV — the demo's
// actual bucket-naming convention ({prefix}-{context}) collapses to just
// {prefix} once an account is the tenant boundary, which is exactly the
// taxonomy trade-off Phase 18 exists to document.
func TestKVBucketIsolation(t *testing.T) {
	g := NewWithT(t)
	srv, shutdown := newSpikeServer(t)
	defer shutdown()

	ncAcme := connectAs(t, srv, "acme", "acme-spike-pass")
	ncGlobex := connectAs(t, srv, "globex", "globex-spike-pass")
	jsAcme, err := jetstream.New(ncAcme)
	g.Expect(err).NotTo(HaveOccurred())
	jsGlobex, err := jetstream.New(ncGlobex)
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const bucket = "dict-a" // no {context} suffix needed: the account is the boundary

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
