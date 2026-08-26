// Command seed-traces is Phase 48h's verification instrument (BR-054): the
// one thing in this repo that exercises a *multi-hop* trace, across a NATS
// account boundary, in both the OK and the ERROR outcome.
//
// Every other piece of trace coverage here is per-service and single-hop —
// trace_async_test.go, trace_middleware_test.go, browserrpc_test.go,
// refdata-service's natsrpc_test.go, organizations-service's
// browserrpc_roundtrip_test.go — each asserting that its *own* span is
// emitted correctly against a trace it hand-built in a fixture. None of them
// produces a traceId with a real parent/child chain across two services, and
// none crosses an account boundary, so neither BR-051's tenant attribution
// nor BR-053's concurrent-write property can be demonstrated by them.
//
// The chain it drives is a real one, live in the running stack:
//
//	tenant account          | PLATFORM account
//	------------------------+------------------------------------------
//	api.{ctx}.organizations.fleet-asset.add.v1   (organizations-service)
//	  └─ rpc.{ctx}.refdata.item.get.v1  ────────▶ refdata-service
//
// organizations-service's handleFleetAssetAdd threads the inbound span down
// through FleetAssetHandler to refdataclient's outbound rpc.* call (BR-037),
// so one api.* request yields three spans under one traceId: the api handler,
// the outbound rpc hop, and refdata's own handler on the far side of the
// account boundary.
//
// That boundary is also what makes the trace's attribution mixed rather than
// uniform: the first two spans are published inside the tenant's account and
// attribute to it, while refdata's span is published inside PLATFORM and
// attributes to "_platform". BR-051 is per span for exactly this reason, so
// -expect-tenant asserts that the named tenant appears and that PLATFORM is
// the only other value — not that every span carries the same one.
//
// BR-054's own wording names shipping-service as the middle hop. That is
// wrong and this harness deliberately does not follow it: Phase 32 removed
// shipping-service's five refdata relay routes, leaving
// internal/refdataconsumer with no live callers at all. organizations-service
// is the only live cross-service, cross-account chain in the stack.
//
// # The two runs
//
// The outcome is chosen by the vehicleTypeCode, because BR-TP14 resolves it
// against refdata *over the rpc hop*:
//
//   - OK    — a code that exists in the context's vehicle-type corpus. Every
//     span reports StatusCode OK.
//
//   - ERROR — a code that cannot exist. This is the more interesting shape
//     than a transport failure, and the observed span statuses are worth
//     stating exactly, because they are not the obvious guess:
//
//     api.{ctx}.organizations.fleet-asset.add.v1   ERROR  (organizations)
//     refdata.item.get.v1                          OK     (organizations)
//     rpc.{ctx}.refdata.item.get.v1                ERROR  (refdata)
//
//     The middle span is the *caller-side* outbound hop, named for the local
//     alias the tenant account publishes on; it closes OK because the
//     request/reply round trip genuinely succeeded — "not found" is a
//     delivered answer, not a transport failure. The third is refdata's own
//     inbound handler span, named for the subject the server remapped that
//     alias to, and it reports ERROR because the handler answered over
//     browserrpc's respondError path. So the two ERROR spans sit at the two
//     ends of the domain decision with an OK hop between them, and the root
//     closes in ERROR over a child that closed OK — which is exactly the
//     shape the waterfall has never been checked against.
//
//     Whether a domain not-found *should* read as ERROR on the callee's own
//     span is a fair question, but not one this harness can settle:
//     natstrace has only OK and ERROR, so drawing that distinction means
//     widening the status vocabulary everywhere. Recorded here rather than
//     quietly asserted around.
//
// # Credentials
//
// It drives the tenant hop with the tenant's own credential rather than a
// browser token, which BR-054's Test line describes. That is not a shortcut:
// /api/auth/connectInfo answers 409 for the day-0 bootstrap accounts
// (PLATFORM/ACME/GLOBEX), which have no signing key on record, so a browser
// token is not obtainable for them at all. It costs nothing under test —
// the tenant token BR-051 attributes is inserted by the NATS server from the
// *account* the publisher is in, and both credentials are in the same one.
//
// # Usage
//
//	go run ./cmd/seed-traces [-context acme] [-creds ../../nats/creds/acme.creds]
//
// Prerequisites, both checked and reported rather than failing halfway:
//
//   - a TRANSPORTER organization in the context — go run ./cmd/seed-transporters   (organizations-service)
//   - the vehicle-type corpus in the context    — go run ./cmd/seed-vehicle-types  (refdata-service)
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	requestTimeout = 10 * time.Second

	// bucketName/keyPrefix/kvContext mirror tracestore's own constants. They
	// are duplicated rather than exported from it on purpose: this harness
	// verifies tracestore's stored shape from the outside, and importing the
	// constants it is checking would let a rename silently move both sides
	// together.
	bucketName = "trace-request-reply"
	kvContext  = "_platform"
	keyPrefix  = "trace."

	vehicleTypeKey = "vehicle-type"

	// unknownVehicleType drives the ERROR run. The run-unique suffix keeps it
	// from ever colliding with a real corpus entry someone adds later.
	unknownVehicleTypePrefix = "NO_SUCH_VEHICLE_TYPE_"
)

func main() {
	contextKey := flag.String("context", "acme", "business-unit context to drive")
	credsPath := flag.String("creds", "../../nats/creds/acme.creds", "tenant NATS credentials (drives the api.* call)")
	platformCredsPath := flag.String("platform-creds", "../../nats/creds/platform.creds", "PLATFORM NATS credentials (reads the stored trace)")
	natsURL := flag.String("nats-url", nats.DefaultURL, "NATS URL")
	// 30s, not the 10s this started at: on a cold-ish stack the projector has
	// been measured taking 10-13s to store all three spans of a chain, so the
	// old default reported "never reached 3 spans (last saw 0)" for a trace
	// that was merely late. A timeout that fires before the thing it waits for
	// is a worse harness than a slow one.
	settle := flag.Duration("settle", 30*time.Second, "how long to wait for the trace projector to store every span")
	expectTenant := flag.String("expect-tenant", "", "assert BR-051's attribution: this tenant must appear and be the only non-PLATFORM value")
	doMeasure := flag.Bool("measure", false, "after the runs, report the stored trace-record size distribution (BR-053's sizing input)")
	runCount := flag.Int("runs", 1, "repeat the OK/ERROR pair N times — use a larger N with -measure to build a multi-span sample worth sizing against")
	flag.Parse()

	// nats.Name is required of every connection in this repo — an anonymous
	// one is indistinguishable in `nats server list connections`.
	tenantConn, err := nats.Connect(*natsURL, nats.UserCredentials(*credsPath), nats.Name("seed-traces"))
	if err != nil {
		log.Fatalf("connect to NATS as the tenant: %v", err)
	}
	defer tenantConn.Close()

	platformConn, err := nats.Connect(*natsURL, nats.UserCredentials(*platformCredsPath), nats.Name("seed-traces-verify"))
	if err != nil {
		log.Fatalf("connect to NATS as PLATFORM: %v", err)
	}
	defer platformConn.Close()

	js, err := jetstream.New(platformConn)
	if err != nil {
		log.Fatalf("jetstream: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	kv, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		log.Fatalf("open kv bucket %s: %v", bucketName, err)
	}

	h := &harness{
		tenant:       tenantConn,
		kv:           kv,
		contextKey:   *contextKey,
		settle:       *settle,
		expectTenant: *expectTenant,
	}

	partnerID, vehicleType, err := h.prerequisites(ctx)
	if err != nil {
		log.Fatalf("%v", err)
	}
	log.Printf("driving context %q against transporter %s with vehicle type %q", *contextKey, partnerID, vehicleType)

	runs := []struct {
		name        string
		vehicleType string
		wantErr     bool
	}{
		{"OK", vehicleType, false},
		{"ERROR", unknownVehicleTypePrefix + randomHex(4), true},
	}

	// Repeat runs are quiet: the per-span dump is what makes a single pair
	// readable, and 2N of them is what makes a sizing run unreadable.
	h.quiet = *runCount > 1

	var failures int
	for i := 0; i < *runCount; i++ {
		for _, r := range runs {
			// Each ERROR run needs its own unresolvable code, or the second
			// one measures a cached/repeated lookup rather than a fresh miss.
			vt := r.vehicleType
			if r.wantErr {
				vt = unknownVehicleTypePrefix + randomHex(4)
			}
			if err := h.run(ctx, r.name, partnerID, vt, r.wantErr); err != nil {
				log.Printf("FAIL  %-5s  %v", r.name, err)
				failures++
				continue
			}
			if !h.quiet {
				log.Printf("PASS  %-5s", r.name)
			}
		}
	}
	if h.quiet {
		log.Printf("PASS  %d run(s) of the OK/ERROR pair", *runCount)
	}
	if *doMeasure {
		if err := measure(ctx, kv); err != nil {
			log.Printf("FAIL  measure  %v", err)
			failures++
		}
	}

	if failures > 0 {
		os.Exit(1)
	}
}

type harness struct {
	tenant       *nats.Conn
	kv           jetstream.KeyValue
	contextKey   string
	settle       time.Duration
	expectTenant string
	quiet        bool
}

// storedSpan is the subset of natstrace's wire span this harness asserts on,
// flattened: Tenant is BR-051's attribution, which the projector stores in a
// wrapper *around* the span rather than inside it (see wireSpan below), and
// the rest are natstrace's own fields. Tenant is absent from records written
// before 48b/48c, which is why -expect-tenant stays opt-in rather than
// always-on — a hard assertion here would make an older bucket unreadable.
type storedSpan struct {
	TraceID       string `json:"traceId"`
	SpanID        string `json:"spanId"`
	ParentSpanID  string `json:"parentSpanId"`
	Service       string `json:"service"`
	Entity        string `json:"entity"`
	Action        string `json:"action"`
	Subject       string `json:"subject"`
	StatusCode    string `json:"statusCode"`
	StatusMessage string `json:"statusMessage"`
	DurationMs    int64  `json:"durationMs"`
	Tenant        string `json:"tenant"`
}

type traceRecord struct {
	TraceID string            `json:"traceId"`
	Spans   []json.RawMessage `json:"spans"`
}

// wireSpan is the projector's stored element as of 48c: the span document the
// observed account wrote, wrapped in the account token the NATS server
// inserted. The two are kept apart on the wire precisely so the second cannot
// be forged by the first, so this harness unwraps rather than decoding the
// span's own "tenant" field — there isn't one.
type wireSpan struct {
	Tenant string          `json:"tenant"`
	Span   json.RawMessage `json:"span"`
}

// decodeSpan reads either shape: the 48c wrapper, or a bare span from a record
// written before it. A bare span simply has no attribution, and reports as
// such rather than being dropped.
func decodeSpan(raw json.RawMessage) (storedSpan, error) {
	var wrapper wireSpan
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Span) > 0 {
		var s storedSpan
		if err := json.Unmarshal(wrapper.Span, &s); err != nil {
			return storedSpan{}, err
		}
		s.Tenant = wrapper.Tenant
		return s, nil
	}
	var s storedSpan
	if err := json.Unmarshal(raw, &s); err != nil {
		return storedSpan{}, err
	}
	s.Tenant = ""
	return s, nil
}

// run issues one api.* call under a traceparent this harness minted itself —
// so the traceId is known before the request goes out, rather than scraped
// back out of a reply — then waits for the projector and asserts on what it
// stored.
func (h *harness) run(ctx context.Context, name, partnerID, vehicleType string, wantErr bool) error {
	traceID, rootSpanID := randomHex(16), randomHex(8)

	msg := nats.NewMsg(fmt.Sprintf("api.%s.organizations.fleet-asset.add.v1", h.contextKey))
	msg.Header.Set("Traceparent", fmt.Sprintf("00-%s-%s-01", traceID, rootSpanID))
	body, err := json.Marshal(map[string]any{
		"id":              partnerID,
		"registrationNo":  "TRACE-" + strings.ToUpper(name) + "-" + randomHex(3),
		"vin":             "",
		"make":            "Volvo",
		"model":           "FH16",
		"vehicleTypeCode": vehicleType,
	})
	if err != nil {
		return err
	}
	msg.Data = body

	reply, err := h.tenant.RequestMsg(msg, requestTimeout)
	if err != nil {
		return fmt.Errorf("%s: %w", msg.Subject, err)
	}
	gotErr := reply.Header.Get("Nats-Service-Error") != ""
	if gotErr != wantErr {
		return fmt.Errorf("expected error=%v, got error=%v (%s)", wantErr, gotErr, reply.Header.Get("Nats-Service-Error"))
	}

	spans, err := h.awaitTrace(ctx, traceID, 3)
	if err != nil {
		return err
	}
	return h.assert(spans, rootSpanID, wantErr)
}

// awaitTrace polls the KV entry until it holds at least want spans or settle
// expires. Polling rather than watching is deliberate: a watch would report
// the record's first revision immediately and the harness would then have to
// decide when to stop waiting anyway, with the extra failure mode of a missed
// update. The last-seen count is reported on timeout so a short trace is
// distinguishable from no trace at all.
func (h *harness) awaitTrace(ctx context.Context, traceID string, want int) ([]storedSpan, error) {
	key := kvContext + "." + keyPrefix + traceID
	deadline := time.Now().Add(h.settle)
	var last int
	for {
		entry, err := h.kv.Get(ctx, key)
		switch {
		case err == nil:
			var record traceRecord
			if unmarshalErr := json.Unmarshal(entry.Value(), &record); unmarshalErr != nil {
				return nil, fmt.Errorf("decoding trace record %s: %w", key, unmarshalErr)
			}
			if len(record.Spans) >= want {
				spans := make([]storedSpan, 0, len(record.Spans))
				for _, raw := range record.Spans {
					s, unmarshalErr := decodeSpan(raw)
					if unmarshalErr != nil {
						return nil, fmt.Errorf("decoding span in %s: %w", key, unmarshalErr)
					}
					spans = append(spans, s)
				}
				return spans, nil
			}
			last = len(record.Spans)
		case errors.Is(err, jetstream.ErrKeyNotFound):
			// projector has not written the first span yet
		default:
			return nil, fmt.Errorf("reading %s: %w", key, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("trace %s never reached %d spans (last saw %d) within %s", traceID, want, last, h.settle)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// assert checks the four properties BR-054 names: span count, parent/child
// linkage, the error run's status fields with a closed parent, and — once
// 48b lands and -expect-tenant is set — BR-051's attribution.
func (h *harness) assert(spans []storedSpan, rootSpanID string, wantErr bool) error {
	byID := make(map[string]storedSpan, len(spans))
	for _, s := range spans {
		byID[s.SpanID] = s
	}

	// Print the trace before asserting on it. This is a diagnostic harness as
	// much as a check: a bare "expected 1 ERROR span, got 2" sends the reader
	// to the KV bucket to find out which two, and the answer is right here.
	if !h.quiet {
		for _, s := range spans {
			log.Printf("      span %s parent=%s %-6s %s", s.SpanID, parentLabel(s, rootSpanID), s.StatusCode, s.Subject)
		}
	}

	// Linkage: exactly one span descends from the traceparent this harness
	// injected, and every other span's parent is a span in this same trace.
	var roots []storedSpan
	for _, s := range spans {
		switch {
		case s.ParentSpanID == rootSpanID:
			roots = append(roots, s)
		case s.ParentSpanID == "":
			return fmt.Errorf("span %s (%s) has no parent — the injected traceparent was not continued", s.SpanID, s.Subject)
		default:
			if _, ok := byID[s.ParentSpanID]; !ok {
				return fmt.Errorf("span %s (%s) has parent %s, which is not in this trace", s.SpanID, s.Subject, s.ParentSpanID)
			}
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("expected exactly 1 span under the injected traceparent, got %d", len(roots))
	}
	root := roots[0]

	// The chain has to actually cross the service boundary, or the linkage
	// above would be satisfied by three spans from one service.
	services := map[string]bool{}
	for _, s := range spans {
		services[s.Service] = true
	}
	if !services["organizations"] || !services["refdata"] {
		return fmt.Errorf("expected spans from both organizations and refdata, got %v", keys(services))
	}

	var errored []storedSpan
	for _, s := range spans {
		if s.StatusCode == "ERROR" {
			errored = append(errored, s)
		}
	}

	if !wantErr {
		if len(errored) != 0 {
			return fmt.Errorf("expected every span OK, got %d in ERROR (%s)", len(errored), errored[0].StatusMessage)
		}
	} else {
		// Assert the shape the doc comment above records, rather than a span
		// count — a count would break on any future hop added to this chain
		// without saying anything about whether the trace is still correct.
		if len(errored) == 0 {
			return errors.New("expected at least one ERROR span, got none")
		}
		for _, s := range errored {
			if s.StatusMessage == "" {
				return fmt.Errorf("ERROR span %s (%s) carries no statusMessage", s.SpanID, s.Subject)
			}
		}
		// The property this run exists to guard: a parent that fails must
		// still close. An open parent renders as a truncated waterfall
		// rather than as an error, which is worse than showing nothing. A
		// span reaches the store only from finish(), so the root's presence
		// above is what proves it closed; that it closed in ERROR is what
		// proves the failure surfaced to the caller rather than being
		// swallowed at the boundary.
		if root.StatusCode != "ERROR" {
			return fmt.Errorf("expected the root span to close in ERROR, got %q", root.StatusCode)
		}
		// The failure must be attributable *across* the account boundary,
		// which is the whole point of the harness: the rejection originates
		// in refdata, and a trace that only showed the caller's own ERROR
		// would leave the reader unable to see where it came from.
		if !anyErroredFrom(errored, "refdata") {
			return errors.New("expected an ERROR span from refdata, the service that rejected the code")
		}
		// And the round trip itself succeeded — the caller-side hop closes
		// OK. Without this, a transport failure would satisfy every check
		// above while proving nothing about the cross-account path.
		if len(errored) == len(spans) {
			return errors.New("expected the caller-side rpc hop to close OK; every span is in ERROR, which is a transport failure, not a domain rejection")
		}
	}

	// BR-051 attribution is per span, not per trace, and this chain crosses an
	// account boundary: the two organizations hops arrive from the tenant's own
	// account, the refdata hop arrives inside PLATFORM. So "every span is the
	// expected tenant" is the wrong assertion — it would fail on a correctly
	// attributed trace. What must hold is that the tenant is present, that
	// PLATFORM is the only other value, and that nothing is unattributed.
	if h.expectTenant != "" {
		seen := false
		for _, s := range spans {
			switch s.Tenant {
			case h.expectTenant:
				seen = true
			case kvContext:
				// the PLATFORM-side hop; expected on this chain
			case "":
				return fmt.Errorf("span %s (%s) carries no tenant attribution", s.SpanID, s.Subject)
			default:
				return fmt.Errorf("span %s (%s) attributed to %q, want %q or %q", s.SpanID, s.Subject, s.Tenant, h.expectTenant, kvContext)
			}
		}
		if !seen {
			return fmt.Errorf("no span attributed to %q; stored tenants: %v", h.expectTenant, tenantsOf(spans))
		}
	} else if !h.quiet {
		log.Printf("      tenant attribution not asserted (-expect-tenant unset); stored tenants: %v", tenantsOf(spans))
	}

	if !h.quiet {
		log.Printf("      %d spans, root %s %s", len(spans), root.Subject, root.StatusCode)
	}
	return nil
}

// prerequisites resolves the two things the runs need from the live stack —
// a TRANSPORTER to hang a fleet asset off, and a vehicle-type code that
// really is in the context's corpus — and turns either absence into the name
// of the seeder that fixes it, rather than an opaque BR-TP12/BR-TP14 refusal
// halfway through a run.
func (h *harness) prerequisites(ctx context.Context) (partnerID, vehicleType string, err error) {
	var orgs struct {
		Organizations []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"organizations"`
	}
	if err := h.apiRequest("organizations", "organization.list", map[string]any{}, &orgs); err != nil {
		return "", "", fmt.Errorf("listing organizations: %w", err)
	}
	for _, o := range orgs.Organizations {
		// BR-TP12: only a TRANSPORTER may own a fleet asset.
		if strings.EqualFold(o.Type, "TRANSPORTER") {
			partnerID = o.ID
			break
		}
	}
	if partnerID == "" {
		return "", "", fmt.Errorf(
			"no TRANSPORTER organization in context %q — run: (organizations-service) go run ./cmd/seed-transporters -context %s",
			h.contextKey, h.contextKey)
	}

	var corpus struct {
		Items []struct {
			Item struct {
				Code string `json:"code"`
			} `json:"item"`
		} `json:"items"`
	}
	if err := h.apiRequest("refdata", "type.list", map[string]any{
		"typeKey": vehicleTypeKey, "locale": "en", "all": false,
	}, &corpus); err != nil {
		return "", "", fmt.Errorf("listing the vehicle-type corpus: %w", err)
	}
	if len(corpus.Items) == 0 {
		return "", "", fmt.Errorf(
			"the vehicle-type corpus is empty in context %q — run: (refdata-service) go run ./cmd/seed-vehicle-types -context %s",
			h.contextKey, h.contextKey)
	}
	return partnerID, corpus.Items[0].Item.Code, nil
}

// apiRequest sends one api.{context}.{service}.{action}.v1 call. It carries no
// traceparent: the prerequisite calls are setup, and giving them one would put
// their spans in a trace the assertions would then have to filter out.
func (h *harness) apiRequest(service, action string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("api.%s.%s.%s.v1", h.contextKey, service, action)
	reply, err := h.tenant.Request(subject, body, requestTimeout)
	if err != nil {
		return fmt.Errorf("%s: %w", subject, err)
	}
	if msg := reply.Header.Get("Nats-Service-Error"); msg != "" {
		return fmt.Errorf("%s: %s (%s)", subject, msg, reply.Header.Get("Nats-Service-Error-Code"))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(reply.Data, out); err != nil {
		return fmt.Errorf("%s: decoding reply: %w", subject, err)
	}
	return nil
}

// parentLabel renders a span's parent as something readable in a one-line
// summary: the injected traceparent is named rather than shown as an id the
// reader would have to match up by eye against nothing.
func parentLabel(s storedSpan, rootSpanID string) string {
	if s.ParentSpanID == rootSpanID {
		return "(injected)"
	}
	if s.ParentSpanID == "" {
		return "(none)"
	}
	return s.ParentSpanID
}

// anyErroredFrom reports whether any of the failing spans was recorded by
// the named service.
func anyErroredFrom(errored []storedSpan, service string) bool {
	for _, s := range errored {
		if s.Service == service {
			return true
		}
	}
	return false
}

func tenantsOf(spans []storedSpan) []string {
	seen := map[string]bool{}
	for _, s := range spans {
		if s.Tenant == "" {
			seen["(none)"] = true
			continue
		}
		seen[s.Tenant] = true
	}
	return keys(seen)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice, and a harness that silently
		// reused a traceId would assert against the wrong trace.
		log.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
