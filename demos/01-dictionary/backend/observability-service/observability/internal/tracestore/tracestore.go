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
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/nats-io/nats.go/jetstream"
)

// StreamName / StreamMaxAge / StreamMaxBytes mirror shipping-service's
// originals exactly.
const (
	StreamName     = "TRACES"
	StreamMaxAge   = time.Hour
	StreamMaxBytes = 64 << 20 // 64 MiB

	// PlatformSubjectWildcard captures spans published inside PLATFORM
	// itself; TenantSubjectWildcard captures every tenant's imported export,
	// which BR-AC36's LocalSubject remap lands on monitor.{tenant}.trace.>.
	// Both are required and neither is redundant (BR-051): capturing only
	// the remapped form would blind the stream to PLATFORM's own services,
	// which cross no import and so never carry a tenant token, and capturing
	// only the bare form is what this stream did before Phase 48b — which
	// went unnoticed only because the remap did not exist yet.
	//
	// Adding the second filter is a stream UPDATE, not a recreate: a
	// LimitsPolicy stream keeps its contents when a subject is added.
	PlatformSubjectWildcard = "obs.trace.>"
	TenantSubjectWildcard   = "monitor.*.trace.>"

	// bucketName is bare — no "-_platform" suffix — matching this bucket's
	// pre-existing naming (context lives in the KEY via kvContext below).
	bucketName   = "trace-request-reply"
	kvContext    = "_platform"
	consumerName = "trace-store-projector"
	keyPrefix    = "trace."

	// BucketMaxAge / BucketMaxBytes bound the Traces panel's visible window
	// (BR-053). Until Phase 48f this bucket was created as a bare
	// KeyValueConfig{Bucket} — no TTL, no MaxBytes — which made it the only
	// unbounded thing in the whole trace path, since the TRACES stream
	// feeding it is capped at StreamMaxAge/StreamMaxBytes above. The stream
	// forgot and the bucket did not.
	//
	// The governing cost is the same one pubsubstore.go states for
	// pubsub-messages: the panel bootstrap-fetches every entry in the bucket
	// on load, so bucket size is a page-load cost and not merely a disk
	// cost. A trace record is a *merged multi-span* document rather than one
	// envelope, so pubsub's measured numbers do not transfer and these were
	// measured separately.
	//
	// Measured 2026-08-26 by `seed-traces -measure -runs 20` over the
	// running stack — 1,638 stored records, mean 997 B:
	//
	//	1-span   ×1589   p50 611 B    p90 1.5 KiB   max 5.5 KiB
	//	3-span   ×49     p50 3.1 KiB  p90 3.2 KiB   max 3.2 KiB
	//
	// So roughly 1.1 KiB per span at p90, and 8 MiB is on the order of 5,200
	// records — or ~2,500 three-span traces. That is deliberate headroom:
	// measured demo traffic fills about 0.1 MiB per 15 minutes, so MaxAge is
	// the bound that actually bites and MaxBytes is the backstop against a
	// payload spike, which is the property pubsubstore.go argues for rather
	// than a byte cap that silently overrides the time window first.
	//
	// The 15 minutes matches pubsub-messages exactly, and that is a
	// functional choice rather than tidiness: the two panels are read side
	// by side for one incident, so a message visible in the Messages panel
	// must still have its trace visible in the Traces panel. Equal windows
	// are what guarantee that; unequal ones break the cross-reference in
	// whichever direction is shorter.
	BucketMaxAge   = 15 * time.Minute
	BucketMaxBytes = 8 << 20 // 8 MiB
)

// traceRecord is the KV-stored assembly of every span seen so far for one
// traceId — a trace is rarely just one span (an inbound call plus at least
// one outbound hop it causes), so a later span must append to the existing
// entry rather than replace it.
type traceRecord struct {
	TraceID string `json:"traceId"`
	// Tenant is the one piece of information that is NOT in any span: which
	// account published it. It comes from the subject the span arrived on —
	// a token the NATS server inserts via BR-AC36's import remap — and never
	// from the envelope, which is written by the account under observation
	// (BR-051). traceSpan carries a Requester field that looks like the
	// answer and is self-declared; see natstrace.go's own comment on it.
	Tenant string            `json:"tenant"`
	Spans  []json.RawMessage `json:"spans"`
}

// tenantFromSubject reads the tenant token out of the subject the span
// arrived on. "monitor.{tenant}.trace.>" is an imported tenant export
// (BR-AC36's remap); anything else — "obs.trace.>" — was published inside
// PLATFORM itself and has no tenant, so it reports the platform context.
// Mirrors pubsubstore.tenantFromSubject, and is positional for the same
// reason: trace subjects are fixed-arity, which is why CLAUDE.md forbids a
// "." inside any id that appears in a subject token.
func tenantFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 && parts[0] == "monitor" && parts[2] == "trace" {
		return parts[1]
	}
	return kvContext
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
		Subjects:  []string{PlatformSubjectWildcard, TenantSubjectWildcard},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    StreamMaxAge,
		MaxBytes:  StreamMaxBytes,
	}); err != nil {
		return nil, err
	}

	// CreateOrUpdate, not recreate: a KV bucket's TTL and MaxBytes are its
	// backing stream's MaxAge and MaxBytes, both updatable on a live stream,
	// so an existing bucket keeps its name and contents when the bound is
	// applied.
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:   bucketName,
		TTL:      BucketMaxAge,
		MaxBytes: BucketMaxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("create kv bucket %s: %w", bucketName, err)
	}

	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		// No FilterSubject: this projector wants everything on the stream,
		// and naming one wildcard here is what would silently drop the other
		// subject set the moment it was added above. pubsubstore's consumer
		// omits it for the same reason.
		Durable:   consumerName,
		AckPolicy: jetstream.AckExplicitPolicy,
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
		if err := appendSpan(ctx, kv, nc, log, tenantFromSubject(msg.Subject()), key.TraceID, key.SpanID, msg.Data()); err != nil {
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
func appendSpan(ctx context.Context, kv jetstream.KeyValue, nc *nats.Conn, log *slog.Logger, tenant, traceID, spanID string, span json.RawMessage) error {
	key := keyPrefix + traceID
	fullKey := kvContext + "." + key
	record := traceRecord{TraceID: traceID, Tenant: tenant}
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
	// BR-052: first writer wins on the tenant. Two tenants under one traceId
	// is never normal traffic — a traceId is minted once at a request's root
	// and propagated, so a disagreement is either a collision or an attempt
	// to attach spans to another tenant's trace. The span is still stored,
	// because dropping it would hide the evidence, but the established
	// attribution does not move. This is a deliberate override of the
	// zero-value assignment above, whose plain-struct-field default would be
	// last-writer-wins — the wrong answer, arrived at by doing nothing.
	if record.Tenant != "" && record.Tenant != tenant {
		log.Warn("trace span reports a different tenant than the trace it joins; keeping the first attribution",
			"traceId", traceID, "spanId", spanID, "attributed", record.Tenant, "reported", tenant)
		tenant = record.Tenant
	}
	record.Tenant = tenant

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

func kvChanged(key string) natsnotify.Subject {
	return natsnotify.Subject{
		Name: "notify." + kvContext + ".kv." + bucketName + "." + key + ".changed",
		Tokens: natsnotify.Tokens{
			Context: kvContext,
			Service: "kv",
			Entity:  bucketName,
			Action:  "changed",
		},
	}
}

// publishNotify fires notify.{context}.kv.{bucket}.{key}.changed after a
// successful Put.
//
// Constructed WITHOUT natsnotify.WithObservation, and that omission is the
// rule rather than an oversight: this is the service's own internal
// KV-change plumbing for its trace-request-reply bucket, not a domain event,
// and BR-045 names it as excluded. Under the seam's opt-in gate the exclusion
// is a fact about how this Notifier was built, which is why it no longer
// needs an entry in a hand-maintained coverage list.
//
// Unlike shipping-service's kvstore, which builds the same subject and IS
// observed, no traceparent is attached either: that mechanism exists for a
// Put caused by an in-flight request with a span in its context, and
// RegisterTraceStore's consume callback never attaches one.
func publishNotify(nc *nats.Conn, log *slog.Logger, key string, value []byte) {
	natsnotify.New(nc, log).Publish(context.Background(), kvChanged(key), value)
}
