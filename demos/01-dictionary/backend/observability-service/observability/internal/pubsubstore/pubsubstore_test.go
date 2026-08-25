package pubsubstore

// Specs for Phase 43b (BUSINESS_RULES-SHIPPING.md's BR-047), derived from the
// rule rather than the implementation. Written against a real embedded
// JetStream-enabled NATS server, the same way tracestore's are: the risk here
// is stream/bucket shape, tenant derivation from the arrival subject, and
// dedup — not HTTP plumbing.

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
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("pubsubstore-test"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js, func() { nc.Close(); srv.Shutdown() }
}

// publishEnvelope publishes one obs.pubsub.* envelope the way natstrace's
// ObservePublish does — core NATS, with Nats-Msg-Id set to the spanId.
func publishEnvelope(t *testing.T, nc *nats.Conn, arrivalSubject, spanID, observedSubject string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"direction": "publish",
		"subject":   observedSubject,
		"spanId":    spanID,
		"traceId":   "trace-" + spanID,
		"entity":    "ship",
		"action":    "arrived",
		"timestamp": time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := nats.NewMsg(arrivalSubject)
	msg.Header.Set(nats.MsgIdHdr, spanID)
	msg.Data = data
	if err := nc.PublishMsg(msg); err != nil {
		t.Fatal(err)
	}
}

func waitForEntry(t *testing.T, kv jetstream.KeyValue, spanID string) pubsubRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entry, err := kv.Get(context.Background(), kvContext+"."+keyPrefix+spanID)
		if err == nil {
			var rec pubsubRecord
			if err := json.Unmarshal(entry.Value(), &rec); err != nil {
				t.Fatal(err)
			}
			return rec
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no KV entry for span %s within the deadline", spanID)
	return pubsubRecord{}
}

// BR-047 (ADR-047 A5) — obs.pubsub.> lives on its OWN stream, not as a second
// subject set on TRACES, so an evt.* burst cannot evict RPC traces.
func TestUsesItsOwnStreamNotASecondSubjectOnTraces(t *testing.T) {
	ctx := context.Background()
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cc, err := Register(ctx, js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()

	if StreamName == "TRACES" {
		t.Fatal("obs.pubsub.> must not share the TRACES stream")
	}
	info, err := js.Stream(ctx, StreamName)
	if err != nil {
		t.Fatalf("expected a dedicated %s stream: %v", StreamName, err)
	}
	cfg, err := info.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Both subject sets matter: PLATFORM-local publishers emit on
	// obs.pubsub.>, while every tenant's export arrives remapped onto
	// monitor.{tenant}.pubsub.> (BR-AC34). Capturing only the first would
	// make the panel blind to exactly the cross-tenant traffic Phase 43
	// exists for.
	want := map[string]bool{"obs.pubsub.>": false, "monitor.*.pubsub.>": false}
	for _, s := range cfg.Config.Subjects {
		if _, ok := want[s]; ok {
			want[s] = true
		}
	}
	for subject, found := range want {
		if !found {
			t.Fatalf("stream %s must capture %q, got %v", StreamName, subject, cfg.Config.Subjects)
		}
	}
}

// BR-047 — LimitsPolicy plus MaxAge/MaxBytes sized from the seed run
// recorded in the rule, and an EXPLICIT Duplicates window (A6) rather than
// the 2-minute server default.
func TestStreamProvisionedWithBoundedRetentionAndExplicitDuplicates(t *testing.T) {
	ctx := context.Background()
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cc, err := Register(ctx, js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()

	stream, err := js.Stream(ctx, StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.Retention != jetstream.LimitsPolicy {
		t.Fatalf("retention = %v, want LimitsPolicy", info.Config.Retention)
	}
	if info.Config.MaxAge != StreamMaxAge || info.Config.MaxAge == 0 {
		t.Fatalf("MaxAge = %v, want %v", info.Config.MaxAge, StreamMaxAge)
	}
	if info.Config.MaxBytes != StreamMaxBytes || info.Config.MaxBytes == 0 {
		t.Fatalf("MaxBytes = %v, want %v", info.Config.MaxBytes, StreamMaxBytes)
	}
	if info.Config.Duplicates != StreamDuplicates || info.Config.Duplicates == 0 {
		t.Fatalf("Duplicates = %v, want an explicit %v — relying on the server default is what A6 rules out", info.Config.Duplicates, StreamDuplicates)
	}
}

// BR-047 — the tenant is read from the arrival subject's remap token, which
// the NATS server inserts and a tenant cannot spoof (BR-AC34/BR-048).
func TestTenantIsDerivedFromTheImportRemapSubject(t *testing.T) {
	ctx := context.Background()
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cc, err := Register(ctx, js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()
	kv, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		t.Fatal(err)
	}

	publishEnvelope(t, nc, "monitor.acme.pubsub.acme.shipping.ship.arrived", "span-acme", "evt.acme.shipping.ship.S1.arrived")
	if got := waitForEntry(t, kv, "span-acme").Tenant; got != "acme" {
		t.Fatalf("tenant = %q, want acme (read from the monitor.{tenant}.pubsub.> remap)", got)
	}

	// A PLATFORM-local publisher emits on the un-remapped subject — there is
	// no remap token to read, and it is not a tenant.
	publishEnvelope(t, nc, "obs.pubsub._platform.accounts.account.created", "span-platform", "notify.accounts.account.created")
	if got := waitForEntry(t, kv, "span-platform").Tenant; got != kvContext {
		t.Fatalf("tenant = %q, want %q for a PLATFORM-local publish", got, kvContext)
	}
}

// BR-047 (ADR-047 A6) — a redelivery of the same envelope must not produce a
// duplicate visible entry.
func TestRedeliveryIsDeduplicatedByMessageID(t *testing.T) {
	ctx := context.Background()
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cc, err := Register(ctx, js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()
	kv, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		publishEnvelope(t, nc, "monitor.acme.pubsub.acme.shipping.ship.arrived", "span-dup", "evt.acme.shipping.ship.S1.arrived")
	}
	waitForEntry(t, kv, "span-dup")

	// The stream's Duplicates window collapses the three publishes into one
	// stored message, so the projector only ever sees one.
	stream, err := js.Stream(ctx, StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("stream holds %d messages, want 1 — Nats-Msg-Id dedup did not apply", info.State.Msgs)
	}

	// And the KV write itself is idempotent independently of the stream, so a
	// redelivery after a Nak cannot double-write either.
	keys, err := kv.Keys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("bucket holds %d keys, want 1: %v", len(keys), keys)
	}
}

// BR-047 — a malformed envelope is dropped and acked, never redelivered
// forever, and never blocks the envelopes behind it.
func TestMalformedEnvelopeIsDroppedNotRetriedForever(t *testing.T) {
	ctx := context.Background()
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	cc, err := Register(ctx, js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()
	kv, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		t.Fatal(err)
	}

	msg := nats.NewMsg("monitor.acme.pubsub.acme.shipping.ship.arrived")
	msg.Header.Set(nats.MsgIdHdr, "span-bad")
	msg.Data = []byte("not json")
	if err := nc.PublishMsg(msg); err != nil {
		t.Fatal(err)
	}
	publishEnvelope(t, nc, "monitor.acme.pubsub.acme.shipping.ship.arrived", "span-good", "evt.acme.shipping.ship.S1.arrived")

	waitForEntry(t, kv, "span-good")
	if _, err := kv.Get(ctx, kvContext+"."+keyPrefix+"span-bad"); err == nil {
		t.Fatal("a malformed envelope must not produce a KV entry")
	}
}

// BR-047 — the panel's live-update path. Every successful write fires the
// same notify.{context}.kv.{bucket}.{key}.changed publish the trace store's
// writes already produce, which is what 43c subscribes to.
func TestWriteFiresKVChangeNotify(t *testing.T) {
	ctx := context.Background()
	nc, js, cleanup := newTestNATS(t)
	defer cleanup()

	notifies := make(chan *nats.Msg, 4)
	sub, err := nc.Subscribe("notify._platform.kv."+bucketName+".>", func(m *nats.Msg) { notifies <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	cc, err := Register(ctx, js, nc, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Stop()

	publishEnvelope(t, nc, "monitor.acme.pubsub.acme.shipping.ship.arrived", "span-notify", "evt.acme.shipping.ship.S1.arrived")

	select {
	case m := <-notifies:
		want := "notify._platform.kv." + bucketName + "." + keyPrefix + "span-notify.changed"
		if m.Subject != want {
			t.Fatalf("notify subject = %q, want %q", m.Subject, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no KV change notify fired — 43c's live feed has nothing to subscribe to")
	}
}

// Register is nil-safe, matching tracestore.Register's contract, so a
// process wired without NATS starts rather than panics.
func TestRegisterIsNilSafe(t *testing.T) {
	cc, err := Register(context.Background(), nil, nil, discardLogger())
	if err != nil || cc != nil {
		t.Fatalf("Register(nil, nil) = (%v, %v), want (nil, nil)", cc, err)
	}
}
