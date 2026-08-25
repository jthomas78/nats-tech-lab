// Package natstest is the embedded-NATS bootstrap the observability specs
// share.
//
// Phase 43 grew four near-identical copies of this (newTestNATS,
// newObservabilityTestNATS + subscribeObservations, runEmbeddedNATS), each
// starting a server on a random port, waiting for readiness and returning a
// named connection plus a cleanup. They differ only in whether they enable
// JetStream and what they name the connection. This is that shape, once.
//
// Phase 43e promoted it from shared/natsnotify/natstest to a module of its
// own, because shared/jstream's specs need it too and a test helper that
// lives inside one seam's module cannot be reached from a sibling seam's.
// The remaining hand-rolled copies (organizations, accounts) migrate onto it
// opportunistically rather than in the commit that moved it.
package natstest

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Option configures the embedded server Start runs.
type Option func(*server.Options, *testing.T)

// WithJetStream enables JetStream on the embedded server, backed by a store
// directory scoped to the test. Off by default: notify.* is core NATS, and a
// spec that needs a stream should say so rather than have every spec pay for
// a store directory.
func WithJetStream() Option {
	return func(o *server.Options, t *testing.T) {
		o.JetStream = true
		o.StoreDir = t.TempDir()
	}
}

// Start runs an embedded NATS server for the life of the test and returns a
// connection to it.
//
// The connection is named, because an anonymous one is indistinguishable in
// `nats server list connections` — the same rule the services follow.
func Start(t *testing.T, name string, opts ...Option) *nats.Conn {
	t.Helper()
	sopts := &server.Options{Port: -1}
	for _, opt := range opts {
		opt(sopts, t)
	}
	srv, err := server.NewServer(sopts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		srv.Shutdown()
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.Name(name))
	if err != nil {
		srv.Shutdown()
		t.Fatal(err)
	}
	t.Cleanup(func() { nc.Close(); srv.Shutdown() })
	return nc
}

// StartJetStream runs an embedded JetStream-enabled server and returns both
// the connection and a JetStream context on it — the pair every evt.* spec
// opens with.
func StartJetStream(t *testing.T, name string) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	nc := Start(t, name, WithJetStream())
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js
}

// Subscribe collects messages on subject for the life of the test. The
// channel is buffered, so a spec that asserts on the first message does not
// deadlock the ones behind it.
func Subscribe(t *testing.T, nc *nats.Conn, subject string) chan *nats.Msg {
	t.Helper()
	msgs := make(chan *nats.Msg, 16)
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) { msgs <- m })
	if err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return msgs
}

// Observations collects the BR-045 obs.pubsub.* envelopes.
func Observations(t *testing.T, nc *nats.Conn) chan *nats.Msg {
	t.Helper()
	return Subscribe(t, nc, "obs.pubsub.>")
}
