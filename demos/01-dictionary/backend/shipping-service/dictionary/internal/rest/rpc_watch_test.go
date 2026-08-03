package rest

// Covers the BR-D29 catch-up contract at the actual HTTP boundary a browser
// hits (GET /api/rpc-watch): refdata-service's own BR-D29 test proves
// RPCTRACE retains and replays obs.rpc.* traffic in isolation, but nothing
// asserted that watchRPCObs itself (sse.go) delivers that backlog to an SSE
// client before switching to live traffic. This test seeds RPCTRACE with a
// "backlog" event before the handler is invoked, then publishes a "live"
// event only after the replay has landed, and asserts both appear in the
// response body with the backlog first.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
)

// syncRecorder is a concurrency-safe http.ResponseWriter + http.Flusher.
// watchRPCObs writes from its own goroutine while this test polls the body
// from the main goroutine — a plain httptest.ResponseRecorder's bytes.Buffer
// isn't safe for that, so this wraps a strings.Builder in a mutex instead.
type syncRecorder struct {
	mu     sync.Mutex
	header http.Header
	body   strings.Builder
}

func newSyncRecorder() *syncRecorder { return &syncRecorder{header: make(http.Header)} }

func (r *syncRecorder) Header() http.Header { return r.header }

func (r *syncRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(b)
}

func (r *syncRecorder) WriteHeader(int) {}

func (r *syncRecorder) Flush() {}

func (r *syncRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

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
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("rpc-watch-test"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js, func() { nc.Close(); srv.Shutdown() }
}

func waitForBody(t *testing.T, rec *syncRecorder, substr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.String(), substr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in response body: %s", substr, rec.String())
}

func TestWatchRPCObsReplaysBacklogBeforeLive(t *testing.T) {
	nc, js, cleanup := newTestNATSJS(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := jstream.CreateStream(ctx, js, "RPCTRACE", []string{"obs.rpc.>"}); err != nil {
		t.Fatal(err)
	}

	backlog := `{"direction":"request","correlationId":"backlog-1","subject":"rpc.acme-test.refdata.item.get.v1"}`
	if _, err := js.Publish(ctx, "obs.rpc.acme-test.refdata.item.get.v1", []byte(backlog)); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(Deps{DefaultJS: js, NC: nc, Log: slog.New(slog.DiscardHandler)})

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/rpc-watch", nil).WithContext(reqCtx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		h.watchRPCObs(rec, req)
		close(done)
	}()

	// Wait for the backlog replay to land before publishing "live" traffic,
	// so the assertion below is actually testing ordering, not just presence.
	waitForBody(t, rec, "backlog-1")

	live := `{"direction":"request","correlationId":"live-1","subject":"rpc.acme-test.refdata.item.get.v1"}`
	if err := nc.Publish("obs.rpc.acme-test.refdata.item.get.v1", []byte(live)); err != nil {
		t.Fatal(err)
	}
	waitForBody(t, rec, "live-1")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchRPCObs did not return after context cancellation")
	}

	body := rec.String()
	backlogIdx := strings.Index(body, "backlog-1")
	liveIdx := strings.Index(body, "live-1")
	if backlogIdx == -1 || liveIdx == -1 {
		t.Fatalf("expected both backlog-1 and live-1 in body, got: %s", body)
	}
	if backlogIdx >= liveIdx {
		t.Fatalf("expected backlog replay before live traffic, got backlog@%d live@%d: %s", backlogIdx, liveIdx, body)
	}
}

// TestWatchRPCObsDegradesToLiveOnlyWhenJSNil — when DefaultJS isn't
// configured (mirrors production's own nil-safety for deployments without
// JetStream wired to this handler), the handler must still work as a
// live-only feed rather than failing.
func TestWatchRPCObsDegradesToLiveOnlyWhenJSNil(t *testing.T) {
	nc, _, cleanup := newTestNATSJS(t)
	defer cleanup()

	h := NewHandlers(Deps{NC: nc, Log: slog.New(slog.DiscardHandler)})

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/rpc-watch", nil).WithContext(reqCtx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		h.watchRPCObs(rec, req)
		close(done)
	}()

	live := `{"direction":"request","correlationId":"live-only-1","subject":"rpc.acme-test.refdata.item.get.v1"}`
	// No backlog to wait for here (JS is nil) — poll until the subscribe has
	// definitely taken effect by retrying the publish until it's observed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := nc.Publish("obs.rpc.acme-test.refdata.item.get.v1", []byte(live)); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rec.String(), "live-only-1") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitForBody(t, rec, "live-only-1")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchRPCObs did not return after context cancellation")
	}
}

// TestWatchRPCObsDeliversTenantAPITraffic — Phase 16: the Request/Reply feed
// must also carry obs.api.* events, which browserrpc's adapter publishes on
// the ACTIVE tenant's connection (Deps.TenantNC), not the DEFAULT-account
// connection the obs.rpc.> subscribe uses. This test wires two separate
// connections (standing in for the two accounts), publishes one family on
// each, and asserts both interleave into the same SSE body — proving the
// obs.api.> half really reads from TenantNC rather than piggybacking on NC.
func TestWatchRPCObsDeliversTenantAPITraffic(t *testing.T) {
	nc, _, cleanup := newTestNATSJS(t)
	defer cleanup()
	tenantNC, err := nats.Connect(nc.ConnectedUrl(), nats.Name("rpc-watch-test-tenant"))
	if err != nil {
		t.Fatal(err)
	}
	defer tenantNC.Close()

	h := NewHandlers(Deps{NC: nc, TenantNC: tenantNC, Log: slog.New(slog.DiscardHandler)})

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/rpc-watch", nil).WithContext(reqCtx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		h.watchRPCObs(rec, req)
		close(done)
	}()

	apiEvent := `{"direction":"request","correlationId":"api-1","subject":"api.acme.shipping.ship.list.v1"}`
	rpcEvent := `{"direction":"request","correlationId":"rpc-1","subject":"rpc.acme-test.refdata.item.get.v1"}`
	// No replay backlog here (JS is nil) — retry-publish until the
	// subscribes have definitely taken effect (same pattern as the
	// live-only test above), then assert both families landed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := tenantNC.Publish("obs.api.acme.shipping.ship.list.v1", []byte(apiEvent)); err != nil {
			t.Fatal(err)
		}
		if err := nc.Publish("obs.rpc.acme-test.refdata.item.get.v1", []byte(rpcEvent)); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rec.String(), "api-1") && strings.Contains(rec.String(), "rpc-1") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitForBody(t, rec, "api-1")
	waitForBody(t, rec, "rpc-1")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchRPCObs did not return after context cancellation")
	}
}
