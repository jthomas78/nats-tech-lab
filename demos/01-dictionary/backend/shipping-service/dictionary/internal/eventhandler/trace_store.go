package eventhandler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// TracesStreamName / TracesSubjectWildcard / traceStoreBucket back Phase
// 28f/28g's cross-account trace store: every service's obs.trace.* span
// (BR-036/BR-037) is published on its own account and crosses into PLATFORM
// via the account import/export wiring accounts-service's
// Provisioner.addPlatformTraceImport (or bootstrap-operator.sh's day-0
// equivalent) establishes per tenant. TRACES is where all of them land on
// the PLATFORM side, and RegisterTraceStore's durable consumer assembles
// same-traceId spans into one KV entry so the Admin UI's trace waterfall
// panel (Phase 28g) can fetch a whole cross-hop trace in one read instead of
// replaying the stream and reassembling it client-side.
const (
	TracesStreamName      = "TRACES"
	TracesSubjectWildcard = "obs.trace.>"
	// TracesStreamMaxAge mirrors RPCTRACE's short-replay-window rationale
	// (refdata-service's composition.go) for the pre-28f side channel this
	// supersedes — long enough for the durable consumer to catch up after a
	// restart, short enough not to grow unbounded on an idle demo stack.
	TracesStreamMaxAge = time.Hour
	// TracesStreamMaxBytes caps disk use independent of MaxAge — a burst of
	// span traffic within the hour must not grow the stream unbounded.
	TracesStreamMaxBytes = 64 << 20 // 64 MiB

	// traceStoreBucket is bare — no "-_platform" suffix — matching
	// internal/kvstore's own naming convention (its package doc: "one bucket
	// per application role..., named by the prefix alone"; context lives in
	// the KEY via traceStoreKVContext below, never the bucket name).
	traceStoreBucket       = "trace-request-reply"
	traceStoreKVContext    = "_platform"
	traceStoreConsumerName = "trace-store-projector"
	traceStoreKeyPrefix    = "trace."
)

// traceRecord is the KV-stored assembly of every span seen so far for one
// traceId — BR-036's "merge, don't overwrite" contract: a trace is rarely
// just one span (an inbound call plus at least one outbound hop it causes),
// so a later span must append to the existing entry rather than replace it.
type traceRecord struct {
	TraceID string            `json:"traceId"`
	Spans   []json.RawMessage `json:"spans"`
}

// traceSpanKey is the minimal shape read out of an obs.trace.* span payload —
// just enough to key and de-duplicate KV writes. The full span (every other
// field natstrace.go's own traceSpan type carries) is preserved verbatim as
// raw JSON inside traceRecord.Spans, so this consumer never needs its own
// copy of that struct's full shape.
type traceSpanKey struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

// RegisterTraceStore provisions the TRACES stream and trace-request-reply KV bucket
// (idempotent — CreateOrUpdate*) and starts the durable consumer that
// projects every obs.trace.* span into it, merging same-traceId spans under
// one key — mirrors container_handler.go's read-then-write structural
// pattern, but merges an array instead of applying a domain aggregate.
//
// The trace-request-reply bucket is wrapped in internal/kvstore.Store (not raw
// jetstream.KeyValue) specifically so its EnableNotify gets reused
// unchanged: every appendSpan write already gets a
// notify._platform.kv.trace-request-reply.trace.{traceId}.changed publish for free, the
// same mechanism (and the same Admin UI plumbing, kv.go's
// kvBucketEntriesOnce/listKVBuckets) every other KV panel already uses —
// Phase 28g needs no new REST endpoint and no new watch-bridge goroutine to
// give the trace waterfall panel a bootstrap-then-live feed.
//
// platformFullJS must be the unrestricted PLATFORM connection
// (monolith.Monolith.PlatformFullJS(), never the restricted shipping-admin
// mono.JS()) since provisioning stream/KV resources is a $JS.API.> write —
// see PlatformFullJS's doc comment. platformNC only needs shipping-admin's
// restricted grant (mono.NC()): its notify._platform.> publish permission
// (nats/bootstrap-operator.sh) already covers the new notify subject above,
// no bootstrap-operator.sh change required. Nil-safe on either connection —
// a no-op, matching RegisterRefdataNotify/RegisterRPCTraceNotify's
// convention for the same two connections.
func RegisterTraceStore(ctx context.Context, platformFullJS jetstream.JetStream, platformNC *nats.Conn, log *slog.Logger) (jetstream.ConsumeContext, error) {
	if platformFullJS == nil || platformNC == nil {
		return nil, nil
	}

	if _, err := platformFullJS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      TracesStreamName,
		Subjects:  []string{TracesSubjectWildcard},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    TracesStreamMaxAge,
		MaxBytes:  TracesStreamMaxBytes,
	}); err != nil {
		return nil, err
	}

	store := kvstore.New(platformFullJS, traceStoreBucket)
	store.EnableNotify(platformNC, log)

	cons, err := platformFullJS.CreateOrUpdateConsumer(ctx, TracesStreamName, jetstream.ConsumerConfig{
		Durable:       traceStoreConsumerName,
		FilterSubject: TracesSubjectWildcard,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, err
	}

	return cons.Consume(func(msg jetstream.Msg) {
		var key traceSpanKey
		if err := json.Unmarshal(msg.Data(), &key); err != nil || key.TraceID == "" || key.SpanID == "" {
			log.Error("drop malformed trace span", "subject", msg.Subject(), "err", err)
			_ = msg.Ack()
			return
		}
		if err := appendSpan(ctx, store, key.TraceID, key.SpanID, msg.Data()); err != nil {
			log.Error("trace store projection failed, will redeliver", "traceId", key.TraceID, "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
}

// appendSpan reads the current trace record (if any), skips the write
// entirely if spanID is already present (redelivery after a Nak, or any
// other at-least-once duplicate), and otherwise appends span and writes
// back — the same read-modify-write shape currentContainerAgg/Upsert uses
// in container_handler.go, just merging a list instead of applying a
// domain event.
func appendSpan(ctx context.Context, store *kvstore.Store, traceID, spanID string, span json.RawMessage) error {
	key := traceStoreKeyPrefix + traceID
	record := traceRecord{TraceID: traceID}
	value, _, err := store.Get(ctx, traceStoreKVContext, key)
	switch {
	case err == nil:
		if unmarshalErr := json.Unmarshal(value, &record); unmarshalErr != nil {
			return unmarshalErr
		}
	case errors.Is(err, jetstream.ErrKeyNotFound):
		// first span seen for this trace — record stays zero-value above.
	default:
		return err
	}
	for _, existing := range record.Spans {
		var existingKey traceSpanKey
		if json.Unmarshal(existing, &existingKey) == nil && existingKey.SpanID == spanID {
			return nil
		}
	}
	record.Spans = append(record.Spans, span)
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = store.Put(ctx, traceStoreKVContext, key, data)
	return err
}
