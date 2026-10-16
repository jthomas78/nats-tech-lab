// Command otel-stackcheck runs Phase 54a's stack checks against a cell
// started with the `otel` overlay (deploy/cell/compose.otel.yaml). It is the
// repeatable form of the checks recorded in Main-POC-Plan.md, 54a-6.
//
// It drives the real fleet-asset path — api.{ctx}.organizations.fleet-asset.
// add.v1 → organizations-service → rpc.*.refdata.item.get.v1 → refdata-service
// — with real browser tokens from accounts-service. Tokens stay in memory and
// are never printed.
//
// # How a trace is found again
//
// Every call carries a sent `traceparent`. BR-071 turns it into a span link,
// never a parent, so the check finds the trace in Tempo by
// `{ link:traceID = "…" }`. That search goes through Grafana's datasource
// proxy, because trace.search.v1 replies 501 until Phase 54c. Tempo matches a
// link ID only without its leading zeros, so the query strips them.
//
// # Subcommands
//
//	access               BR-067 checks 1 and 2 (operator reads every tenant;
//	                     tenant tokens and .creds get no trace data)
//	trust                BR-069 and BR-071 (spoofed tenant headers and body
//	                     change nothing; a sent traceparent is a link)
//	delay <n>            decision 8's visibility delay, n runs
//	send <n> <batchID>   n calls, all linked to batchID; prints latency
//	count <batchID>      traces in Tempo linked to batchID
//	waitcount <batchID> <want> <secs>
//	                     polls count until want or secs elapse
//	verify <batchID>     every trace linked to batchID has 3 spans
//	services             BR-072 S1-S3 (one complete list across accounts;
//	                     live endpoint statistics; tenants and raw $SRV
//	                     requests are refused)
//	snapshot             prints the service list, for the stop and pause
//	                     checks
//	watch <secs>         one connection, a snapshot every 2 s; start it
//	                     before a `docker pause`, which can stop a token
//	                     being minted
//
// BR-068 capacity (54b-7b). Run the services with OTEL_CAPTURE_BODIES=true:
//
//	seedships            the load dataset's 5 ports and 20 ships in acme
//	                     (the 114 vehicle types come from refdata-service's
//	                     cmd/seed-vehicle-types)
//	load <secs> <workers> <batchID>
//	                     the fixed workload (fleet-asset.add, type.list,
//	                     ship.list in turn); prints rate, latency, expected
//	                     spans and SUSTAINABLE PASS/FAIL
//	capture <batchID> <out>
//	                     one trace per root from a load run, as a template,
//	                     with reply bodies Tempo cut rebuilt to the size the
//	                     service exports
//	flood <templates> <spans> <workers> <linkID> <sampleOut>
//	                     sends the templates, with fresh IDs, straight to the
//	                     Collector (-collector); run it inside the cell network
//	floodverify <sampleFile>
//	                     every sampled flood trace is in Tempo, whole
//
// The outage and durability checks (BR-066, BR-068) are send/count/verify
// runs around `docker stop|pause|restart|kill` of the Collector and Tempo;
// the plan lists the sequence. access and trust exit 1 on any FAIL.
//
// Defaults are the za-1 cell's ports; pass the au-1 ports with flags.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	getSubject         = "api._platform.observability.trace.get.v1"
	serviceListSubject = "api._platform.observability.service.list.v1"
	// An organization seeded in acme. The vehicle type below is never in
	// refdata, so every call ends in ERROR — and still yields all 3 spans.
	// It was TAUTLINER until 54b-7b seeded the real vehicle types into acme.
	orgID       = "01M4ZQ0H2MF81CJX8VPWQR39T4"
	vehicleType = "STK-NEVER-SEEDED"
)

var traceSubjects = []string{
	"api._platform.observability.trace.search.v1",
	getSubject,
	"api._platform.observability.trace.services.v1",
}

var (
	accountsURL = flag.String("accounts", "http://127.0.0.1:7202", "accounts-service base URL (au-1: http://127.0.0.1:7252)")
	natsWS      = flag.String("nats-ws", "ws://127.0.0.1:9222", "NATS websocket URL for browser tokens (au-1: ws://127.0.0.1:9322)")
	natsTCP     = flag.String("nats", "nats://127.0.0.1:4222", "NATS URL for .creds (au-1: nats://127.0.0.1:4322)")
	grafanaURL  = flag.String("grafana", "http://127.0.0.1:3000", "Grafana base URL (au-1: http://127.0.0.1:3050)")
	credsDir    = flag.String("creds-dir", "demos/01-dictionary/nats/creds", "directory holding <tenant>.creds")
	collector   = flag.String("collector", "http://otel-collector:4318", "Collector OTLP/HTTP base URL for flood (reachable only on the backend network)")
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fail("usage: otel-stackcheck [flags] access|trust|delay|send|count|waitcount|verify|services|snapshot|watch …")
	}
	switch args[0] {
	case "access":
		exit(access())
	case "trust":
		exit(trust())
	case "delay":
		delay(atoi(arg(args, 1)))
	case "send":
		send(atoi(arg(args, 1)), arg(args, 2))
	case "count":
		fmt.Println(len(findByLink(arg(args, 1), 2000)))
	case "waitcount":
		waitCount(arg(args, 1), atoi(arg(args, 2)), atoi(arg(args, 3)))
	case "verify":
		verify(arg(args, 1))
	case "services":
		exit(services())
	case "snapshot":
		snapshotCmd()
	case "watch":
		watchCmd(atoi(arg(args, 1)))
	case "load":
		exit(load(atoi(arg(args, 1)), atoi(arg(args, 2)), arg(args, 3)))
	case "seedships":
		exit(seedShips())
	case "capture":
		capture(arg(args, 1), arg(args, 2))
	case "flood":
		flood(arg(args, 1), atoi(arg(args, 2)), atoi(arg(args, 3)), arg(args, 4), arg(args, 5))
	case "floodverify":
		exit(floodVerify(arg(args, 1)))
	default:
		fail("unknown subcommand " + args[0])
	}
}

// ---- credentials -------------------------------------------------------

type connectInfo struct {
	JWT      string `json:"jwt"`
	NKeySeed string `json:"nkeySeed"`
}

func browserConn(path, name string) *nats.Conn {
	resp, err := http.Get(*accountsURL + path)
	must(err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Sprintf("%s: status %d", path, resp.StatusCode))
	}
	var ci connectInfo
	must(json.NewDecoder(resp.Body).Decode(&ci))
	nc, err := nats.Connect(*natsWS, nats.UserJWTAndSeed(ci.JWT, ci.NKeySeed), nats.Name(name))
	must(err)
	return nc
}

func adminConn() *nats.Conn {
	return browserConn("/api/auth/adminConnectInfo", "otel-stackcheck-admin")
}

func tenantConn(tenant string) *nats.Conn {
	return browserConn("/api/auth/connectInfo?tenant="+url.QueryEscape(tenant), "otel-stackcheck-"+tenant)
}

func credsConn(tenant string) *nats.Conn {
	nc, err := nats.Connect(*natsTCP,
		nats.UserCredentials(filepath.Join(*credsDir, tenant+".creds")),
		nats.Name("otel-stackcheck-"+tenant+"-creds"))
	must(err)
	return nc
}

// ---- business call -----------------------------------------------------

// addFleet sends one fleet-asset add carrying a traceparent whose trace ID
// is link.
func addFleet(nc *nats.Conn, ctx, link string, sampled bool, extra nats.Header, body map[string]any) (time.Duration, error) {
	flags := "01"
	if !sampled {
		flags = "00"
	}
	msg := nats.NewMsg("api." + ctx + ".organizations.fleet-asset.add.v1")
	msg.Header = nats.Header{}
	for k, v := range extra {
		msg.Header[k] = v
	}
	msg.Header.Set("traceparent", "00-"+link+"-"+randHex(8)+"-"+flags)
	if body == nil {
		body = map[string]any{}
	}
	body["id"] = orgID
	body["registrationNo"] = "STK" + randHex(3)
	body["vin"] = "VIN" + randHex(5)
	body["make"] = "Volvo"
	body["model"] = "FH16"
	body["vehicleTypeCode"] = vehicleType
	msg.Data, _ = json.Marshal(body)
	t0 := time.Now()
	_, err := nc.RequestMsg(msg, 5*time.Second)
	return time.Since(t0), err
}

// ---- Tempo search, a stand-in for trace.search until 54c ----------------

func findByLink(link string, limit int) []string {
	q := url.Values{}
	// Tempo's TraceQL matches a link ID only without its leading zeros.
	q.Set("q", fmt.Sprintf(`{ link:traceID = "%s" }`, strings.TrimLeft(link, "0")))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("start", strconv.FormatInt(time.Now().Add(-3*time.Hour).Unix(), 10))
	q.Set("end", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
	req, _ := http.NewRequest(http.MethodGet, *grafanaURL+"/api/datasources/proxy/uid/tempo/api/search?"+q.Encode(), nil)
	req.SetBasicAuth("admin", envOr("GRAFANA_ADMIN_PASSWORD", "admin"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var out struct {
		Traces []struct {
			TraceID string `json:"traceID"`
		} `json:"traces"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	ids := []string{}
	for _, t := range out.Traces {
		// Tempo search drops leading zeros from the trace ID too.
		ids = append(ids, strings.Repeat("0", max(0, 32-len(t.TraceID)))+t.TraceID)
	}
	return ids
}

func waitByLink(link string, timeout time.Duration) (string, time.Duration) {
	t0 := time.Now()
	for time.Since(t0) < timeout {
		if ids := findByLink(link, 5); len(ids) > 0 {
			return ids[0], time.Since(t0)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", time.Since(t0)
}

// ---- gateway -----------------------------------------------------------

type trace struct {
	Schema  string `json:"schema"`
	TraceID string `json:"traceId"`
	Spans   []struct {
		ParentSpanID string         `json:"parentSpanId"`
		Kind         string         `json:"kind"`
		Service      string         `json:"service"`
		Attributes   map[string]any `json:"attributes"`
		Links        []struct {
			TraceID string `json:"traceId"`
		} `json:"links"`
	} `json:"spans"`
}

func gatewayGet(nc *nats.Conn, id string) (trace, error) {
	body, _ := json.Marshal(map[string]string{"traceId": id})
	msg, err := nc.Request(getSubject, body, 5*time.Second)
	if err != nil {
		return trace{}, err
	}
	if code := msg.Header.Get("Nats-Service-Error-Code"); code != "" {
		return trace{}, fmt.Errorf("service error %s: %s", code, msg.Header.Get("Nats-Service-Error"))
	}
	var t trace
	err = json.Unmarshal(msg.Data, &t)
	return t, err
}

// ---- checks ------------------------------------------------------------

func access() bool {
	admin := adminConn()
	defer admin.Close()
	pass := true
	known := ""

	// (1) The operator reads traces from every tenant.
	for _, tenant := range []string{"acme", "globex"} {
		nc := tenantConn(tenant)
		link := randHex(16)
		_, err := addFleet(nc, tenant, link, true, nil, nil)
		nc.Close()
		fmt.Printf("A1 %s call: err=%v\n", tenant, err)
		id, d := waitByLink(link, 90*time.Second)
		if id == "" {
			fmt.Printf("A1 %s: trace not found in Tempo after %s FAIL\n", tenant, d.Round(time.Second))
			pass = false
			continue
		}
		known = id
		t, err := gatewayGet(admin, id)
		recv := ""
		for _, s := range t.Spans {
			if s.Kind == "SERVER" && s.Service == "organizations-service" {
				recv, _ = s.Attributes["lab.tenant.receiving_account"].(string)
			}
		}
		ok := err == nil && t.Schema == "lab.trace.v1" && len(t.Spans) >= 3 && recv == tenant
		pass = pass && ok
		fmt.Printf("A1 admin token reads %s trace %s: err=%v spans=%d receiving_account=%q %s\n",
			tenant, id, err, len(t.Spans), recv, verdict(ok))
	}

	// (2) A tenant credential cannot reach the trace service or read data.
	// No responders or a permission error passes; any reply fails.
	if known == "" {
		known = randHex(16)
	}
	probe := func(label string, nc *nats.Conn) {
		for _, subj := range traceSubjects {
			body, _ := json.Marshal(map[string]string{"traceId": known})
			msg, err := nc.Request(subj, body, 3*time.Second)
			ok := msg == nil && (errors.Is(err, nats.ErrNoResponders) || isPermission(err))
			pass = pass && ok
			got := "no reply"
			if msg != nil {
				got = fmt.Sprintf("REPLY %d bytes", len(msg.Data))
			}
			fmt.Printf("A2 %s %s: err=%v %s %s\n", label, subj, err, got, verdict(ok))
		}
	}
	for _, tenant := range []string{"acme", "globex"} {
		nc := tenantConn(tenant)
		probe(tenant+" browser token", nc)
		nc.Close()
		nc = credsConn(tenant)
		probe(tenant+".creds", nc)
		nc.Close()
	}
	fmt.Println("ACCESS", verdict(pass))
	return pass
}

func trust() bool {
	admin := adminConn()
	defer admin.Close()
	acme := tenantConn("acme")
	defer acme.Close()
	pass := true

	spoof := nats.Header{}
	spoof.Set("Nats-Requestor", `{"acc":"globex"}`)
	spoof.Set("Nats-Request-Info", `{"acc":"globex","rtt":"1ms"}`)
	spoof.Set("tenant", "globex")
	body := map[string]any{"tenant": "globex", "organizationTenant": "globex"}

	for _, sampled := range []bool{false, true} {
		link := randHex(16)
		_, err := addFleet(acme, "acme", link, sampled, spoof, body)
		fmt.Printf("T call sampled=%v: err=%v\n", sampled, err)
		id, _ := waitByLink(link, 90*time.Second)
		if id == "" {
			// Not found means the sent flags turned recording off.
			fmt.Printf("T2 sampled=%v: no trace found FAIL\n", sampled)
			pass = false
			continue
		}
		t, err := gatewayGet(admin, id)
		if err != nil {
			fmt.Println("T get:", err, "FAIL")
			pass = false
			continue
		}
		for _, s := range t.Spans {
			if s.Kind != "SERVER" || s.Service != "organizations-service" {
				continue
			}
			ok2 := len(s.Links) == 1 && s.Links[0].TraceID == link &&
				t.TraceID != link && s.ParentSpanID == ""
			orig, has := s.Attributes["lab.tenant.originating"]
			recv := s.Attributes["lab.tenant.receiving_account"]
			ok1 := (!has || orig == "") && recv == "acme"
			pass = pass && ok1 && ok2
			fmt.Printf("T2 sampled=%v: trace=%s sent=%s parent=%q links=%d %s\n",
				sampled, t.TraceID, link, s.ParentSpanID, len(s.Links), verdict(ok2))
			fmt.Printf("T1 sampled=%v: receiving_account=%v originating=%v (present=%v) %s\n",
				sampled, recv, orig, has, verdict(ok1))
		}
	}
	fmt.Println("TRUST", verdict(pass))
	return pass
}

func delay(n int) {
	admin := adminConn()
	defer admin.Close()
	acme := tenantConn("acme")
	defer acme.Close()
	var search, get []float64
	for i := 0; i < n; i++ {
		link := randHex(16)
		t0 := time.Now()
		if _, err := addFleet(acme, "acme", link, true, nil, nil); err != nil {
			fmt.Println("call err:", err)
		}
		id, _ := waitByLink(link, 90*time.Second)
		if id == "" {
			fmt.Println("run", i, "not found")
			continue
		}
		ds := time.Since(t0)
		for time.Since(t0) < 120*time.Second {
			if t, err := gatewayGet(admin, id); err == nil && len(t.Spans) >= 3 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		dg := time.Since(t0)
		search = append(search, ds.Seconds())
		get = append(get, dg.Seconds())
		fmt.Printf("run %d: tempo-search %.1fs, gateway get (3 spans) %.1fs\n", i, ds.Seconds(), dg.Seconds())
	}
	fmt.Printf("DELAY search: %s\nDELAY get: %s\n", stats(search), stats(get))
}

func send(n int, batch string) {
	acme := tenantConn("acme")
	defer acme.Close()
	var lat []float64
	errs := 0
	for i := 0; i < n; i++ {
		d, err := addFleet(acme, "acme", batch, true, nil, nil)
		if err != nil {
			errs++
		}
		lat = append(lat, float64(d.Microseconds())/1000)
	}
	fmt.Printf("SEND n=%d errors=%d latency ms: %s\n", n, errs, stats(lat))
}

func waitCount(batch string, want, secs int) {
	t0 := time.Now()
	got := 0
	for time.Since(t0) < time.Duration(secs)*time.Second {
		if got = len(findByLink(batch, 2000)); got >= want {
			break
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Printf("COUNT %d of %d after %.0fs\n", got, want, time.Since(t0).Seconds())
}

func verify(batch string) {
	admin := adminConn()
	defer admin.Close()
	full, short := 0, 0
	for _, id := range findByLink(batch, 2000) {
		if t, err := gatewayGet(admin, id); err == nil && len(t.Spans) == 3 {
			full++
		} else {
			short++
		}
	}
	fmt.Printf("VERIFY traces with 3 spans: %d, incomplete: %d\n", full, short)
}

// ---- service list (BR-072) ---------------------------------------------

type snapshot struct {
	Schema    string   `json:"schema"`
	WindowMs  int64    `json:"windowMs"`
	Complete  bool     `json:"complete"`
	Truncated bool     `json:"truncated"`
	Errors    []string `json:"errors"`
	Accounts  []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"accounts"`
	Services []struct {
		Name      string `json:"name"`
		Account   string `json:"account"`
		Readiness string `json:"readiness"`
		Instances []struct {
			ID        string `json:"id"`
			Version   string `json:"version"`
			Endpoints []struct {
				Subject     string `json:"subject"`
				NumRequests int    `json:"numRequests"`
				NumErrors   int    `json:"numErrors"`
			} `json:"endpoints"`
		} `json:"instances"`
	} `json:"services"`
}

func serviceList(nc *nats.Conn) (snapshot, error) {
	msg, err := nc.Request(serviceListSubject, nil, 5*time.Second)
	if err != nil {
		return snapshot{}, err
	}
	if code := msg.Header.Get("Nats-Service-Error-Code"); code != "" {
		return snapshot{}, fmt.Errorf("service error %s: %s", code, msg.Header.Get("Nats-Service-Error"))
	}
	var s snapshot
	err = json.Unmarshal(msg.Data, &s)
	return s, err
}

// fleetAddCounts sums requests and errors on every acme endpoint whose
// subject is the fleet-asset add.
func fleetAddCounts(s snapshot) (reqs, errs, endpoints int) {
	for _, svc := range s.Services {
		if svc.Account != "acme" {
			continue
		}
		for _, in := range svc.Instances {
			for _, ep := range in.Endpoints {
				if strings.HasSuffix(ep.Subject, "fleet-asset.add.v1") {
					reqs += ep.NumRequests
					errs += ep.NumErrors
					endpoints++
				}
			}
		}
	}
	return
}

func printSnapshot(s snapshot) {
	fmt.Printf("schema=%s window=%dms complete=%v truncated=%v errors=%v\n",
		s.Schema, s.WindowMs, s.Complete, s.Truncated, s.Errors)
	for _, a := range s.Accounts {
		fmt.Printf("  account %-10s %s %s\n", a.Name, a.Status, a.Error)
	}
	for _, svc := range s.Services {
		ids := []string{}
		for _, in := range svc.Instances {
			ids = append(ids, in.ID)
		}
		fmt.Printf("  %-10s %-24s readiness=%-9s instances=%d %v\n",
			svc.Account, svc.Name, svc.Readiness, len(svc.Instances), ids)
	}
}

func snapshotCmd() {
	admin := adminConn()
	defer admin.Close()
	s, err := serviceList(admin)
	must(err)
	printSnapshot(s)
}

// watchCmd connects once, then prints a snapshot every 2 s for secs. The
// token is minted before anything is paused: accounts-service needs
// Postgres to mint one.
func watchCmd(secs int) {
	admin := adminConn()
	defer admin.Close()
	fmt.Println("connected; watching for", secs, "s")
	for t0 := time.Now(); time.Since(t0) < time.Duration(secs)*time.Second; time.Sleep(2 * time.Second) {
		fmt.Printf("--- t=%.0fs\n", time.Since(t0).Seconds())
		if s, err := serviceList(admin); err != nil {
			fmt.Println("service.list:", err)
		} else {
			printSnapshot(s)
		}
	}
}

func services() bool {
	admin := adminConn()
	defer admin.Close()
	pass := true

	// S1: one call lists PLATFORM and tenant services, complete.
	s, err := serviceList(admin)
	if err != nil {
		fmt.Println("S1 service.list:", err, "FAIL")
		return false
	}
	printSnapshot(s)
	accountsOK := len(s.Accounts) > 0
	for _, a := range s.Accounts {
		accountsOK = accountsOK && a.Status == "ok"
	}
	inPlatform, inTenant := false, false
	for _, svc := range s.Services {
		if svc.Account == "PLATFORM" {
			inPlatform = true
		} else {
			inTenant = true
		}
	}
	ok := s.Schema == "lab.servicelist.v1" && s.Complete && !s.Truncated && accountsOK && inPlatform && inTenant
	pass = pass && ok
	fmt.Printf("S1 complete=%v accounts ok=%v platform=%v tenant=%v %s\n",
		s.Complete, accountsOK, inPlatform, inTenant, verdict(ok))

	// S2: the endpoint statistics are live — they move by the calls sent.
	r0, e0, eps := fleetAddCounts(s)
	acme := tenantConn("acme")
	const n = 3
	for i := 0; i < n; i++ {
		_, _ = addFleet(acme, "acme", randHex(16), true, nil, nil)
	}
	acme.Close()
	s2, err := serviceList(admin)
	r1, e1, _ := fleetAddCounts(s2)
	ok = err == nil && eps > 0 && r1-r0 >= n
	pass = pass && ok
	fmt.Printf("S2 fleet-asset.add endpoints=%d requests %d→%d errors %d→%d (sent %d) err=%v %s\n",
		eps, r0, r1, e0, e1, n, err, verdict(ok))

	// S3: tenants cannot list; the admin token cannot discover by itself.
	refused := func(label, subj string, nc *nats.Conn) {
		// A publish permission violation arrives on the async error
		// handler; the request itself only times out.
		denied := make(chan error, 1)
		nc.SetErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, e error) {
			if isPermission(e) && strings.Contains(e.Error(), `"`+subj+`"`) {
				select {
				case denied <- e:
				default:
				}
			}
		})
		msg, err := nc.Request(subj, nil, 3*time.Second)
		if errors.Is(err, nats.ErrTimeout) {
			select {
			case e := <-denied:
				err = e
			default:
			}
		}
		ok := msg == nil && (errors.Is(err, nats.ErrNoResponders) || isPermission(err))
		pass = pass && ok
		got := "no reply"
		if msg != nil {
			got = fmt.Sprintf("REPLY %d bytes", len(msg.Data))
		}
		fmt.Printf("S3 %s %s: err=%v %s %s\n", label, subj, err, got, verdict(ok))
	}
	for _, tenant := range []string{"acme", "globex"} {
		nc := tenantConn(tenant)
		refused(tenant+" browser token", serviceListSubject, nc)
		nc.Close()
		nc = credsConn(tenant)
		refused(tenant+".creds", serviceListSubject, nc)
		nc.Close()
	}
	refused("admin token", "$SRV.PING", admin)
	refused("admin token", "monitor.acme.srv.PING", admin)

	fmt.Println("SERVICES", verdict(pass))
	return pass
}

// ---- load (BR-068 capacity and overflow, 54b-7b) -----------------------

// op is one call in the load workload. Every call carries a traceparent
// linked to the batch, so count and verify find the traces it made.
type op struct {
	name    string
	subject string
	// spans: how many spans one call makes (BR-071: a new root per api.* call).
	spans int
	body    func() []byte
	// wantErr: a service-error reply is this op's normal outcome.
	wantErr bool
}

// workload is the fixed mix, sent in this order by every worker: one write
// that crosses two services (3 spans) and two reads with list replies (1
// span each), so 5 spans per 3 calls. With OTEL_CAPTURE_BODIES=true every
// span also carries its request and reply bodies.
func workload() []op {
	return []op{
		{name: "fleet-asset.add", spans: 3, subject: "api.acme.organizations.fleet-asset.add.v1", wantErr: true, body: func() []byte {
			b, _ := json.Marshal(map[string]any{
				"id": orgID, "registrationNo": "STK" + randHex(3), "vin": "VIN" + randHex(5),
				"make": "Volvo", "model": "FH16", "vehicleTypeCode": vehicleType,
			})
			return b
		}},
		{name: "refdata.type.list", spans: 1, subject: "api.acme.refdata.type.list.v1", body: func() []byte {
			return []byte(`{"typeKey":"vehicle-type","locale":"en","all":false}`)
		}},
		{name: "shipping.ship.list", spans: 1, subject: "api.acme.shipping.ship.list.v1", body: func() []byte {
			return []byte(`{}`)
		}},
	}
}

// Sustainable-load criteria (54b-7b). A run at a concurrency is sustainable
// when both hold; the lab peak is the highest sustainable call rate.
const (
	maxBadRate = 0.001 // at most 0.1% unexpected outcomes
	maxP95Ms   = 250.0 // p95 latency over every call, in ms
)

// load runs workers browser connections for secs seconds. Each worker sends
// the workload's ops in turn, one call at a time, with no think time. It
// prints the call rate, latency and bad outcomes, and returns whether the
// run met the criteria above.
func load(secs, workers int, batch string) bool {
	ops := workload()
	type result struct {
		op  int
		ms  float64
		bad bool
	}
	results := make(chan result, 4096)
	conns := make([]*nats.Conn, workers)
	for w := range conns {
		conns[w] = tenantConn("acme")
	}
	start := time.Now()
	deadline := start.Add(time.Duration(secs) * time.Second)
	var wg sync.WaitGroup
	for w, nc := range conns {
		wg.Add(1)
		go func(w int, nc *nats.Conn) {
			defer wg.Done()
			defer nc.Close()
			for i := w; time.Now().Before(deadline); i++ {
				k := i % len(ops)
				d, bad := call(nc, ops[k], batch)
				results <- result{k, float64(d.Microseconds()) / 1000, bad}
			}
		}(w, nc)
	}
	go func() { wg.Wait(); close(results) }()

	all := []float64{}
	perOp := make([][]float64, len(ops))
	badOp := make([]int, len(ops))
	bad := 0
	for r := range results {
		all = append(all, r.ms)
		perOp[r.op] = append(perOp[r.op], r.ms)
		if r.bad {
			bad++
			badOp[r.op]++
		}
	}
	elapsed := time.Since(start).Seconds()
	n := len(all)
	badRate := float64(bad) / float64(max(n, 1))
	p95 := pct(all, 0.95)
	ok := n > 0 && badRate <= maxBadRate && p95 <= maxP95Ms
	fmt.Printf("LOAD workers=%d secs=%.1f calls=%d rate=%.1f/s bad=%d (%.3f%%) latency ms: %s\n",
		workers, elapsed, n, float64(n)/elapsed, bad, 100*badRate, stats(all))
	spans := 0
	for k, o := range ops {
		spans += o.spans * len(perOp[k])
		fmt.Printf("  %-22s calls=%d bad=%d latency ms: %s\n", o.name, len(perOp[k]), badOp[k], stats(perOp[k]))
	}
	// Compare with the Collector's otelcol_receiver_accepted_spans delta: a
	// smaller delta means the SDK dropped spans before export.
	fmt.Printf("SPANS expected=%d rate=%.1f/s\n", spans, float64(spans)/elapsed)
	fmt.Printf("SUSTAINABLE (bad <= %.1f%%, p95 <= %.0f ms): %s\n", 100*maxBadRate, maxP95Ms, verdict(ok))
	return ok
}

// call sends one op. A call is bad when the request fails, or when the reply
// is a service error and the op does not expect one, or the other way round.
func call(nc *nats.Conn, o op, batch string) (time.Duration, bool) {
	msg := nats.NewMsg(o.subject)
	msg.Header = nats.Header{}
	msg.Header.Set("traceparent", "00-"+batch+"-"+randHex(8)+"-01")
	msg.Data = o.body()
	t0 := time.Now()
	reply, err := nc.RequestMsg(msg, 5*time.Second)
	d := time.Since(t0)
	if err != nil {
		return d, true
	}
	gotErr := reply.Header.Get("Nats-Service-Error-Code") != ""
	return d, gotErr != o.wantErr
}

func pct(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(q*float64(len(s)-1))]
}

// ---- the load dataset (54b-7b) -----------------------------------------
//
// The load's list replies are only representative when the lists are not
// empty. The dataset, in acme: the 114 vehicle types of refdata-service's
// cmd/seed-vehicle-types (run separately), and the ports and ships below.
// Safe to re-run: a repeat registration is reported and skipped.

var seedPorts = []string{"DURBAN", "CAPE-TOWN", "PORT-ELIZABETH", "RICHARDS-BAY", "WALVIS-BAY"}

const seedShipCount = 20

func seedShips() bool {
	nc := tenantConn("acme")
	defer nc.Close()
	ok := true
	req := func(subject string, body map[string]any) {
		b, _ := json.Marshal(body)
		msg, err := nc.Request(subject, b, 5*time.Second)
		switch {
		case err != nil:
			fmt.Printf("SEED %s %v: %v\n", subject, body, err)
			ok = false
		case msg.Header.Get("Nats-Service-Error-Code") != "":
			fmt.Printf("SEED %s %v: %s %s\n", subject, body, msg.Header.Get("Nats-Service-Error-Code"), msg.Header.Get("Nats-Service-Error"))
		}
	}
	for _, p := range seedPorts {
		req("api.acme.shipping.port.register.v1", map[string]any{"name": p})
	}
	for i := 1; i <= seedShipCount; i++ {
		id := fmt.Sprintf("STK-SHIP-%02d", i)
		req("api.acme.shipping.ship.register.v1", map[string]any{"shipID": id, "shipName": fmt.Sprintf("MV Stackcheck %02d", i)})
		req("api.acme.shipping.ship.arrive.v1", map[string]any{"shipID": id, "port": seedPorts[i%len(seedPorts)]})
	}
	fmt.Printf("SEED ports=%d ships=%d: %s\n", len(seedPorts), seedShipCount, verdict(ok))
	return ok
}

// ---- flood (BR-068 volume capacity, 54b-7b) ----------------------------
//
// The services cannot send faster than the measured peak, so a 2-hour
// volume through them takes 2 hours. flood fills the same volume faster: it
// replays real traces, captured from Tempo with their real attributes and
// body events, into the real Collector. Only the trace, span and link IDs
// change. Timestamps are the captured ones. This is volume-capacity
// evidence, never evidence of a 2-hour elapsed outage.

// template is one captured trace, in OTLP/JSON, with its IDs replaced by
// placeholders: __T__ (trace), __S0__… (spans), __L__ and __LS__ (link).
type template struct {
	Root  string `json:"root"`
	Spans int    `json:"spans"`
	OTLP  string `json:"otlp"` // a JSON array of resourceSpans
}

// capture saves one trace per root span name from the traces linked to
// batch, which a `load` run made.
func capture(batch, out string) {
	q := url.Values{}
	q.Set("q", fmt.Sprintf(`{ link:traceID = "%s" }`, strings.TrimLeft(batch, "0")))
	q.Set("limit", "300")
	q.Set("start", strconv.FormatInt(time.Now().Add(-3*time.Hour).Unix(), 10))
	q.Set("end", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
	var found struct {
		Traces []struct {
			TraceID       string `json:"traceID"`
			RootTraceName string `json:"rootTraceName"`
		} `json:"traces"`
	}
	must(grafanaJSON("/api/search?"+q.Encode(), &found))
	seen := map[string]bool{}
	var out_ []template
	for _, t := range found.Traces {
		if seen[t.RootTraceName] {
			continue
		}
		var tr struct {
			Trace struct {
				ResourceSpans []any `json:"resourceSpans"`
			} `json:"trace"`
		}
		if err := grafanaJSON("/api/v2/traces/"+t.TraceID, &tr); err != nil || len(tr.Trace.ResourceSpans) == 0 {
			continue
		}
		restored := restoreBodies(tr.Trace.ResourceSpans)
		tm, err := toTemplate(t.RootTraceName, tr.Trace.ResourceSpans)
		if err != nil {
			fail(err.Error())
		}
		seen[t.RootTraceName] = true
		out_ = append(out_, tm)
		fmt.Printf("CAPTURED %-44s spans=%d otlp_json_bytes=%d restored_bodies=%d\n", tm.Root, tm.Spans, len(tm.OTLP), restored)
	}
	if len(out_) == 0 {
		fail("no traces found for batch " + batch)
	}
	b, _ := json.MarshalIndent(out_, "", "  ")
	must(os.WriteFile(out, b, 0o644))
}

func grafanaJSON(path string, v any) error {
	req, _ := http.NewRequest(http.MethodGet, *grafanaURL+"/api/datasources/proxy/uid/tempo"+path, nil)
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth("admin", envOr("GRAFANA_ADMIN_PASSWORD", "admin"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// restoreBodies undoes Tempo's cut. Tempo stores an attribute of at most
// max_attribute_bytes (2048 by default), but a service exports a body of up
// to otelnats.MaxBodyBytes (4096). So a stored reply body can be shorter
// than the one the Collector queued. For each such reply on an api.* span,
// restoreBodies sends the captured request again, takes the real reply, and
// puts it through otelnats's own body pipeline (redact, then cut at 4096).
// The event's attributes become the ones the service would export. It
// returns how many events it changed.
func restoreBodies(rs []any) int {
	subjects := map[string]bool{}
	for _, o := range workload() {
		subjects[o.subject] = true
	}
	var nc *nats.Conn
	n := 0
	for _, r := range rs {
		for _, ss := range asList(asMap(r)["scopeSpans"]) {
			for _, s := range asList(asMap(ss)["spans"]) {
				sp := asMap(s)
				name := str(sp["name"])
				var req, reply map[string]any
				for _, e := range asList(sp["events"]) {
					switch em := asMap(e); str(em["name"]) {
					case otelnats.EventRequestBody:
						req = em
					case otelnats.EventReplyBody:
						reply = em
					}
				}
				if reply == nil {
					continue
				}
				stored, size := len(eventAttr(reply, otelnats.AttrBody)), atoi(eventAttr(reply, otelnats.AttrBodyBytes))
				if stored >= min(size, otelnats.MaxBodyBytes) {
					continue
				}
				if !subjects[name] || req == nil {
					fail(fmt.Sprintf("span %s: reply body cut by Tempo (%d of %d bytes) and not restorable", name, stored, size))
				}
				if nc == nil {
					nc = tenantConn("acme")
					defer nc.Close()
				}
				msg, err := nc.Request(name, []byte(eventAttr(req, otelnats.AttrBody)), 5*time.Second)
				must(err)
				reply["attributes"] = exportedBody(msg.Data, msg.Header)
				n++
			}
		}
	}
	return n
}

// eventAttr is one attribute of an OTLP/JSON event, as a string.
func eventAttr(e map[string]any, key string) string {
	for _, a := range asList(e["attributes"]) {
		am := asMap(a)
		if str(am["key"]) != key {
			continue
		}
		v := asMap(am["value"])
		if s, ok := v["stringValue"].(string); ok {
			return s
		}
		return str(v["intValue"])
	}
	return ""
}

// exportedBody runs body through otelnats's reply-body pipeline and returns
// the event's attributes in OTLP/JSON form.
func exportedBody(body []byte, h nats.Header) []any {
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "stackcheck-restore", Exporter: exp, CaptureBodies: true})
	must(err)
	_, span := p.Tracer().Start(context.Background(), "restore")
	p.RecordReplyBody(span, body, h)
	span.End()
	must(p.ForceFlush(context.Background()))
	spans := exp.GetSpans()
	if len(spans) != 1 || len(spans[0].Events) != 1 {
		fail("restore: otelnats made no body event")
	}
	var out []any
	for _, kv := range spans[0].Events[0].Attributes {
		var v map[string]any
		switch kv.Value.Type() {
		case attribute.STRING:
			v = map[string]any{"stringValue": kv.Value.AsString()}
		case attribute.INT64:
			v = map[string]any{"intValue": strconv.FormatInt(kv.Value.AsInt64(), 10)}
		case attribute.BOOL:
			v = map[string]any{"boolValue": kv.Value.AsBool()}
		case attribute.STRINGSLICE:
			vals := []any{}
			for _, s := range kv.Value.AsStringSlice() {
				vals = append(vals, map[string]any{"stringValue": s})
			}
			v = map[string]any{"arrayValue": map[string]any{"values": vals}}
		default:
			fail("restore: unexpected attribute type " + kv.Value.Type().String())
		}
		out = append(out, map[string]any{"key": string(kv.Key), "value": v})
	}
	return out
}

// toTemplate swaps every ID in a captured trace for a placeholder.
func toTemplate(root string, rs []any) (template, error) {
	spanIDs := map[string]string{}
	place := func(id string) string {
		if id == "" {
			return ""
		}
		p, ok := spanIDs[id]
		if !ok {
			p = fmt.Sprintf("__S%d__", len(spanIDs))
			spanIDs[id] = p
		}
		return p
	}
	spans := 0
	for _, r := range rs {
		for _, ss := range asList(asMap(r)["scopeSpans"]) {
			for _, s := range asList(asMap(ss)["spans"]) {
				sp := asMap(s)
				spans++
				sp["traceId"] = "__T__"
				sp["spanId"] = place(str(sp["spanId"]))
				if p := str(sp["parentSpanId"]); p != "" {
					sp["parentSpanId"] = place(p)
				}
				for _, l := range asList(sp["links"]) {
					lm := asMap(l)
					lm["traceId"] = "__L__"
					lm["spanId"] = "__LS__"
				}
			}
		}
	}
	b, err := json.Marshal(rs)
	return template{Root: root, Spans: spans, OTLP: string(b)}, err
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }
func str(v any) string           { s, _ := v.(string); return s }

// instance fills one template with fresh IDs and returns it and its trace ID.
func (t template) instance(link string) (string, string) {
	tid := randHex(16)
	pairs := []string{"__T__", tid, "__L__", link, "__LS__", randHex(8)}
	for i := 0; i < t.Spans; i++ {
		pairs = append(pairs, fmt.Sprintf("__S%d__", i), randHex(8))
	}
	return strings.NewReplacer(pairs...).Replace(t.OTLP), tid
}

// flood sends at least total spans to the Collector from workers senders,
// 100 traces per OTLP request, the templates in turn. It writes about 2000
// sampled trace IDs, with their span counts, to sampleOut.
func flood(tmplPath string, total, workers int, link, sampleOut string) {
	var tmpls []template
	b, err := os.ReadFile(tmplPath)
	must(err)
	must(json.Unmarshal(b, &tmpls))
	perCycle := 0
	for _, t := range tmpls {
		perCycle += t.Spans
	}
	const tracesPerReq = 100
	spansPerReq := perCycle * tracesPerReq / len(tmpls)
	reqs := (total + spansPerReq - 1) / spansPerReq
	sampleEvery := max(1, reqs/2000)

	var mu sync.Mutex
	var sent, refused, sentBytes int64
	samples := []string{}
	jobs := make(chan int, workers)
	go func() {
		for i := 0; i < reqs; i++ {
			jobs <- i
		}
		close(jobs)
	}()
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: workers}}
	start := time.Now()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(10 * time.Second):
				mu.Lock()
				fmt.Printf("FLOOD progress spans=%d refused_reqs=%d rate=%.0f/s\n", sent, refused, float64(sent)/time.Since(start).Seconds())
				mu.Unlock()
			}
		}
	}()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var sb strings.Builder
			for i := range jobs {
				sb.Reset()
				sb.WriteString(`{"resourceSpans":[`)
				n := 0
				var sample string
				for k := 0; k < tracesPerReq; k++ {
					t := tmpls[(i*tracesPerReq+k)%len(tmpls)]
					body, tid := t.instance(link)
					if k > 0 {
						sb.WriteByte(',')
					}
					sb.WriteString(body[1 : len(body)-1]) // strip the array brackets
					n += t.Spans
					if k == 0 && i%sampleEvery == 0 {
						sample = fmt.Sprintf("%s %d", tid, t.Spans)
					}
				}
				sb.WriteString(`]}`)
				payload := sb.String()
				resp, err := client.Post(*collector+"/v1/traces", "application/json", strings.NewReader(payload))
				ok := err == nil && resp.StatusCode/100 == 2
				if resp != nil {
					resp.Body.Close()
				}
				mu.Lock()
				if ok {
					sent += int64(n)
					sentBytes += int64(len(payload))
					if sample != "" {
						samples = append(samples, sample)
					}
				} else {
					refused++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(done)
	el := time.Since(start).Seconds()
	must(os.WriteFile(sampleOut, []byte(strings.Join(samples, "\n")+"\n"), 0o644))
	fmt.Printf("FLOOD done spans=%d refused_reqs=%d secs=%.0f rate=%.0f/s otlp_json_bytes=%d samples=%d\n",
		sent, refused, el, float64(sent)/el, sentBytes, len(samples))
}

// floodVerify reads every sampled trace through the gateway and checks it
// has all its spans.
func floodVerify(sampleFile string) bool {
	b, err := os.ReadFile(sampleFile)
	must(err)
	admin := adminConn()
	defer admin.Close()
	full, short := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var id string
		var want int
		if _, err := fmt.Sscanf(line, "%s %d", &id, &want); err != nil {
			continue
		}
		if t, err := gatewayGet(admin, id); err == nil && len(t.Spans) == want {
			full++
		} else {
			short++
		}
	}
	fmt.Printf("FLOODVERIFY sampled traces complete: %d, missing or short: %d\n", full, short)
	return short == 0 && full > 0
}

// ---- helpers -----------------------------------------------------------

func stats(xs []float64) string {
	if len(xs) == 0 {
		return "none"
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	p := func(q float64) float64 { return s[int(q*float64(len(s)-1))] }
	return fmt.Sprintf("min %.2f p50 %.2f p95 %.2f max %.2f", s[0], p(0.5), p(0.95), s[len(s)-1])
}

func isPermission(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "permission")
}

func verdict(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func arg(args []string, i int) string {
	if i >= len(args) {
		fail("missing argument for " + args[0])
	}
	return args[i]
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		fail("not a number: " + s)
	}
	return n
}

func exit(pass bool) {
	if !pass {
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(s string) {
	fmt.Fprintln(os.Stderr, s)
	os.Exit(1)
}
