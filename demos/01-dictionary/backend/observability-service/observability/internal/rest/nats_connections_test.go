package rest

// Connections + Account Activity panels — adapted from shipping-service's
// nats_ops_test.go (Phase 17c/27). The reshape/sort/secondary-read specs are
// lifted verbatim; the tenant-label specs are rewritten against
// AccountsClient's mocked HTTP call instead of the old LocalAddr-matching
// trick, since that mechanism no longer exists in this service (Phase 30d's
// design note in accounts_client.go).

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// newAccountsMock returns an AccountsClient pointed at a mock server
// serving accs at GET /api/accounts, verifying Basic Auth is presented with
// the expected secret.
func newAccountsMock(t *testing.T, accs []AccountsClientAccount) *AccountsClient {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != accountsBasicAuthUser || pass != "test-secret" {
			t.Errorf("expected basic auth %s/test-secret, got %q/%q (ok=%v)", accountsBasicAuthUser, user, pass, ok)
		}
		_ = json.NewEncoder(w).Encode(accs)
	}))
	t.Cleanup(mock.Close)
	return &AccountsClient{BaseURL: mock.URL, AuthSecret: "test-secret", Log: discardLogger()}
}

// TestTenantNamesExcludesPlatformAndSysCaseInsensitively pins the shape
// GET /api/accounts actually returns, caught by Phase 30i's live
// verification against a real accounts-service: it stores and returns
// PLATFORM/SYS with lowercase names ("platform", "sys" —
// accounts/handler.go's h.Store.Get(ctx, "platform")), not the uppercase
// "PLATFORM" this file originally compared against, and SYS gets a
// Postgres row like any other account rather than being naturally absent.
// Before this fix, introspectableAccounts (kv.go) built a bogus
// monitor.platform.js/monitor.sys.js-prefixed JetStream context for each —
// neither has a matching BR-AC32 cross-account import, so the very next
// $JS.API call failed closed with "no responders", aborting
// listKVBuckets/listStreams' entire response (including every real
// tenant's already-successful results) the moment either was reached.
func TestTenantNamesExcludesPlatformAndSysCaseInsensitively(t *testing.T) {
	accounts := newAccountsMock(t, []AccountsClientAccount{
		{Name: "acme", PublicKey: "AAAACME"},
		{Name: "globex", PublicKey: "AAAGLOBEX"},
		{Name: "platform", PublicKey: "AAAPLATFORM"},
		{Name: "sys", PublicKey: "AAASYS"},
	})

	names := accounts.TenantNames(t.Context())

	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if !got["acme"] || !got["globex"] {
		t.Fatalf("expected acme and globex in TenantNames, got %v", names)
	}
	if got["platform"] || got["sys"] {
		t.Fatalf("expected platform and sys excluded from TenantNames, got %v", names)
	}
	if len(names) != 2 {
		t.Fatalf("expected exactly 2 tenant names, got %d: %v", len(names), names)
	}
}

// TestTenantStatusesReturnsStatusPerTenantExcludingPlatformAndSys mirrors
// TestTenantNamesExcludesPlatformAndSysCaseInsensitively for the status map
// introspectableAccounts (kv.go) uses to tag the Streams/KV Buckets panels'
// account groups with their real accounts-service lifecycle state.
func TestTenantStatusesReturnsStatusPerTenantExcludingPlatformAndSys(t *testing.T) {
	accounts := newAccountsMock(t, []AccountsClientAccount{
		{Name: "acme", PublicKey: "AAAACME", Status: "active"},
		{Name: "globex", PublicKey: "AAAGLOBEX", Status: "suspended"},
		{Name: "platform", PublicKey: "AAAPLATFORM", Status: "active"},
		{Name: "sys", PublicKey: "AAASYS", Status: "active"},
	})

	statuses := accounts.TenantStatuses(t.Context())

	if statuses["acme"] != "active" {
		t.Fatalf("expected acme active, got %v", statuses)
	}
	if statuses["globex"] != "suspended" {
		t.Fatalf("expected globex suspended, got %v", statuses)
	}
	if _, ok := statuses["platform"]; ok {
		t.Fatalf("expected platform excluded from TenantStatuses, got %v", statuses)
	}
	if _, ok := statuses["sys"]; ok {
		t.Fatalf("expected sys excluded from TenantStatuses, got %v", statuses)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected exactly 2 tenant statuses, got %d: %v", len(statuses), statuses)
	}
}

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

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
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
	if body.Page.Limit != 1024 || body.Page.Total != 2 || body.Page.NumConnections != 2 || body.Page.Offset != 0 {
		t.Fatalf("expected connz paging envelope passed through (total 2, offset 0, limit 1024), got %+v", body.Page)
	}
	if body.Server.MaxConnections != 65536 {
		t.Fatalf("expected maxConnections 65536 read from /varz, got %d", body.Server.MaxConnections)
	}
}

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

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
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

func TestListNatsConnectionsPassesThroughTruncatedConnzPage(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(connzResponse{NumConnections: 1, Total: 2100, Offset: 0, Limit: 1, Connections: []connzConnection{
			{CID: 1, Type: "nats", Name: "shipping-service", Account: "PLATFORM"},
		}})
	}))
	defer mock.Close()

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
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

// TestListNatsConnectionsResolvesTenantLabelFromAccountsService replaces
// shipping-service's LocalAddr-matching spec: labels now come from a plain
// account-pubkey lookup against accounts-service's GET /api/accounts, so
// every /connz row with a recognized account is labeled directly — no
// "which connections do we own" fan-out needed, and no separate case for a
// connection sharing an account with one we hold (PLATFORM/refdata-service
// both resolve the same simple way now). An unrecognized account (SYS) is
// still left unlabeled.
func TestListNatsConnectionsResolvesTenantLabelFromAccountsService(t *testing.T) {
	accounts := newAccountsMock(t, []AccountsClientAccount{
		{Name: "PLATFORM", PublicKey: "AAAPLATFORM"},
		{Name: "acme", PublicKey: "AAAACME"},
	})

	monitor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(connzResponse{Connections: []connzConnection{
			{CID: 1, Type: "nats", Name: "shipping-service", Account: "AAAPLATFORM"},
			{CID: 2, Type: "nats", Name: "shipping-service", Account: "AAAACME"},
			{CID: 3, Type: "nats", Name: "refdata-service", Account: "AAAPLATFORM"},
			{CID: 4, Type: "nats", Name: "accounts-service", Account: "AAASYS"},
		}})
	}))
	defer monitor.Close()

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: monitor.URL, Accounts: accounts})
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
		t.Fatalf("expected cid 1 labeled PLATFORM, got %q", got)
	}
	if got := byCID[2].TenantLabel; got != "acme" {
		t.Fatalf("expected cid 2 labeled acme, got %q", got)
	}
	if got := byCID[3].TenantLabel; got != "PLATFORM" {
		t.Fatalf("expected cid 3 (refdata-service, same PLATFORM account) labeled PLATFORM, got %q", got)
	}
	if got := byCID[4].TenantLabel; got != "" {
		t.Fatalf("expected cid 4 (unrelated SYS account) to have no label, got %q", got)
	}
}

// TestAccountsClientLabelsIsNilSafeAndDegradesOnFailure covers the
// nil-receiver and unreachable/erroring cases directly — Connections and
// AccountActivity must never fail because labeling failed.
func TestAccountsClientLabelsIsNilSafeAndDegradesOnFailure(t *testing.T) {
	var nilClient *AccountsClient
	if got := nilClient.Labels(t.Context()); got != nil {
		t.Fatalf("expected nil labels from a nil AccountsClient, got %+v", got)
	}

	unconfigured := &AccountsClient{}
	if got := unconfigured.Labels(t.Context()); got != nil {
		t.Fatalf("expected nil labels from an unconfigured (empty BaseURL) client, got %+v", got)
	}

	unreachable := &AccountsClient{BaseURL: "http://127.0.0.1:1", Log: discardLogger()}
	if got := unreachable.Labels(t.Context()); got != nil {
		t.Fatalf("expected nil labels when accounts-service is unreachable, got %+v", got)
	}
}

func TestListNatsConnectionsReturns502WhenMonitoringEndpointUnreachable(t *testing.T) {
	h := New(Deps{Log: discardLogger(), NatsMonitorURL: "http://127.0.0.1:1"}) // port 1: connection refused
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

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
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
		_ = json.NewEncoder(w).Encode(accstatzResponse{AccountStatz: []accstatzAccount{
			{Account: "SYS", Conns: 1, LeafNodes: 0, TotalConns: 1, NumSubs: 42, Sent: accstatzDataStats{Msgs: 100, Bytes: 2000}, Received: accstatzDataStats{Msgs: 90, Bytes: 1800}, SlowConsumers: 0},
			{Account: "ACME", Conns: 3, LeafNodes: 1, TotalConns: 4, NumSubs: 12, Sent: accstatzDataStats{Msgs: 500, Bytes: 9000}, Received: accstatzDataStats{Msgs: 480, Bytes: 8800}, SlowConsumers: 2},
		}})
	}))
	defer mock.Close()

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
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
	if body.Accounts[0].Account != "ACME" || body.Accounts[1].Account != "SYS" {
		t.Fatalf("expected accounts sorted [ACME, SYS], got [%s, %s]", body.Accounts[0].Account, body.Accounts[1].Account)
	}
	acme := body.Accounts[0]
	if acme.Connections != 3 || acme.LeafNodes != 1 || acme.TotalConnections != 4 || acme.Subscriptions != 12 {
		t.Fatalf("unexpected reshaping of ACME's connection counters: %+v", acme)
	}
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

// TestListNatsAccountActivityResolvesTenantLabel proves accountsClient.Labels
// reaches accountActivity.TenantLabel, keyed off /accstatz's own "acc" field.
func TestListNatsAccountActivityResolvesTenantLabel(t *testing.T) {
	accounts := newAccountsMock(t, []AccountsClientAccount{{Name: "PLATFORM", PublicKey: "AAAPLATFORM"}})

	monitor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(accstatzResponse{AccountStatz: []accstatzAccount{
			{Account: "AAAPLATFORM", Conns: 1},
			{Account: "AAASYS", Conns: 1},
		}})
	}))
	defer monitor.Close()

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: monitor.URL, Accounts: accounts})
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

// Account labeling is a secondary read here too: an unreachable
// accounts-service must not cost the caller the activity rollup itself.
func TestListNatsAccountActivitySurvivesAccountsServiceFailure(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(accstatzResponse{AccountStatz: []accstatzAccount{{Account: "ACME", Conns: 1}}})
	}))
	defer mock.Close()

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL, Accounts: &AccountsClient{BaseURL: "http://127.0.0.1:1", Log: discardLogger()}})
	w := httptest.NewRecorder()
	h.listNatsAccountActivity(w, httptest.NewRequest(http.MethodGet, "/api/nats/account-activity", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 despite accounts-service being unreachable, got %d: %s", w.Code, w.Body.String())
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
	h := New(Deps{Log: discardLogger(), NatsMonitorURL: "http://127.0.0.1:1"}) // port 1: connection refused
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

	h := New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL})
	w := httptest.NewRecorder()
	h.listNatsAccountActivity(w, httptest.NewRequest(http.MethodGet, "/api/nats/account-activity", nil))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}
