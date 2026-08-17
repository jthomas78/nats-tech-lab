package rest

// Shared embedded-NATS test helper for kv_test.go, streams_test.go,
// replay_test.go — JetStream enabled, single account (no operator-mode/
// export-import wiring, unlike accounts-service's operatorTestServer). That
// means these tests can fully exercise the "platform" account path (default
// $JS.API prefix — identical in test and production) and the client-side
// half of the tenant path (does introspectableAccounts/jsForAccount
// correctly construct a distinct jetstream.NewWithAPIPrefix client per
// tenant name), but NOT the server-side half of BR-AC32's cross-account
// $JS.API import resolution itself — that needs a real multi-account
// export/import deployment and is exercised at Phase 30i's live
// docker-compose verification instead, the same place BR-AC31/32's
// CONSUMER.MSG.NEXT multi-reply mechanism risk is deferred to.
import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func newTestNATS(t *testing.T) (*nats.Conn, func()) {
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
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("observability-rest-test"))
	if err != nil {
		t.Fatal(err)
	}
	return nc, func() { nc.Close(); srv.Shutdown() }
}
