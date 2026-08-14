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
		if r.URL.Path == "/varz" {
			_ = json.NewEncoder(w).Encode(varzResponse{MaxConnections: 65536})
			return
		}
		if got := r.URL.Query().Get("subs"); got != "true" {
			t.Errorf("expected subs=true query param, got %q", got)
		}
		if got := r.URL.Query().Get("auth"); got != "true" {
			t.Errorf("expected auth=true query param, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(connzResponse{NumConnections: 2, Total: 2, Offset: 0, Limit: 1024, Connections: []connzConnection{
			{CID: 2, Type: "websocket", Name: "", Lang: "nats.ws", Version: "3.4.0", Account: "ACME", RTT: "1ms", Uptime: "1m", Idle: "1m"},
			{CID: 1, Type: "nats", Name: "refdata-service", Lang: "go", Version: "1.52.0", Account: "PLATFORM", RTT: "300µs", Uptime: "56m", Idle: "10s", InMsgs: 208, OutMsgs: 560, Subscriptions: 16, SubscriptionsList: []string{"rpc.*.refdata.type.list.v1"}},
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
	if refdata.Name != "refdata-service" || refdata.Type != "nats" || refdata.Account != "PLATFORM" {
		t.Fatalf("unexpected reshaping of refdata-service connection: %+v", refdata)
	}
	if refdata.InMsgs != 208 || refdata.Subscriptions != 16 || len(refdata.SubscriptionsList) != 1 {
		t.Fatalf("unexpected counters/subs on refdata-service connection: %+v", refdata)
	}
	ws := body.Connections[1]
	if ws.Type != "websocket" || ws.Lang != "nats.ws" {
		t.Fatalf("unexpected reshaping of websocket connection: %+v", ws)
	}
	// /connz's paging envelope is passed through, not recomputed from the row
	// count — limit especially, which the panel shows and no amount of
	// counting rows could reveal.
	if body.Page.Limit != 1024 || body.Page.Total != 2 || body.Page.NumConnections != 2 || body.Page.Offset != 0 {
		t.Fatalf("expected connz paging envelope passed through (total 2, offset 0, limit 1024), got %+v", body.Page)
	}
	// The server's real ceiling comes from a second endpoint (/varz), not from
	// /connz — the panel draws a capacity rail from it.
	if body.Server.MaxConnections != 65536 {
		t.Fatalf("expected maxConnections 65536 read from /varz, got %d", body.Server.MaxConnections)
	}
}

// /varz is a secondary read: an unreachable or erroring /varz costs the caller
// the capacity ceiling, not the connection list. A zero ceiling is the signal
// the panel uses to draw no capacity rail.
func TestListNatsConnectionsSurvivesVarzFailure(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/varz" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(connzResponse{NumConnections: 1, Total: 1, Limit: 1024, Connections: []connzConnection{
			{CID: 1, Type: "nats", Name: "shipping-service", Account: "PLATFORM"},
		}})
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	w := httptest.NewRecorder()
	h.listNatsConnections(w, httptest.NewRequest(http.MethodGet, "/api/nats/connections", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 despite /varz failing, got %d: %s", w.Code, w.Body.String())
	}
	var body natsConnectionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Connections) != 1 || body.Page.Limit != 1024 {
		t.Fatalf("expected the connection list and paging envelope intact, got %d rows / page %+v", len(body.Connections), body.Page)
	}
	if body.Server.MaxConnections != 0 {
		t.Fatalf("expected maxConnections 0 when /varz fails, got %d", body.Server.MaxConnections)
	}
}

// A /connz response that paged (total > offset+num_connections) must report
// the server's own numbers untouched, so the panel can say "one page of
// several" instead of implying the list it drew is every connection.
func TestListNatsConnectionsPassesThroughTruncatedConnzPage(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(connzResponse{NumConnections: 1, Total: 2100, Offset: 0, Limit: 1, Connections: []connzConnection{
			{CID: 1, Type: "nats", Name: "shipping-service", Account: "PLATFORM"},
		}})
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	w := httptest.NewRecorder()
	h.listNatsConnections(w, httptest.NewRequest(http.MethodGet, "/api/nats/connections", nil))

	var body natsConnectionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Connections) != 1 {
		t.Fatalf("expected the single paged row, got %d", len(body.Connections))
	}
	if body.Page.Total != 2100 || body.Page.NumConnections != 1 || body.Page.Limit != 1 {
		t.Fatalf("expected total 2100 / num 1 / limit 1 preserved, got %+v", body.Page)
	}
}

// TestListNatsConnectionsLabelsAnyConnectionSharingAKnownAccount is why
// tenantLabelsByAccount is two stages, not one: it opens two REAL
// connections (so LocalAddr() is a real ephemeral port, not a fabricated
// value) and has the mocked /connz report those exact addresses back to
// establish "this account NKey means PLATFORM / means acme" — then proves
// that mapping is applied by ACCOUNT to a fourth connz row (simulating
// refdata-service: same PLATFORM account, but an address this process never
// held, i.e. a connection it doesn't own) — the whole point being that
// refdata-service and the nats CLI, sharing PLATFORM with shipping-service's
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
			{CID: 1, Type: "nats", Name: "shipping-service", IP: defaultIP, Port: defaultPort, Account: "AAAPLATFORM"},
			{CID: 2, Type: "nats", Name: "shipping-service", IP: tenantIP, Port: tenantPort, Account: "AAAACME"},
			{CID: 3, Type: "nats", Name: "refdata-service", IP: "10.0.0.9", Port: 9999, Account: "AAAPLATFORM"},
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
	if got := byCID[1].TenantLabel; got != "PLATFORM" {
		t.Fatalf("expected cid 1 (deps.NC) labeled PLATFORM, got %q", got)
	}
	if got := byCID[2].TenantLabel; got != "acme" {
		t.Fatalf("expected cid 2 (tenant connection) labeled acme, got %q", got)
	}
	if got := byCID[3].TenantLabel; got != "PLATFORM" {
		t.Fatalf("expected cid 3 (refdata-service, sharing the PLATFORM account but not our connection) labeled PLATFORM via account fan-out, got %q", got)
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

// ─── Account Activity ───────────────────────────────────────────────────────

func TestListNatsAccountActivityReshapesAndSortsAccstatz(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connz" {
			_ = json.NewEncoder(w).Encode(connzResponse{}) // no accounts resolvable; not under test here
			return
		}
		_ = json.NewEncoder(w).Encode(accstatzResponse{AccountStatz: []accstatzAccount{
			{Account: "SYS", Conns: 1, LeafNodes: 0, TotalConns: 1, NumSubs: 42, Sent: accstatzDataStats{Msgs: 100, Bytes: 2000}, Received: accstatzDataStats{Msgs: 90, Bytes: 1800}, SlowConsumers: 0},
			{Account: "ACME", Conns: 3, LeafNodes: 1, TotalConns: 4, NumSubs: 12, Sent: accstatzDataStats{Msgs: 500, Bytes: 9000}, Received: accstatzDataStats{Msgs: 480, Bytes: 8800}, SlowConsumers: 2},
		}})
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	w := httptest.NewRecorder()
	h.listNatsAccountActivity(w, httptest.NewRequest(http.MethodGet, "/api/nats/account-activity", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body natsAccountActivityResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(body.Accounts))
	}
	// Sorted by account name ascending ("ACME" < "SYS"), not declaration order.
	if body.Accounts[0].Account != "ACME" || body.Accounts[1].Account != "SYS" {
		t.Fatalf("expected accounts sorted [ACME, SYS], got [%s, %s]", body.Accounts[0].Account, body.Accounts[1].Account)
	}
	acme := body.Accounts[0]
	if acme.Connections != 3 || acme.LeafNodes != 1 || acme.TotalConnections != 4 || acme.Subscriptions != 12 {
		t.Fatalf("unexpected reshaping of ACME's connection counters: %+v", acme)
	}
	// received -> In, sent -> Out, matching natsConnection's naming.
	if acme.InMsgs != 480 || acme.InBytes != 8800 || acme.OutMsgs != 500 || acme.OutBytes != 9000 {
		t.Fatalf("expected received->In / sent->Out reshaping, got %+v", acme)
	}
	if acme.SlowConsumers != 2 {
		t.Fatalf("expected slowConsumers 2 passed through, got %d", acme.SlowConsumers)
	}
	if body.Accounts[1].SlowConsumers != 0 {
		t.Fatalf("expected SYS's slowConsumers to stay 0, got %d", body.Accounts[1].SlowConsumers)
	}
}

// TestListNatsAccountActivityResolvesTenantLabel proves the secondary /connz
// read actually reaches accountActivity.TenantLabel — the same
// tenantLabelsByAccount fan-out Connections/Services use (BR-028), keyed off
// /accstatz's own "acc" identifier this time instead of a connz row's account
// field.
func TestListNatsAccountActivityResolvesTenantLabel(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connz" {
			ip, portStr, err := net.SplitHostPort(nc.LocalAddr())
			if err != nil {
				t.Fatalf("split local addr: %v", err)
			}
			port, _ := strconv.Atoi(portStr)
			_ = json.NewEncoder(w).Encode(connzResponse{Connections: []connzConnection{
				{CID: 1, IP: ip, Port: port, Account: "AAAPLATFORM"},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(accstatzResponse{AccountStatz: []accstatzAccount{
			{Account: "AAAPLATFORM", Conns: 1},
			{Account: "AAASYS", Conns: 1},
		}})
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL, NC: nc})
	w := httptest.NewRecorder()
	h.listNatsAccountActivity(w, httptest.NewRequest(http.MethodGet, "/api/nats/account-activity", nil))

	var body natsAccountActivityResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byAccount := map[string]accountActivity{}
	for _, a := range body.Accounts {
		byAccount[a.Account] = a
	}
	if got := byAccount["AAAPLATFORM"].TenantLabel; got != "PLATFORM" {
		t.Fatalf("expected AAAPLATFORM labeled PLATFORM, got %q", got)
	}
	if got := byAccount["AAASYS"].TenantLabel; got != "" {
		t.Fatalf("expected the unrelated SYS account to have no label, got %q", got)
	}
}

// Account labeling is a secondary read here too: a failed /connz probe must
// not cost the caller the activity rollup itself, matching /varz's role for
// Connections.
func TestListNatsAccountActivitySurvivesConnzProbeFailure(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connz" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(accstatzResponse{AccountStatz: []accstatzAccount{{Account: "ACME", Conns: 1}}})
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	w := httptest.NewRecorder()
	h.listNatsAccountActivity(w, httptest.NewRequest(http.MethodGet, "/api/nats/account-activity", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 despite /connz probe failing, got %d: %s", w.Code, w.Body.String())
	}
	var body natsAccountActivityResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Accounts) != 1 || body.Accounts[0].TenantLabel != "" {
		t.Fatalf("expected the activity rollup intact with no label, got %+v", body.Accounts)
	}
}

func TestListNatsAccountActivityReturns502WhenMonitoringEndpointUnreachable(t *testing.T) {
	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: "http://127.0.0.1:1"}) // port 1: connection refused
	w := httptest.NewRecorder()

	h.listNatsAccountActivity(w, httptest.NewRequest(http.MethodGet, "/api/nats/account-activity", nil))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListNatsAccountActivityReturns502OnMalformedBody(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer mock.Close()

	h := NewHandlers(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	w := httptest.NewRecorder()
	h.listNatsAccountActivity(w, httptest.NewRequest(http.MethodGet, "/api/nats/account-activity", nil))

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
	// production NC (PLATFORM) and TenantNC (a tenant account) are different
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
