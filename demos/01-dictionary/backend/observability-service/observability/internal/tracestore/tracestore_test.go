package tracestore

// Adapted from shipping-service's trace_store equivalent tests (no direct
// prior art file — this package didn't exist standalone in shipping-service,
// RegisterTraceStore was covered indirectly through eventhandler's suite).
// Written fresh against a real embedded JetStream-enabled NATS server: the
// real risk here is the write shape and the notify side-effect, not HTTP
// plumbing.
//
// Phase 48g moved trace ASSEMBLY out of this package — one KV entry is now
// one span, and the reader joins them (BR-053). So traceRecord and readTrace
// below are test-local on purpose: they are this suite standing in for the
// Admin UI, not a shape the projector still knows about. The equivalent
// assembly in JavaScript, including its tolerance for records written before
// this phase, is covered by frontend/admin/src/nats/useTraceFeed.spec.js.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// traceRecord is the assembled trace — what the reader builds, and what this
// suite asserts against. Test-local by design: see the file comment.
type traceRecord struct {
	TraceID string
	Spans   []storedSpan
}

// readTrace assembles one trace out of the bucket the way useTraceFeed.js
// does: every key sharing the trace.{traceId}. prefix holds exactly one span,
// so the join is a prefix scan and dedup-by-spanId is free.
func readTrace(t *testing.T, kv jetstream.KeyValue, traceID string) traceRecord {
	t.Helper()
	record := traceRecord{TraceID: traceID}
	prefix := kvContext + "." + keyPrefix + traceID + "."
	keys, err := kv.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	for key := range keys.Keys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, err := kv.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		var stored storedSpan
		if err := json.Unmarshal(entry.Value(), &stored); err != nil {
			t.Fatalf("unmarshal %s: %v", key, err)
		}
		record.Spans = append(record.Spans, stored)
	}
	return record
}

// waitForSpans polls until traceID has at least wantSpans spans stored, or
// fails after a short deadline — the consume callback runs asynchronously off
// the publish that triggers it.
func waitForSpans(t *testing.T, kv jetstream.KeyValue, traceID string, wantSpans int) traceRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if record := readTrace(t, kv, traceID); len(record.Spans) >= wantSpans {
			return record
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for trace %s to reach %d spans", traceID, wantSpans)
	return traceRecord{}
}

// spanIDsByTenant reads back the (spanId -> tenant) pairs of an assembled
// trace — the two facts nearly every spec below asserts on.
func spanIDsByTenant(t *testing.T, record traceRecord) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, stored := range record.Spans {
		var k traceSpanKey
		if err := json.Unmarshal(stored.Span, &k); err != nil {
			t.Fatalf("unmarshal stored span: %v", err)
		}
		got[k.SpanID] = stored.Tenant
	}
	return got
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

// TestRegisterAssemblesMultipleSpansIntoOneTrace is the join, end to end: two
// spans of one trace go in on separate publishes and come back out of a
// prefix scan as one trace. Since 48g the assembly is the READER's, so what
// this really pins is that both spans landed under keys the scan finds.
func TestRegisterAssemblesMultipleSpansIntoOneTrace(t *testing.T) {
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
	seen := spanIDsByTenant(t, waitForSpans(t, kv, "trace-1", 2))
	if _, ok := seen["span-a"]; !ok {
		t.Fatalf("expected span-a in the assembled trace, got %+v", seen)
	}
	if _, ok := seen["span-b"]; !ok {
		t.Fatalf("expected span-b in the assembled trace, got %+v", seen)
	}
}

// TestSpanIsStoredUnderItsOwnKey is BR-053's write shape stated directly. Every
// other spec here reads through readTrace, which assembles by prefix and so
// passes just as happily against the old one-key-per-trace record — this is
// the one that does not.
func TestSpanIsStoredUnderItsOwnKey(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cons, err := Register(context.Background(), js, nc, discardLogger())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer cons.Stop()

	publishSpan(t, nc, "trace-keys", "span-a")
	publishSpan(t, nc, "trace-keys", "span-b")

	kv, err := js.KeyValue(context.Background(), bucketName)
	if err != nil {
		t.Fatalf("open kv bucket: %v", err)
	}
	waitForSpans(t, kv, "trace-keys", 2)

	for _, spanID := range []string{"span-a", "span-b"} {
		key := kvContext + "." + keyPrefix + "trace-keys." + spanID
		entry, err := kv.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("expected a KV entry at %s: %v", key, err)
		}
		var stored storedSpan
		if err := json.Unmarshal(entry.Value(), &stored); err != nil {
			t.Fatalf("value at %s is not a storedSpan: %v", key, err)
		}
		var k traceSpanKey
		if err := json.Unmarshal(stored.Span, &k); err != nil {
			t.Fatal(err)
		}
		if k.SpanID != spanID {
			t.Errorf("key %s holds span %q — a key must hold its own span, and only it", key, k.SpanID)
		}
	}
	// The pre-48g merged key must not be written alongside the per-span ones:
	// a projector that wrote both would pass every assertion above and double
	// the bucket.
	if _, err := kv.Get(context.Background(), kvContext+"."+keyPrefix+"trace-keys"); err == nil {
		t.Error("the pre-48g merged trace key is still being written")
	}
}

// TestRegisterDedupsRedeliveredSpan still holds after 48g, but for a
// different reason worth keeping straight: the projector no longer SCANS for
// the span id before writing — the redelivered span simply overwrites its own
// key with identical content. Dedup became a property of the key rather than
// a step in the write.
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
	waitForSpans(t, kv, "trace-2", 1)

	// Republish the identical (traceId, spanId) — simulates an at-least-once
	// redelivery. Give it time to land, then assert the count never grows.
	publishSpan(t, nc, "trace-2", "span-a")
	time.Sleep(200 * time.Millisecond)

	record := readTrace(t, kv, "trace-2")
	if len(record.Spans) != 1 {
		t.Fatalf("expected exactly 1 span after a duplicate delivery, got %d: %+v", len(record.Spans), record.Spans)
	}
}

func TestRegisterFiresNotifyOnWrite(t *testing.T) {
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	notifyCh := make(chan *nats.Msg, 4)
	// The key — and so the notify subject — carries the span id since 48g:
	// a subscriber that wants a whole trace watches the prefix, which is what
	// useTraceFeed.js's "...trace-request-reply.>" subscription already did.
	sub, err := nc.ChanSubscribe("notify._platform.kv.trace-request-reply.trace.trace-3.span-a.changed", notifyCh)
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
		var stored storedSpan
		if err := json.Unmarshal(msg.Data, &stored); err != nil {
			t.Fatalf("notify payload not the stored span: %v", err)
		}
		var k traceSpanKey
		if err := json.Unmarshal(stored.Span, &k); err != nil {
			t.Fatalf("notify payload carries no span: %v", err)
		}
		if k.TraceID != "trace-3" || k.SpanID != "span-a" {
			t.Fatalf("expected trace-3/span-a in the notify payload, got %s/%s", k.TraceID, k.SpanID)
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
	waitForSpans(t, kv, "trace-4", 1)
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
	record := waitForSpans(t, kv, "trace-5", 1)
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
	record := waitForSpans(t, kv, "trace-6", 1)
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
	waitForSpans(t, kv, "trace-7", 1)
	publishSpanOn(t, nc, "obs.trace.acme.refdata.item.get",
		map[string]string{"traceId": "trace-7", "spanId": "span-handler"})
	got := spanIDsByTenant(t, waitForSpans(t, kv, "trace-7", 2))
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

// TestStreamSetsDuplicatesExplicitly is the second half of decision 15's
// dedup contract, at the stream rather than the key. Like the bucket bound
// above it is invisible in behaviour — a stream inheriting the server's
// 2-minute default de-duplicates exactly the same as one that names it, right
// up until a server upgrade moves the default — so the config is the only
// thing that can be asserted.
func TestStreamSetsDuplicatesExplicitly(t *testing.T) {
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
	if info.Config.Duplicates != StreamDuplicates {
		t.Errorf("TRACES Duplicates = %v, want %v", info.Config.Duplicates, StreamDuplicates)
	}
	// A window with no message id on the wire is inert, so the two halves are
	// asserted together — natstrace's own suite covers the publish side, this
	// records that the window exists for a reason.
	if StreamDuplicates > StreamMaxAge {
		t.Errorf("Duplicates %v outlives the stream's own MaxAge %v", StreamDuplicates, StreamMaxAge)
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

// TestConcurrentSpanWritesLoseNothing is BR-053's write-shape half, and the
// spec that forced Phase 48g's rewrite. Until then the projector read the
// whole trace record, appended one span and wrote it back — a read-modify-
// write with no CAS, so two writers racing on one traceId each read the same
// record and the second Put silently discarded the first's span. That it
// never bit in production was a property of the deployment (one durable
// consumer, one goroutine), not of the design: it breaks the moment the
// projector is scaled out or a redelivery overlaps a live span.
//
// Written against the store function directly rather than through Register,
// because the race is in the write and driving it through a single-threaded
// consume callback is exactly what hides it.
func TestConcurrentSpanWritesLoseNothing(t *testing.T) {
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

	const traceID = "trace-concurrent"
	const spanCount = 16
	var wg sync.WaitGroup
	for i := 0; i < spanCount; i++ {
		spanID := fmt.Sprintf("span-%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload, err := json.Marshal(map[string]string{"traceId": traceID, "spanId": spanID})
			if err != nil {
				t.Error(err)
				return
			}
			if err := storeSpan(context.Background(), kv, nc, discardLogger(), kvContext, traceID, spanID, payload); err != nil {
				t.Errorf("storeSpan(%s): %v", spanID, err)
			}
		}()
	}
	wg.Wait()

	record := readTrace(t, kv, traceID)
	if len(record.Spans) != spanCount {
		t.Fatalf("%d concurrent writers stored %d spans, want %d — an overlapping write lost one", spanCount, len(record.Spans), spanCount)
	}
}
