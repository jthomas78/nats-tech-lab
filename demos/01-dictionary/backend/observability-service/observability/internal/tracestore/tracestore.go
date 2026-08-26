// Package tracestore projects PLATFORM's obs.trace.> spans (published by
// every service's natstrace package, BR-036/BR-037) into a KV bucket, one
// entry per span — Phase 30g, lifted from shipping-service's
// dictionary/internal/eventhandler/trace_store.go.
//
// Phase 48g replaced the original merge-into-one-entry-per-traceId shape
// (BR-053). That version read the whole record, appended one span and wrote
// it back, so a trace of n spans wrote O(n²) bytes and every append was an
// uncontrolled read-modify-write: two writers on one traceId each read the
// same record and the second Put discarded the first's span. It survived
// only because a single durable consumer happened to be the only writer —
// a property of this deployment, not a guarantee of the design.
//
// The trace is now assembled at READ time, by the reader, from the keys
// sharing its traceId prefix. That is what makes dedup-by-spanId free rather
// than a merge step: the same span overwrites its own key. The reader is the
// Admin UI (frontend/admin/src/nats/useTraceFeed.js), which is why nothing
// in this package reads the bucket back.
//
// The 30g lift itself changed no behaviour; only the surrounding structure
// differed, since this
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

	// StreamDuplicates is set EXPLICITLY, the way ADR-047 A6 made PUBSUB do
	// it, rather than inheriting the server's 2-minute default — the window
	// IS the dedup contract, so it belongs somewhere it can be read, not in
	// a default a server upgrade could move underneath us.
	//
	// The other half was missing until Phase 48g and is worth naming, since
	// a Duplicates window with no message id is inert: natstrace's span
	// publish now sets Nats-Msg-Id to the span id, exactly as its
	// obs.pubsub.* sibling already did. Note this de-duplicates at the
	// STREAM, before the projector ever sees the span; storeSpan's
	// same-key-same-content Put is the second, unbounded line of defence
	// for a redelivery arriving after the window has closed.
	StreamDuplicates = 2 * time.Minute

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

	// keyPrefix opens a key of the form trace.{traceId}.{spanId} — one KV
	// entry per span, not per trace (BR-053). Both ids are subject-safe by
	// CLAUDE.md's identity rule (no "."), so the third token is a genuine
	// span id and a prefix scan on trace.{traceId}. is exactly that trace.
	keyPrefix = "trace."

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
	// cost.
	//
	// Measured 2026-08-26 by `seed-traces -measure -runs 20` over the running
	// stack, when a record was still a merged multi-span document — 1,638
	// records, mean 997 B:
	//
	//	1-span   ×1589   p50 611 B    p90 1.5 KiB   max 5.5 KiB
	//	3-span   ×49     p50 3.1 KiB  p90 3.2 KiB   max 3.2 KiB
	//
	// Phase 48g split those records one-per-span, and the bound was left
	// where the measurement put it. The per-span figure is what carries over
	// and it barely moves: ~1.1 KiB at p90 either way, since a merged record
	// was almost exactly the sum of its spans plus a few bytes of envelope.
	// What DID change is in the bound's favour — the O(n²) rewrite is gone,
	// so a multi-span trace now consumes its own size rather than the sum of
	// every intermediate version of itself. 8 MiB is on the order of 7,000
	// spans. That is deliberate headroom:
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

// storedSpan is one span plus the account it arrived from — the whole value
// under one trace.{traceId}.{spanId} key.
//
// The span is wrapped rather than stored bare so it carries its attribution
// (BR-051). The tenant is deliberately NOT merged into the span object: the
// span is a document the observed account wrote, the tenant is a token the
// NATS server inserted, and a reader has to be able to tell those apart at a
// glance. Same shape, and the same reason, as pubsubstore's
// pubsubRecord{Tenant, Span}.
//
// Per SPAN, not per trace, and that is the correction Phase 48c forced — now
// structural rather than conventional, since a span IS the record. A
// record-level tenant looked right until the panel needed to render it:
// organizations-service holds tenant-scoped connections while refdata-service
// runs on platform.creds, so the most ordinary cross-account trace in this
// stack — an api.* root, its rpc.* hop, and refdata's handler — contains two
// acme spans and one _platform span. One tenant per trace cannot express
// that, and forcing it would have the panel label refdata's PLATFORM span
// with a tenant's name, which is a false statement on the one surface whose
// whole value is trustworthy provenance.
type storedSpan struct {
	Tenant string          `json:"tenant"`
	Span   json.RawMessage `json:"span"`
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
// projects every obs.trace.* span into it under its own
// trace.{traceId}.{spanId} key. js and nc must be the same single PLATFORM connection this
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
		Name:       StreamName,
		Subjects:   []string{PlatformSubjectWildcard, TenantSubjectWildcard},
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     StreamMaxAge,
		MaxBytes:   StreamMaxBytes,
		Duplicates: StreamDuplicates,
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
		if err := storeSpan(ctx, kv, nc, log, tenantFromSubject(msg.Subject()), key.TraceID, key.SpanID, msg.Data()); err != nil {
			log.Error("trace store projection failed, will redeliver", "traceId", key.TraceID, "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
}

// storeSpan writes one span under its own trace.{traceId}.{spanId} key and
// fires the same best-effort notify.{context}.kv.{bucket}.{key}.changed
// publish every other KV panel's writes already produce.
//
// A plain Put, and idempotent by construction rather than by checking:
// re-delivering a span overwrites its own key with identical content, so a
// duplicate cannot produce a second visible entry even after the stream's
// Duplicates window has closed. That is the property the old read-modify-
// write bought with a Get, a scan of every span already stored, and a
// rewrite of the whole record (BR-053) — and bought incompletely, since two
// writers racing on one traceId lost a span outright.
//
// There is deliberately no read here at all. Assembling the trace is the
// reader's job, from the keys sharing its trace.{traceId}. prefix; keeping a
// read on the write path is what would reintroduce the race.
func storeSpan(ctx context.Context, kv jetstream.KeyValue, nc *nats.Conn, log *slog.Logger, tenant, traceID, spanID string, span json.RawMessage) error {
	key := keyPrefix + traceID + "." + spanID
	data, err := json.Marshal(storedSpan{Tenant: tenant, Span: span})
	if err != nil {
		return err
	}
	if _, err := kv.Put(ctx, kvContext+"."+key, data); err != nil {
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
