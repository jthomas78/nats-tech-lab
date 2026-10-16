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
// The outage and durability checks (BR-066, BR-068) are send/count/verify
// runs around `docker stop|pause|restart|kill` of the Collector and Tempo;
// the plan lists the sequence. access and trust exit 1 on any FAIL.
//
// Defaults are the za-1 cell's ports; pass the au-1 ports with flags.
package main

import (
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
	"time"

	"github.com/nats-io/nats.go"
)

const (
	getSubject         = "api._platform.observability.trace.get.v1"
	serviceListSubject = "api._platform.observability.service.list.v1"
	// An organization seeded in acme. The vehicle type below is not in
	// refdata, so every call ends in ERROR — and still yields all 3 spans.
	orgID       = "01M4ZQ0H2MF81CJX8VPWQR39T4"
	vehicleType = "TAUTLINER"
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
