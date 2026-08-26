package natsconn

import (
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(discardWriter{}, nil)) }

// The rule this package exists for: a service connection must survive NATS
// being away for longer than the library's default 60 attempts (~2 min).
func TestOptionsReconnectForever(t *testing.T) {
	opts := nats.GetDefaultOptions()
	for _, apply := range Options("some-service", "", testLogger()) {
		if err := apply(&opts); err != nil {
			t.Fatal(err)
		}
	}

	if opts.MaxReconnect >= 0 {
		t.Fatalf("MaxReconnect = %d, want negative (retry forever)", opts.MaxReconnect)
	}
	if opts.ReconnectWait <= 0 {
		t.Fatalf("ReconnectWait = %v, want a positive backoff", opts.ReconnectWait)
	}
	if opts.Name != "some-service" {
		t.Fatalf("Name = %q, want some-service", opts.Name)
	}
}

// Every connection in this repo must be identifiable in /connz.
func TestOptionsAlwaysNamesTheConnection(t *testing.T) {
	opts := nats.GetDefaultOptions()
	for _, apply := range Options("named", "", nil) {
		if err := apply(&opts); err != nil {
			t.Fatal(err)
		}
	}
	if opts.Name != "named" {
		t.Fatalf("Name = %q, want named", opts.Name)
	}
}

// And the behaviour behind the rule, against a real server that goes away for
// longer than one reconnect interval and comes back on the same address — the
// embedded stand-in for `docker compose restart nats`.
func TestReconnectsAfterServerRestart(t *testing.T) {
	srv := runServer(t, -1)
	port := srv.Addr().(*net.TCPAddr).Port
	url := srv.ClientURL()

	nc, err := nats.Connect(url, Options("restart-test", "", testLogger())...)
	if err != nil {
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
