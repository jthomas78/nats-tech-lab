package rest

// Shared test helpers for this package. newTestNATSJS was split out from the
// old rpc_watch_test.go when Phase 23 removed watchRPCObs (the last consumer
// of that file's syncRecorder/waitForBody SSE-body-polling helpers) along
// with the rest of this package's SSE handlers; discardLogger/discardWriter
// moved here from nats_ops_test.go when Phase 30h lifted the cross-account
// NATS/JetStream diagnostic handlers (and their tests) out to
// observability-service, leaving trace_middleware_test.go as the sole
// remaining consumer of both.

import (
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newTestNATSJS(t *testing.T) (*nats.Conn, jetstream.JetStream, func()) {
	t.Helper()
	opts := &server.Options{JetStream: true, StoreDir: t.TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("rest-test"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js, func() { nc.Close(); srv.Shutdown() }
}
