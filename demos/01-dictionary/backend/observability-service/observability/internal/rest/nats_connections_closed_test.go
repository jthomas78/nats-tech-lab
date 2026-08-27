package rest

// BR-062 (Phase 51a) — the closed-connection ring as connection OUTCOME.
//
// The specs below are derived from the rule, not from the handler: the
// server's own `reason` wording survives verbatim, the BR-058 join key is
// carried through, the raw JWT is dropped exactly as the live path drops it,
// the ordering is the one a caller joining onto a roster row needs, and the
// paging envelope is passed through so an absent entry can be presented as
// "outside the retained window" rather than as "never connected".

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// closedMock serves a canned /connz?state=closed body and records the query
// it was asked for, since asking for the wrong ring would silently return
// the live list — every assertion below would still pass against it.
func closedMock(t *testing.T, body connzResponse) (*Handlers, *string) {
	t.Helper()
	var gotQuery string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(mock.Close)
	return New(Deps{Log: discardLogger(), NatsMonitorURL: mock.URL}), &gotQuery
}

func getClosed(t *testing.T, h *Handlers) (*httptest.ResponseRecorder, natsClosedConnectionsResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	h.listNatsClosedConnections(w, httptest.NewRequest(http.MethodGet, "/api/nats/connections/closed", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out natsClosedConnectionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return w, out
}

// The endpoint is worthless if it reads the live ring, and every other
// assertion in this file would pass just as happily against it.
func TestListNatsClosedConnectionsAsksForTheClosedRing(t *testing.T) {
	h, query := closedMock(t, connzResponse{})
	getClosed(t, h)

	if !strings.Contains(*query, "state=closed") {
		t.Errorf("expected the proxy to request state=closed, got query %q", *query)
	}
	if !strings.Contains(*query, "auth=true") {
		t.Errorf("expected auth=true (without it /connz omits jwt and authorized_user entirely), got query %q", *query)
	}
	// A closed connection's subscription list is history no caller reads,
	// and it is the largest field on a /connz row.
	if strings.Contains(*query, "subs=true") {
		t.Errorf("expected subs to be omitted from the closed read, got query %q", *query)
	}
}

// BR-062's core: why the connection ended, joined by the BR-058 key.
func TestListNatsClosedConnectionsCarriesReasonAndJoinKey(t *testing.T) {
	rawJWT := fakeUserJWT("admin-app")
	stop := time.Date(2026, 8, 27, 10, 30, 0, 0, time.UTC)
	h, _ := closedMock(t, connzResponse{NumConnections: 1, Total: 1, Connections: []connzConnection{
		{CID: 7, Name: "admin-ui", Account: "PLATFORM", JWT: rawJWT, AuthorizedUser: "UADMIN123",
			Reason: "Authentication Expired", Stop: stop},
	}})
	w, body := getClosed(t, h)

	if len(body.Connections) != 1 {
		t.Fatalf("expected 1 closed connection, got %d", len(body.Connections))
	}
	c := body.Connections[0]
	// Verbatim. A friendlier vocabulary of our own would swallow any reason
	// a future NATS release adds, and would not match the server log an
	// operator is correlating against.
	if c.Reason != "Authentication Expired" {
		t.Errorf("expected the server's own reason wording, got %q", c.Reason)
	}
	if !c.Stop.Equal(stop) {
		t.Errorf("expected stop %v, got %v", stop, c.Stop)
	}
	if c.UserKey != "UADMIN123" {
		t.Errorf("expected userKey from authorized_user (the BR-058 join key), got %q", c.UserKey)
	}
	if c.User != "admin-app" {
		t.Errorf("expected the credential name decoded from the jwt name claim, got %q", c.User)
	}
	// Same rule as the live path: the token is decoded server-side and
	// dropped. It is the credential's whole permission grid.
	if strings.Contains(w.Body.String(), rawJWT) {
		t.Error("expected the raw user JWT to be dropped from the response, but it was forwarded")
	}
}

// A refused connection is invisible in the Admin UI today, and is exactly
// what a revoked credential (51b) produces — so this is the case the
// endpoint exists for, not an incidental one.
func TestListNatsClosedConnectionsReportsAuthenticationFailure(t *testing.T) {
	h, _ := closedMock(t, connzResponse{NumConnections: 1, Total: 1, Connections: []connzConnection{
		{CID: 9, AuthorizedUser: "UREVOKED", Reason: "Authentication Failure"},
	}})
	_, body := getClosed(t, h)

	if len(body.Connections) != 1 || body.Connections[0].Reason != "Authentication Failure" {
		t.Fatalf("expected a refused connection to be reported verbatim, got %+v", body.Connections)
	}
}

// Most-recent-first, so a caller joining onto a roster row takes the first
// match rather than scanning the whole ring for the latest.
func TestListNatsClosedConnectionsSortsMostRecentlyStoppedFirst(t *testing.T) {
	base := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	h, _ := closedMock(t, connzResponse{NumConnections: 3, Total: 3, Connections: []connzConnection{
		{CID: 1, AuthorizedUser: "UONE", Reason: "Client Closed", Stop: base},
		{CID: 2, AuthorizedUser: "UONE", Reason: "Authentication Expired", Stop: base.Add(2 * time.Minute)},
		{CID: 3, AuthorizedUser: "UONE", Reason: "Client Closed", Stop: base.Add(time.Minute)},
	}})
	_, body := getClosed(t, h)

	var order []uint64
	for _, c := range body.Connections {
		order = append(order, c.CID)
	}
	if len(order) != 3 || order[0] != 2 || order[1] != 3 || order[2] != 1 {
		t.Errorf("expected cids ordered by stop descending [2 3 1], got %v", order)
	}
	// Three closed connections for ONE credential is the ordinary case for a
	// session NKey; the first entry is the one BR-062 means by "its most
	// recent connection".
	if body.Connections[0].Reason != "Authentication Expired" {
		t.Errorf("expected the latest outcome first, got %q", body.Connections[0].Reason)
	}
}

// The ring is bounded, so a caller has to be able to tell a complete answer
// from a partial one — BR-062's "outside the retained window" clause has no
// honest presentation without this.
func TestListNatsClosedConnectionsPassesThroughThePagingEnvelope(t *testing.T) {
	h, _ := closedMock(t, connzResponse{NumConnections: 2, Total: 59, Offset: 0, Limit: 2,
		Connections: []connzConnection{{CID: 1}, {CID: 2}}})
	_, body := getClosed(t, h)

	if body.Page.Total != 59 || body.Page.NumConnections != 2 || body.Page.Limit != 2 {
		t.Errorf("expected connz's own paging envelope passed through, got %+v", body.Page)
	}
}

// An empty ring is an empty ARRAY, not null: a client that has to
// special-case null before it can say "outside the retained window" will
// eventually forget to.
func TestListNatsClosedConnectionsReturnsAnEmptyArrayNotNull(t *testing.T) {
	h, _ := closedMock(t, connzResponse{})
	w, _ := getClosed(t, h)

	if !strings.Contains(w.Body.String(), `"connections":[]`) {
		t.Errorf("expected an empty connections array, got %s", w.Body.String())
	}
}

func TestListNatsClosedConnectionsReports502WhenMonitorUnreachable(t *testing.T) {
	h := New(Deps{Log: discardLogger(), NatsMonitorURL: "http://127.0.0.1:1"})
	w := httptest.NewRecorder()
	h.listNatsClosedConnections(w, httptest.NewRequest(http.MethodGet, "/api/nats/connections/closed", nil))

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 when the monitoring endpoint is unreachable, got %d", w.Code)
	}
}
