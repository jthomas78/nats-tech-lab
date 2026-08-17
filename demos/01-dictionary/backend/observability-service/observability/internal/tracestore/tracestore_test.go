package tracestore

// Adapted from shipping-service's trace_store equivalent tests (no direct
// prior art file — this package didn't exist standalone in shipping-service,
// RegisterTraceStore was covered indirectly through eventhandler's suite).
// Written fresh against a real embedded JetStream-enabled NATS server: the
// real risk here is the read-modify-write merge/dedup logic and the notify
// side-effect, not HTTP plumbing.

import (
	"context"
	"encoding/json"
	"log/slog"
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
	data, err := json.Marshal(traceSpanKey{TraceID: traceID, SpanID: spanID})
	if err != nil {
		t.Fatalf("marshal span: %v", err)
	}
	if err := nc.Publish("obs.trace.acme.shipping.ship.arrived", data); err != nil {
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
		if err := json.Unmarshal(raw, &k); err != nil {
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
