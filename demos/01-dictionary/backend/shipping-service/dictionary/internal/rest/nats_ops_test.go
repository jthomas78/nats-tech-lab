package rest

// Phase 17c — Connections + Services admin panels. Connections proxies a
// mocked /connz (no real NATS server needed, just the reshaping/sort
// contract); Services exercises the real $SRV.STATS broadcast protocol
// against an embedded NATS server with an actual micro.AddService instance,
// since that fan-in collection logic is the real risk (timing, dedup across
// two connections to the same account).

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// ─── Connections ──────────────────────────────────────────────────────────

func TestListNatsConnectionsReshapesAndSortsConnz(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("subs"); got != "true" {
			t.Errorf("expected subs=true query param, got %q", got)
		}
		if got := r.URL.Query().Get("auth"); got != "true" {
			t.Errorf("expected auth=true query param, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(connzResponse{Connections: []connzConnection{
			{CID: 2, Type: "websocket", Name: "", Lang: "nats.ws", Version: "3.4.0", Account: "ACME", RTT: "1ms", Uptime: "1m", Idle: "1m"},
			{CID: 1, Type: "nats", Name: "refdata-service", Lang: "go", Version: "1.52.0", Account: "DEFAULT", RTT: "300µs", Uptime: "56m", Idle: "10s", InMsgs: 208, OutMsgs: 560, Subscriptions: 16, SubscriptionsList: []string{"rpc.*.refdata.type.list.v1"}},
		}})
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/connections", nil)
	w := httptest.NewRecorder()

	h.listNatsConnections(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body natsConnectionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(body.Connections))
	}
	// Sorted by CID ascending, not the mock's declaration order.
	if body.Connections[0].CID != 1 || body.Connections[1].CID != 2 {
		t.Fatalf("expected connections sorted by cid [1,2], got [%d,%d]", body.Connections[0].CID, body.Connections[1].CID)
	}
	refdata := body.Connections[0]
	if refdata.Name != "refdata-service" || refdata.Type != "nats" || refdata.Account != "DEFAULT" {
		t.Fatalf("unexpected reshaping of refdata-service connection: %+v", refdata)
	}
	if refdata.InMsgs != 208 || refdata.Subscriptions != 16 || len(refdata.SubscriptionsList) != 1 {
		t.Fatalf("unexpected counters/subs on refdata-service connection: %+v", refdata)
	}
	ws := body.Connections[1]
	if ws.Type != "websocket" || ws.Lang != "nats.ws" {
		t.Fatalf("unexpected reshaping of websocket connection: %+v", ws)
	}
}

// TestListNatsConnectionsLabelsAnyConnectionSharingAKnownAccount is why
// tenantLabelsByAccount is two stages, not one: it opens two REAL
// connections (so LocalAddr() is a real ephemeral port, not a fabricated
// value) and has the mocked /connz report those exact addresses back to
// establish "this account NKey means DEFAULT / means acme" — then proves
// that mapping is applied by ACCOUNT to a fourth connz row (simulating
// refdata-service: same DEFAULT account, but an address this process never
// held, i.e. a connection it doesn't own) — the whole point being that
// refdata-service and the nats CLI, sharing DEFAULT with shipping-service's
// own connection, get labeled too. A fifth, truly unrelated account proves
// connections on a genuinely unknown account (accounts-service's SYS
// account) are still left unlabeled rather than mismatched.
func TestListNatsConnectionsLabelsAnyConnectionSharingAKnownAccount(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	tenantNC, err := nats.Connect(nc.ConnectedUrl(), nats.Name("shipping-service"))
	if err != nil {
		t.Fatalf("tenant connection: %v", err)
	}
	defer tenantNC.Close()

	defaultIP, defaultPortStr, err := net.SplitHostPort(nc.LocalAddr())
	if err != nil {
		t.Fatalf("split default local addr: %v", err)
	}
	defaultPort, _ := strconv.Atoi(defaultPortStr)

	tenantIP, tenantPortStr, err := net.SplitHostPort(tenantNC.LocalAddr())
	if err != nil {
		t.Fatalf("split tenant local addr: %v", err)
	}
	tenantPort, _ := strconv.Atoi(tenantPortStr)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(connzResponse{Connections: []connzConnection{
			{CID: 1, Type: "nats", Name: "shipping-service", IP: defaultIP, Port: defaultPort, Account: "AAADEFAULT"},
			{CID: 2, Type: "nats", Name: "shipping-service", IP: tenantIP, Port: tenantPort, Account: "AAAACME"},
			{CID: 3, Type: "nats", Name: "refdata-service", IP: "10.0.0.9", Port: 9999, Account: "AAADEFAULT"},
			{CID: 4, Type: "nats", Name: "accounts-service", IP: "10.0.0.8", Port: 8888, Account: "AAASYS"},
		}})
	}))
	defer mock.Close()

	h := NewHandlers(Deps{
		Log:             discardLogger(),
		NatsMonitorURL:  mock.URL,
		NC:              nc,
		TenantResources: map[string]*tenantResources{"acme": {nc: tenantNC}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/connections", nil)
	w := httptest.NewRecorder()

	h.listNatsConnections(w, req)

	var body natsConnectionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byCID := map[uint64]natsConnection{}
	for _, c := range body.Connections {
		byCID[c.CID] = c
	}
	if got := byCID[1].TenantLabel; got != "DEFAULT" {
		t.Fatalf("expected cid 1 (deps.NC) labeled DEFAULT, got %q", got)
	}
	if got := byCID[2].TenantLabel; got != "acme" {
		t.Fatalf("expected cid 2 (tenant connection) labeled acme, got %q", got)
	}
	if got := byCID[3].TenantLabel; got != "DEFAULT" {
		t.Fatalf("expected cid 3 (refdata-service, sharing the DEFAULT account but not our connection) labeled DEFAULT via account fan-out, got %q", got)
	}
	if got := byCID[4].TenantLabel; got != "" {
		t.Fatalf("expected cid 4 (accounts-service, on the unrelated SYS account) to have no label, got %q", got)
	}
}

func TestTenantLabelsByAccountSkipsNilTenantEntriesAndUnownedAccounts(t *testing.T) {
	if got := tenantLabelsByAccount(Deps{}, nil); len(got) != 0 {
		t.Fatalf("expected nil map for empty deps, got %+v", got)
	}
	deps := Deps{TenantResources: map[string]*tenantResources{"acme": nil}}
	if got := tenantLabelsByAccount(deps, nil); len(got) != 0 {
		t.Fatalf("expected a nil tenantResources entry to be skipped, got %+v", got)
	}
}

func TestListNatsConnectionsReturns502WhenMonitoringEndpointUnreachable(t *testing.T) {
	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: "http://127.0.0.1:1"}) // port 1: connection refused
	req := httptest.NewRequest(http.MethodGet, "/api/nats/connections", nil)
	w := httptest.NewRecorder()

	h.listNatsConnections(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListNatsConnectionsReturns502OnMalformedBody(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/connections", nil)
	w := httptest.NewRecorder()

	h.listNatsConnections(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── Services ─────────────────────────────────────────────────────────────

func TestListNatsServicesReportsInstanceAndEndpointStats(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	svc, err := micro.AddService(nc, micro.Config{Name: "widget-service", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("micro.AddService: %v", err)
	}
	defer svc.Stop() //nolint:errcheck

	requests, errored := 0, 0
	if err := svc.AddEndpoint("get", micro.HandlerFunc(func(req micro.Request) {
		requests++
		if requests == 2 {
			errored++
			_ = req.Error("500", "boom", nil)
			return
		}
		_ = req.Respond([]byte(`{"ok":true}`))
	}), micro.WithEndpointSubject("widget.get")); err != nil {
		t.Fatalf("AddEndpoint: %v", err)
	}

	// Two successes, one error — exercises both NumRequests and NumErrors.
	for i := 0; i < 3; i++ {
		if _, err := nc.Request("widget.get", nil, time.Second); err != nil && i != 1 {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	time.Sleep(50 * time.Millisecond) // let the endpoint's stats counters settle

	h := NewHandlers(Deps{Log: discardLogger(), NC: nc})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/services", nil)
	w := httptest.NewRecorder()

	h.listNatsServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body natsServicesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Services) != 1 {
		t.Fatalf("expected 1 service, got %d: %+v", len(body.Services), body.Services)
	}
	widget := body.Services[0]
	if widget.Name != "widget-service" || widget.Version != "1.0.0" {
		t.Fatalf("unexpected service identity: %+v", widget)
	}
	if len(widget.Instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(widget.Instances))
	}
	inst := widget.Instances[0]
	if inst.ID == "" || inst.Started.IsZero() {
		t.Fatalf("expected instance ID + started time, got %+v", inst)
	}
	if len(inst.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(inst.Endpoints))
	}
	ep := inst.Endpoints[0]
	if ep.Name != "get" || ep.Subject != "widget.get" {
		t.Fatalf("unexpected endpoint identity: %+v", ep)
	}
	if ep.NumRequests != 3 || ep.NumErrors != 1 {
		t.Fatalf("expected 3 requests / 1 error, got %+v", ep)
	}
}

func TestListNatsServicesDedupsSameInstanceSeenOnBothConnections(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	svc, err := micro.AddService(nc, micro.Config{Name: "solo-service", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("micro.AddService: %v", err)
	}
	defer svc.Stop() //nolint:errcheck

	// A second connection to the very same embedded server/account. In
	// production NC (DEFAULT) and TenantNC (a tenant account) are different
	// accounts, so a real service is only ever discoverable on one of them —
	// but this embedded test server has no account separation, so both
	// connections legitimately see the same registered instance. The
	// handler must still report it once, not twice.
	nc2, err := nats.Connect(nc.ConnectedUrl(), nats.Name("rest-handlers-test-2"))
	if err != nil {
		t.Fatalf("second connection: %v", err)
	}
	defer nc2.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NC: nc, TenantNC: nc2})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/services", nil)
	w := httptest.NewRecorder()

	// collectStats always blocks for the full srvDiscoveryWindow (no signal
	// exists for "no more replies coming" in a broadcast/fan-in protocol).
	// Querying NC and TenantNC sequentially would cost ~2*srvDiscoveryWindow
	// (visibly slow in the Admin UI — this is the root cause a "why is the
	// Services panel slow" report traced to); querying them concurrently
	// should cost about one window regardless of connection count. Assert
	// comfortably under 2*srvDiscoveryWindow so a regression back to
	// sequential querying fails this test, with headroom above one window
	// for scheduler jitter in CI.
	start := time.Now()
	h.listNatsServices(w, req)
	elapsed := time.Since(start)
	if elapsed >= 2*srvDiscoveryWindow {
		t.Fatalf("expected querying 2 connections concurrently to take about one %s window, took %s (looks sequential)", srvDiscoveryWindow, elapsed)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body natsServicesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Services) != 1 {
		t.Fatalf("expected 1 service, got %d: %+v", len(body.Services), body.Services)
	}
	if len(body.Services[0].Instances) != 1 {
		t.Fatalf("expected the same instance to be deduped to 1, got %d", len(body.Services[0].Instances))
	}
}

// TestListNatsServicesPassesThroughInstanceMetadata covers the tenant-tag
// addition (Phase 17c follow-up): browserrpc.Adapter tags its micro
// registration with Metadata: {"tenant": <name>} so the Services panel can
// tell two same-named shipping-service instances (one per tenant) apart —
// $SRV.STATS already carries Metadata on its ServiceIdentity, so this is
// pure pass-through, not a new query.
func TestListNatsServicesPassesThroughInstanceMetadata(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	svc, err := micro.AddService(nc, micro.Config{
		Name:     "tagged-service",
		Version:  "1.0.0",
		Metadata: map[string]string{"tenant": "acme"},
	})
	if err != nil {
		t.Fatalf("micro.AddService: %v", err)
	}
	defer svc.Stop() //nolint:errcheck

	h := NewHandlers(Deps{Log: discardLogger(), NC: nc})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/services", nil)
	w := httptest.NewRecorder()

	h.listNatsServices(w, req)

	var body natsServicesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Services) != 1 || len(body.Services[0].Instances) != 1 {
		t.Fatalf("expected 1 service with 1 instance, got %+v", body.Services)
	}
	if got := body.Services[0].Instances[0].Metadata["tenant"]; got != "acme" {
		t.Fatalf("expected instance metadata tenant=acme, got %+v", body.Services[0].Instances[0].Metadata)
	}
}

func TestListNatsServicesReturnsEmptyWhenNoServicesRegistered(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := NewHandlers(Deps{Log: discardLogger(), NC: nc})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/services", nil)
	w := httptest.NewRecorder()

	h.listNatsServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body natsServicesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Services == nil || len(body.Services) != 0 {
		t.Fatalf("expected an empty (non-nil) services list, got %+v", body.Services)
	}
}

// collectStats itself (not just the handler) returns nil rather than
// panicking or blocking forever when handed a nil connection — the handler
// relies on this to skip a not-yet-established TenantNC.
func TestCollectStatsHandlesNilConnection(t *testing.T) {
	if got := collectStats(context.Background(), nil); got != nil {
		t.Fatalf("expected nil for a nil connection, got %+v", got)
	}
}
