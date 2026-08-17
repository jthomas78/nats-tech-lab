// Package tracestore projects PLATFORM's obs.trace.> spans (published by
// every service's natstrace package, BR-036/BR-037) into a KV bucket keyed
// by traceId, merging same-trace spans into one entry — Phase 30g, lifted
// from shipping-service's dictionary/internal/eventhandler/trace_store.go.
// Behavior is unchanged; only the surrounding structure differs, since this
// service has no domain layer and no reason to port shipping-service's
// general-purpose internal/kvstore.Store abstraction (its Keys/Watch/
// Delete/multi-context machinery, and its natstrace-header propagation on
// notify, are unused by this one projector — RegisterTraceStore never
// attaches a span to the consume callback's context, so that branch was
// always a no-op here anyway). This file inlines just the Get/Put-with-
// notify slice that's actually exercised.
package tracestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// StreamName / SubjectWildcard / StreamMaxAge / StreamMaxBytes mirror
// shipping-service's originals exactly.
const (
	StreamName      = "TRACES"
	SubjectWildcard = "obs.trace.>"
	StreamMaxAge    = time.Hour
	StreamMaxBytes  = 64 << 20 // 64 MiB

	// bucketName is bare — no "-_platform" suffix — matching this bucket's
	// pre-existing naming (context lives in the KEY via kvContext below).
	bucketName   = "trace-request-reply"
	kvContext    = "_platform"
	consumerName = "trace-store-projector"
	keyPrefix    = "trace."
)

// traceRecord is the KV-stored assembly of every span seen so far for one
// traceId — a trace is rarely just one span (an inbound call plus at least
// one outbound hop it causes), so a later span must append to the existing
// entry rather than replace it.
type traceRecord struct {
	TraceID string            `json:"traceId"`
	Spans   []json.RawMessage `json:"spans"`
}

// traceSpanKey is the minimal shape read out of an obs.trace.* span
// payload — just enough to key and de-duplicate KV writes.
type traceSpanKey struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

// Register provisions the TRACES stream and trace-request-reply KV bucket
// (idempotent — CreateOrUpdate*) and starts the durable consumer that
// projects every obs.trace.* span into it, merging same-traceId spans under
// one key. js and nc must be the same single PLATFORM connection this
// service holds everywhere else (unlike shipping-service's original, which
// needed its separate, broader PlatformFullJS specifically because
// provisioning is a $JS.API write its narrowly-scoped shipping-admin
// connection was denied — here that provisioning access is granted
// directly to the one connection, scoped to exactly the two resource names
// this service owns: TRACES and KV_trace-request-reply, never a wildcard).
// Nil-safe: returns (nil, nil) if either is nil.
func Register(ctx context.Context, js jetstream.JetStream, nc *nats.Conn, log *slog.Logger) (jetstream.ConsumeContext, error) {
	if js == nil || nc == nil {
		return nil, nil
	}

	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{SubjectWildcard},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    StreamMaxAge,
		MaxBytes:  StreamMaxBytes,
	}); err != nil {
		return nil, err
	}

	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName})
	if err != nil {
		return nil, fmt.Errorf("create kv bucket %s: %w", bucketName, err)
	}

	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       consumerName,
		FilterSubject: SubjectWildcard,
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
		if err := appendSpan(ctx, kv, nc, log, key.TraceID, key.SpanID, msg.Data()); err != nil {
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
// back, then fires the same best-effort notify.{context}.kv.{bucket}.
// {key}.changed publish every other KV panel's writes already produce.
func appendSpan(ctx context.Context, kv jetstream.KeyValue, nc *nats.Conn, log *slog.Logger, traceID, spanID string, span json.RawMessage) error {
	key := keyPrefix + traceID
	fullKey := kvContext + "." + key
	record := traceRecord{TraceID: traceID}
	entry, err := kv.Get(ctx, fullKey)
	switch {
	case err == nil:
		if unmarshalErr := json.Unmarshal(entry.Value(), &record); unmarshalErr != nil {
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
	if _, err := kv.Put(ctx, fullKey, data); err != nil {
		return err
	}
	publishNotify(nc, log, key, data)
	return nil
}

// publishNotify fires notify.{context}.kv.{bucket}.{key}.changed after a
// successful Put — best-effort, a publish error is logged, never returned.
// Unlike shipping-service's internal/kvstore.Store.publishNotify, this
// never attaches a traceparent header: that mechanism exists for a Put
// caused by an in-flight request with a span already in its context, and
// RegisterTraceStore's own consume callback never attaches one — the
// omission changes nothing observable.
func publishNotify(nc *nats.Conn, log *slog.Logger, key string, value []byte) {
	subject := "notify." + kvContext + ".kv." + bucketName + "." + key + ".changed"
	if err := nc.Publish(subject, value); err != nil && log != nil {
		log.Warn("kv notify publish failed", "subject", subject, "err", err)
	}
}
