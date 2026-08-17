package rest

// Services panel — adapted from shipping-service's nats_ops_test.go Services
// section (Phase 17c). The real-registration specs (stats/endpoints,
// metadata pass-through, empty-when-nothing) are lifted verbatim against
// the "platform" bare $SRV.STATS path — identical mechanism in test and
// production. The multi-subject fan-out/dedup specs are rewritten: the
// original tested two live connections sharing one account (an embedded-
// test-server artifact); this rewrite proves the same fan-out/dedup logic
// across two SUBJECTS on one connection instead, using a hand-rolled
// responder on the second (monitor.faux.srv.STATS) to simulate what a real
// tenant's remapped reply would look like — it does not prove the
// BR-AC31 cross-account remap itself resolves on the wire (see
// nats_services.go's package doc comment for why that's deferred to 30i).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

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

	for i := 0; i < 3; i++ {
		if _, err := nc.Request("widget.get", nil, time.Second); err != nil && i != 1 {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	time.Sleep(50 * time.Millisecond) // let the endpoint's stats counters settle

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
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

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
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

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
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

// TestListNatsServicesDedupsSameInstanceSeenOnMultipleSubjects proves the
// dedup-by-(name,id) logic that used to guard against the same instance
// answering both deps.NC and deps.TenantNC now guards against it answering
// both the platform subject and a (simulated) tenant remap subject.
func TestListNatsServicesDedupsSameInstanceSeenOnMultipleSubjects(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	svc, err := micro.AddService(nc, micro.Config{Name: "solo-service", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("micro.AddService: %v", err)
	}
	defer svc.Stop() //nolint:errcheck

	stats := svc.Stats()
	replyPayload, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal stats: %v", err)
	}
	// Simulates what BR-AC31's monitor.faux.srv.STATS remap would deliver:
	// the very same instance replying on the tenant-remapped subject too.
	fauxSub, err := nc.Subscribe("monitor.faux.srv.STATS", func(msg *nats.Msg) {
		_ = msg.Respond(replyPayload)
	})
	if err != nil {
		t.Fatalf("subscribe fake tenant responder: %v", err)
	}
	defer fauxSub.Unsubscribe() //nolint:errcheck

	accounts := newAccountsMock(t, []AccountsClientAccount{{Name: "faux", PublicKey: "AAAFAUX"}})
	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: accounts})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/services", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	h.listNatsServices(w, req)
	elapsed := time.Since(start)
	// Two subjects queried concurrently should cost about one
	// srvDiscoveryWindow, not two — same regression this guarded against
	// pre-lift, now on subject count instead of connection count.
	if elapsed >= 2*srvDiscoveryWindow {
		t.Fatalf("expected querying 2 subjects concurrently to take about one %s window, took %s (looks sequential)", srvDiscoveryWindow, elapsed)
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

func TestDiscoverySubjectsBuildsPlatformPlusPerTenantRemap(t *testing.T) {
	got := discoverySubjects([]string{"acme", "globex"})
	want := []string{"$SRV.STATS", "monitor.acme.srv.STATS", "monitor.globex.srv.STATS"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestDiscoverySubjectsWithNoTenantsIsPlatformOnly(t *testing.T) {
	got := discoverySubjects(nil)
	if len(got) != 1 || got[0] != "$SRV.STATS" {
		t.Fatalf("expected just [\"$SRV.STATS\"], got %v", got)
	}
}

func TestCollectStatsHandlesNilConnection(t *testing.T) {
	if got := collectStats(context.Background(), nil, "$SRV.STATS"); got != nil {
		t.Fatalf("expected nil for a nil connection, got %+v", got)
	}
}
