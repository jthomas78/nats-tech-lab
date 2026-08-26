package main

import (
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// shared/natsconn owns the policy itself and pins it (natsconn_test.go). What
// this service still asserts for itself is that it actually USES that policy:
// a real server that goes away for longer than one reconnect interval and
// comes back on the same address — the embedded stand-in for
// `docker compose restart nats` — must not leave this process closed.
func TestReconnectsAfterServerRestart(t *testing.T) {
	srv := runServer(t, -1)
	port := srv.Addr().(*net.TCPAddr).Port
	url := srv.ClientURL()

	// Through this service's own connect path, not a hand-built option set —
	// the point is that waitForNATS wires in the shared policy.
	var nc *nats.Conn
	if err := waitForNATS(t.Context(), url, "", testLogger(), func(conn *nats.Conn) error {
		nc = conn
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	reconnected := make(chan struct{}, 1)
	nc.SetReconnectHandler(func(*nats.Conn) { reconnected <- struct{}{} })

	srv.Shutdown()
	srv.WaitForShutdown()
	time.Sleep(100 * time.Millisecond)

	runServer(t, port)

	select {
	case <-reconnected:
	case <-time.After(30 * time.Second):
		t.Fatal("client never reconnected after the server came back")
	}
	if !nc.IsConnected() {
		t.Fatal("connection not usable after reconnect")
	}
}

func runServer(t *testing.T, port int) *server.Server {
	t.Helper()
	srv, err := server.NewServer(&server.Options{JetStream: true, StoreDir: t.TempDir(), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv
}
