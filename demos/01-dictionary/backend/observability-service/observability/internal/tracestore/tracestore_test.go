package tracestore

// Adapted from shipping-service's trace_store equivalent tests (no direct
// prior art file — this package didn't exist standalone in shipping-service,
// RegisterTraceStore was covered indirectly through eventhandler's suite).
// Written fresh against a real embedded JetStream-enabled NATS server: the
// real risk here is the read-modify-write merge/dedup logic and the notify
// side-effect, not HTTP plumbing.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newTestNATS(t *testing.T) (*nats.Conn, jetstream.JetStream, func()) {
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
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("tracestore-test"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js, func() { nc.Close(); srv.Shutdown() }
}

// waitForRecord polls the KV bucket until traceID's record has at least
// wantSpans spans, or fails the test after a short deadline — the consume
// callback runs asynchronously off the publish that triggers it.
func waitForRecord(t *testing.T, kv jetstream.KeyValue, traceID string, wantSpans int) traceRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entry, err := kv.Get(context.Background(), kvContext+"."+keyPrefix+traceID)
		if err == nil {
			var record traceRecord
			if json.Unmarshal(entry.Value(), &record) == nil && len(record.Spans) >= wantSpans {
				return record
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for trace %s to reach %d spans", traceID, wantSpans)
	return traceRecord{}
}

func publishSpan(t *testing.T, nc *nats.Conn, traceID, spanID string) {
	t.Helper()
	publishSpanOn(t, nc, "obs.trace.acme.shipping.ship.arrived", map[string]string{"traceId": traceID, "spanId": spanID})
}

// publishSpanOn publishes an arbitrary span payload on an arbitrary subject —
// the subject is the whole point of BR-051, so it has to be a parameter, and
// the payload is a map rather than traceSpanKey so a spec can put fields in
// it that the struct deliberately does not model.
func publishSpanOn(t *testing.T, nc *nats.Conn, subject string, payload map[string]string) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal span: %v", err)
	}
	if err := nc.Publish(subject, data); err != nil {
		t.Fatalf("publish span: %v", err)
	}
}

func TestRegisterReturnsNilForNilInputs(t *testing.T) {
	cons, err := Register(context.Background(), nil, nil, discardLogger())
	if cons != nil || err != nil {
		t.Fatalf("expected (nil, nil) for nil js/nc, got (%v, %v)", cons, err)
	}
}

func TestRegisterAssemblesMultipleSpansIntoOneRecord(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer cons.Stop()

	publishSpan(t, nc, "trace-1", "span-a")
	publishSpan(t, nc, "trace-1", "span-b")

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatalf("open kv bucket: %v", err)
	}
	record := waitForRecord(t, kv, "trace-1", 2)
	if record.TraceID != "trace-1" {
		t.Fatalf("expected traceId trace-1, got %q", record.TraceID)
	}
	seen := map[string]bool{}
	for _, raw := range record.Spans {
		var k traceSpanKey
		if err := json.Unmarshal(raw.Span, &k); err != nil {
			t.Fatalf("unmarshal stored span: %v", err)
		}
		seen[k.SpanID] = true
	}
	if !seen["span-a"] || !seen["span-b"] {
		t.Fatalf("expected both span-a and span-b in the merged record, got %+v", seen)
	}
}

func TestRegisterDedupsRedeliveredSpan(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer cons.Stop()

	publishSpan(t, nc, "trace-2", "span-a")
	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatalf("open kv bucket: %v", err)
	}
	waitForRecord(t, kv, "trace-2", 1)

	// Republish the identical (traceId, spanId) — simulates an at-least-once
	// redelivery. Give it time to land, then assert the count never grows.
	publishSpan(t, nc, "trace-2", "span-a")
	time.Sleep(200 * time.Millisecond)

	entry, err := kv.Get(context.Background(), kvContext+"."+keyPrefix+"trace-2")
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	var record traceRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if len(record.Spans) != 1 {
		t.Fatalf("expected exactly 1 span after a duplicate delivery, got %d: %+v", len(record.Spans), record.Spans)
	}
}

func TestRegisterFiresNotifyOnWrite(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	notifyCh := make(chan *nats.Msg, 4)
	sub, err := nc.ChanSubscribe("notify._platform.kv.trace-request-reply.trace.trace-3.changed", notifyCh)
	if err != nil {
		t.Fatalf("subscribe notify: %v", err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer cons.Stop()

	publishSpan(t, nc, "trace-3", "span-a")

	select {
	case msg := <-notifyCh:
		var record traceRecord
		if err := json.Unmarshal(msg.Data, &record); err != nil {
			t.Fatalf("notify payload not the stored record: %v", err)
		}
		if record.TraceID != "trace-3" {
			t.Fatalf("expected traceId trace-3 in notify payload, got %q", record.TraceID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the notify publish")
	}
}

func TestRegisterDropsMalformedSpanWithoutBlockingLaterOnes(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer cons.Stop()

	if err := nc.Publish("obs.trace.acme.shipping.ship.arrived", []byte("not json")); err != nil {
		t.Fatalf("publish malformed span: %v", err)
	}
	publishSpan(t, nc, "trace-4", "span-a")

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatalf("open kv bucket: %v", err)
	}
	waitForRecord(t, kv, "trace-4", 1)
}

func TestRegisterIsIdempotentAcrossTwoCalls(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons1, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	defer cons1.Stop()

	// A second Register (simulating a restart) must not error re-creating
	// the stream/bucket/durable consumer that already exist.
	cons2, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatalf("second Register: %v", err)
	}
	defer cons2.Stop()
}

// TestRegisterCreatesBoundedBucket is BR-053's guard. The bucket was
// unbounded until Phase 48f, and the failure mode of losing the bound again
// is invisible in every other spec in this file: an unbounded bucket stores
// and reads back exactly like a bounded one, so nothing here would go red
// until a disk filled or a panel load got slow enough to notice. Asserting
// the config directly is the only thing that catches it.
func TestRegisterCreatesBoundedBucket(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cc, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatal(err)
	}
	status, err := kv.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got := status.TTL(); got != BucketMaxAge {
		t.Errorf("bucket TTL = %v, want %v", got, BucketMaxAge)
	}
	if got := status.Bytes(); got > uint64(BucketMaxBytes) {
		t.Errorf("bucket already holds %d bytes, over its %d cap", got, BucketMaxBytes)
	}
	// MaxBytes is not on the KeyValueStatus interface, so it comes off the
	// backing stream — which is the same value, and worth reading through
	// deliberately rather than skipping the half of the bound that governs
	// disk.
	si, err := js.Stream(context.Background(), "KV_"+bucketName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := si.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.MaxBytes != int64(BucketMaxBytes) {
		t.Errorf("bucket MaxBytes = %d, want %d", info.Config.MaxBytes, BucketMaxBytes)
	}
	if info.Config.MaxAge != BucketMaxAge {
		t.Errorf("bucket MaxAge = %v, want %v", info.Config.MaxAge, BucketMaxAge)
	}

	// The bucket must stay strictly tighter than the stream feeding it —
	// BR-053's actual requirement, and the thing a future "just bump it"
	// edit would quietly violate.
	if BucketMaxBytes >= StreamMaxBytes {
		t.Errorf("bucket MaxBytes %d must be tighter than the stream's %d", BucketMaxBytes, StreamMaxBytes)
	}
	if BucketMaxAge >= StreamMaxAge {
		t.Errorf("bucket MaxAge %v must be tighter than the stream's %v", BucketMaxAge, StreamMaxAge)
	}
}

// TestTenantFromSubject is BR-051's extraction, unit-level. The positional
// read is only safe because trace subjects are fixed-arity, so the cases that
// matter are the two real shapes plus the near-misses that must NOT be read
// as a tenant.
func TestTenantFromSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		// The remapped form BR-AC36 mints — 7 tokens.
		{"monitor.acme.trace.acme.refdata.item.get", "acme"},
		{"monitor.globex.trace.globex.shipping.ship.arrived", "globex"},
		// PLATFORM's own services cross no import and carry no tenant token.
		{"obs.trace.acme.shipping.ship.arrived", kvContext},
		// Near-misses: right prefix, wrong third token. monitor.*.pubsub.> is
		// a real subject on this server and must never be read as a trace
		// tenant if it somehow reaches this projector.
		{"monitor.acme.pubsub.acme.shipping.ship.arrived", kvContext},
		{"monitor.acme", kvContext},
		{"", kvContext},
	}
	for _, tc := range cases {
		if got := tenantFromSubject(tc.subject); got != tc.want {
			t.Errorf("tenantFromSubject(%q) = %q, want %q", tc.subject, got, tc.want)
		}
	}
}

// TestRegisterCapturesBothSubjectSets is the half of BR-051 that is invisible
// in behaviour until a tenant actually publishes: capturing only the remapped
// form would blind the stream to PLATFORM's own services, and capturing only
// the bare form would blind it to every tenant the moment BR-AC36's remap is
// reseeded. Neither failure shows up in a spec that publishes on one shape.
func TestRegisterCapturesBothSubjectSets(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cons.Stop()

	si, err := js.Stream(context.Background(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := si.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{PlatformSubjectWildcard: false, TenantSubjectWildcard: false}
	for _, subj := range info.Config.Subjects {
		if _, ok := want[subj]; ok {
			want[subj] = true
		}
	}
	for subj, seen := range want {
		if !seen {
			t.Errorf("TRACES does not capture %q; it has %v", subj, info.Config.Subjects)
		}
	}
}

// TestTenantIsDerivedFromSubjectNotEnvelope is the spec BR-051 exists for.
// The payload names a different tenant than the subject does — the subject
// token is inserted by the NATS server and the payload is written by the
// account under observation, so the subject must win. A change that
// "simplified" attribution by reading the envelope would pass every other
// spec in this file.
func TestTenantIsDerivedFromSubjectNotEnvelope(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cons.Stop()

	publishSpanOn(t, nc, "monitor.acme.trace.acme.refdata.item.get", map[string]string{
		"traceId": "trace-5",
		"spanId":  "span-a",
		// A span claiming to be someone else's, the way a hostile or simply
		// buggy publisher would.
		"tenant":    "globex",
		"requester": "globex",
	})

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatal(err)
	}
	record := waitForRecord(t, kv, "trace-5", 1)
	if got := record.Spans[0].Tenant; got != "acme" {
		t.Fatalf("tenant = %q, want acme — attribution must come from the subject, not the envelope", got)
	}
}

// TestPlatformArrivalIsAttributedToPlatform is the other side of BR-051: a
// bare obs.trace.> arrival crossed no import and so has no tenant token.
func TestPlatformArrivalIsAttributedToPlatform(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cons.Stop()

	publishSpan(t, nc, "trace-6", "span-a")

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatal(err)
	}
	record := waitForRecord(t, kv, "trace-6", 1)
	if got := record.Spans[0].Tenant; got != kvContext {
		t.Fatalf("tenant = %q, want %q for a bare obs.trace.> arrival", got, kvContext)
	}
}

// TestCrossAccountTraceKeepsBothAttributions is the spec that retired BR-052.
// That rule called two accounts under one traceId "never normal traffic" — but
// it is the single most ordinary cross-account trace this stack produces:
// organizations-service holds tenant-scoped connections and refdata-service
// runs on platform.creds, so an api.* root, its rpc.* hop and refdata's
// handler land in one trace as two tenant spans and one _platform span. A
// record-level tenant could not represent that, and first-writer-wins would
// have logged a warning on every one of them.
func TestCrossAccountTraceKeepsBothAttributions(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	var logs safeBuffer
	cons, err := Register(context.Background(), js, nc, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer cons.Stop()

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatal(err)
	}

	publishSpanOn(t, nc, "monitor.acme.trace.acme.organizations.vehicle.create",
		map[string]string{"traceId": "trace-7", "spanId": "span-root"})
	waitForRecord(t, kv, "trace-7", 1)
	publishSpanOn(t, nc, "obs.trace.acme.refdata.item.get",
		map[string]string{"traceId": "trace-7", "spanId": "span-handler"})
	record := waitForRecord(t, kv, "trace-7", 2)

	got := map[string]string{}
	for _, stored := range record.Spans {
		var k traceSpanKey
		if err := json.Unmarshal(stored.Span, &k); err != nil {
			t.Fatal(err)
		}
		got[k.SpanID] = stored.Tenant
	}
	if got["span-root"] != "acme" {
		t.Errorf("root span tenant = %q, want acme", got["span-root"])
	}
	if got["span-handler"] != kvContext {
		t.Errorf("handler span tenant = %q, want %q — a PLATFORM span inside a tenant trace keeps its own attribution", got["span-handler"], kvContext)
	}
	// The crossing is normal traffic. A warning here would fire on every
	// cross-account request in the stack, which is what BR-052's guard did.
	if strings.Contains(logs.String(), "WARN") {
		t.Errorf("a cross-account trace must not warn; got: %s", logs.String())
	}
}

// TestLegacyBareSpansStillDecode covers the at-most-BucketMaxAge window in
// which a record written before Phase 48c is still in the bucket. Its spans
// have no wrapper and no attribution to recover, but the projector must
// append to them rather than decode them into empty wrappers and drop them —
// which is precisely what the default struct decoding would do.
func TestLegacyBareSpansStillDecode(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cons.Stop()

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"traceId":"trace-8","spans":[{"traceId":"trace-8","spanId":"span-old"}]}`
	if _, err := kv.Put(context.Background(), kvContext+"."+keyPrefix+"trace-8", []byte(legacy)); err != nil {
		t.Fatal(err)
	}

	publishSpanOn(t, nc, "monitor.acme.trace.acme.refdata.item.get",
		map[string]string{"traceId": "trace-8", "spanId": "span-new"})
	record := waitForRecord(t, kv, "trace-8", 2)

	byID := map[string]string{}
	for _, stored := range record.Spans {
		var k traceSpanKey
		if err := json.Unmarshal(stored.Span, &k); err != nil {
			t.Fatalf("legacy span did not survive the round trip: %v", err)
		}
		byID[k.SpanID] = stored.Tenant
	}
	if _, ok := byID["span-old"]; !ok {
		t.Errorf("the pre-48c span was dropped; got %v", byID)
	}
	if byID["span-old"] != "" {
		t.Errorf("pre-48c span tenant = %q, want empty — there is nothing to recover, so it must not guess", byID["span-old"])
	}
	if byID["span-new"] != "acme" {
		t.Errorf("new span tenant = %q, want acme", byID["span-new"])
	}
}

// safeBuffer is a bytes.Buffer the consume callback's goroutine and the test
// goroutine can both touch. Without the mutex this spec is a data race that
// only shows under -race, which is exactly the kind of flake a diagnostic
// test should not introduce.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
