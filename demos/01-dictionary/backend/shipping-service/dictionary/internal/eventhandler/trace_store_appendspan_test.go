// White-box tests (package eventhandler, not eventhandler_test) for
// appendSpan's read-modify-write merge contract (BR-036, Phase 28f): a
// trace's KV entry must accumulate every span seen for its traceId rather
// than overwrite-with-latest, and a redelivered span (same traceId+spanId,
// e.g. after a Nak) must not be recorded twice. Mirrors
// container_handler_test.go-style direct testing of the projector's pure
// read-then-write step, without needing to force real JetStream redelivery
// to exercise the dedup path.
package eventhandler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

func newAppendSpanTestStore(t *testing.T) *kvstore.Store {
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
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("append-span-test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return kvstore.New(js, traceStoreBucket)
}

func getTraceRecord(t *testing.T, store *kvstore.Store, traceID string) traceRecord {
	t.Helper()
	value, _, err := store.Get(context.Background(), traceStoreKVContext, traceStoreKeyPrefix+traceID)
	if err != nil {
		t.Fatalf("get trace record: %v", err)
	}
	var record traceRecord
	if err := json.Unmarshal(value, &record); err != nil {
		t.Fatalf("decode trace record: %v", err)
	}
	return record
}

func TestAppendSpanMergesMultipleSpansUnderOneTraceID(t *testing.T) {
	store := newAppendSpanTestStore(t)
	ctx := context.Background()

	span1 := json.RawMessage(`{"traceId":"t1","spanId":"s1","service":"shipping"}`)
	span2 := json.RawMessage(`{"traceId":"t1","spanId":"s2","service":"refdata"}`)

	if err := appendSpan(ctx, store, "t1", "s1", span1); err != nil {
		t.Fatalf("append span1: %v", err)
	}
	if err := appendSpan(ctx, store, "t1", "s2", span2); err != nil {
		t.Fatalf("append span2: %v", err)
	}

	record := getTraceRecord(t, store, "t1")
	if record.TraceID != "t1" {
		t.Fatalf("traceId = %q, want t1", record.TraceID)
	}
	if len(record.Spans) != 2 {
		t.Fatalf("expected 2 merged spans (BR-036's merge-don't-overwrite contract), got %d: %#v", len(record.Spans), record.Spans)
	}
	var seen []string
	for _, raw := range record.Spans {
		var key traceSpanKey
		if err := json.Unmarshal(raw, &key); err != nil {
			t.Fatalf("decode merged span: %v", err)
		}
		seen = append(seen, key.SpanID)
	}
	if !(seen[0] == "s1" && seen[1] == "s2" || seen[0] == "s2" && seen[1] == "s1") {
		t.Fatalf("expected both s1 and s2 present, got %#v", seen)
	}
}

func TestAppendSpanDeduplicatesRedeliveredSpanID(t *testing.T) {
	store := newAppendSpanTestStore(t)
	ctx := context.Background()

	span := json.RawMessage(`{"traceId":"t2","spanId":"s1","service":"shipping"}`)

	if err := appendSpan(ctx, store, "t2", "s1", span); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// Simulate at-least-once redelivery of the identical span (e.g. after a
	// Nak, or any other JetStream at-least-once duplicate).
	if err := appendSpan(ctx, store, "t2", "s1", span); err != nil {
		t.Fatalf("redelivered append: %v", err)
	}

	record := getTraceRecord(t, store, "t2")
	if len(record.Spans) != 1 {
		t.Fatalf("expected redelivery to be a no-op, got %d spans: %#v", len(record.Spans), record.Spans)
	}
}

func TestAppendSpanKeepsDifferentTracesSeparate(t *testing.T) {
	store := newAppendSpanTestStore(t)
	ctx := context.Background()

	spanA := json.RawMessage(`{"traceId":"ta","spanId":"s1"}`)
	spanB := json.RawMessage(`{"traceId":"tb","spanId":"s1"}`)

	if err := appendSpan(ctx, store, "ta", "s1", spanA); err != nil {
		t.Fatalf("append trace ta: %v", err)
	}
	if err := appendSpan(ctx, store, "tb", "s1", spanB); err != nil {
		t.Fatalf("append trace tb: %v", err)
	}

	recordA := getTraceRecord(t, store, "ta")
	if len(recordA.Spans) != 1 {
		t.Fatalf("trace ta must only contain its own span, got %d: %#v", len(recordA.Spans), recordA.Spans)
	}
	recordB := getTraceRecord(t, store, "tb")
	if len(recordB.Spans) != 1 {
		t.Fatalf("trace tb must only contain its own span, got %d: %#v", len(recordB.Spans), recordB.Spans)
	}
}
