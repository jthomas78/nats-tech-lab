// Package pubsubstore projects PLATFORM's obs.pubsub.* envelopes — the
// publish-side observations natstrace emits for every evt.*/notify.* publish
// (BR-045) — into a KV bucket keyed by spanId, so the Admin UI's Messages
// panel can read them (Phase 43b, BR-047).
//
// A deliberate sibling to tracestore, not an extension of it. The two differ
// in three ways worth stating up front, because each one is a decision the
// design review made rather than a detail of this file:
//
//   - Its own stream (ADR-047 A5). obs.pubsub.* is higher-volume than
//     obs.trace.*, and sharing TRACES would let an evt.* burst evict RPC
//     traces — the more expensive signal to lose.
//   - Two subject sets, not one. PLATFORM's own services publish on
//     obs.pubsub.> directly, but every TENANT's export arrives remapped onto
//     monitor.{tenant}.pubsub.> (BR-AC34). tracestore needs no equivalent
//     because the obs.trace.> import carries no remap — which is exactly why
//     the Traces panel can only show a coarse PLATFORM/TENANT split, and why
//     this one can name the tenant.
//   - No merge. A trace is several spans that must be assembled under one
//     traceId; an obs.pubsub.* envelope is standalone, so a write is a plain
//     Put rather than tracestore's read-modify-write append.
//
// A KV bucket rather than a stream-only design (BR-047 left this open): the
// Admin UI reads observability data by bootstrap-fetching a KV bucket and
// live-subscribing to notify._platform.kv.{bucket}.> (see useTraceFeed.js),
// and a browser credential is never granted obs.pubsub.> (BR-AC34), so the
// bucket is the read path 43c needs. Nothing is merged in it — it is a
// bounded visible window over the stream, which is why its caps are tighter
// than the stream's.
package pubsubstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamName = "PUBSUB"

	// PlatformSubjectWildcard captures publishes made inside PLATFORM itself;
	// TenantSubjectWildcard captures every tenant's imported export, which
	// BR-AC34's LocalSubject remap lands on monitor.{tenant}.pubsub.>.
	PlatformSubjectWildcard = "obs.pubsub.>"
	TenantSubjectWildcard   = "monitor.*.pubsub.>"

	// StreamMaxAge / StreamMaxBytes are measured, not inherited from TRACES
	// (BR-047). A seed run over representative shipping payloads put a real
	// envelope at 454 B (evt ship.arrived), 550 B (evt container.loaded) and
	// 592 B (a notify.* carrying a whole projected KV value) — roughly a
	// quarter of the ~2 KiB ADR-047 assumed. 32 MiB is therefore on the order
	// of 55k envelopes, and the hour of MaxAge is a bound this stream can
	// actually honour at realistic demo volume rather than a number the byte
	// cap silently overrides first.
	StreamMaxAge   = time.Hour
	StreamMaxBytes = 32 << 20 // 32 MiB

	// StreamDuplicates is set EXPLICITLY (ADR-047 A6) rather than inheriting
	// the server's 2-minute default — the window is the whole dedup contract,
	// so it belongs in the config where it can be read, not in a default that
	// a server upgrade could move underneath us. Paired with BR-045's emit
	// setting Nats-Msg-Id to the envelope's spanId; without both halves,
	// dedup is unenforceable.
	StreamDuplicates = 2 * time.Minute

	// bucketName is lowercase-kebab per CLAUDE.md's storage-naming rule, and
	// bare — context lives in the KEY via kvContext, matching the
	// trace-request-reply bucket beside it.
	bucketName   = "pubsub-messages"
	kvContext    = "_platform"
	consumerName = "pubsub-store-projector"
	keyPrefix    = "msg."

	// BucketMaxAge / BucketMaxBytes bound the panel's visible window, and are
	// deliberately tighter than the stream's: 43c bootstrap-fetches every
	// entry in this bucket on load (the same way the Traces panel does), so
	// the bucket size is a page-load cost, not just disk. 15 minutes at the
	// measured envelope size is on the order of a few thousand entries.
	BucketMaxAge   = 15 * time.Minute
	BucketMaxBytes = 8 << 20 // 8 MiB
)

// pubsubRecord is what the panel reads: the envelope exactly as published,
// plus the one piece of information that is NOT in it — which tenant
// published it. That comes from the subject the message arrived on, a token
// the NATS server inserts via BR-AC34's import remap, so a tenant cannot
// spoof its own provenance by writing a field into the payload.
type pubsubRecord struct {
	Tenant string          `json:"tenant"`
	Span   json.RawMessage `json:"span"`
}

// envelopeKey is the minimal shape read out of an obs.pubsub.* envelope —
// just enough to key the KV write.
type envelopeKey struct {
	SpanID string `json:"spanId"`
}

// Register provisions the PUBSUB stream and pubsub-messages KV bucket
// (idempotent — CreateOrUpdate*) and starts the durable consumer that
// projects every obs.pubsub.* envelope into it. js and nc must be the same
// single PLATFORM connection this service holds everywhere else, and the
// creds behind it need the same resource-scoped provisioning grants
// tracestore's two resources already carry (see nats/bootstrap-operator.sh's
// observability user). Nil-safe: returns (nil, nil) if either is nil.
//
// Ingestion is best-effort by construction and the UI must not imply
// otherwise (BR-047, ADR-047 A7): BR-045's emit is a core-NATS
// fire-and-forget publish, so an envelope can be lost before it ever reaches
// this stream — under a slow consumer, a reconnect, or simply a publish into
// a stream that is at its byte cap. Everything from the stream onward is
// at-least-once and deduplicated; nothing here can recover what never
// arrived.
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
		return nil, fmt.Errorf("create stream %s: %w", StreamName, err)
	}

	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:   bucketName,
		TTL:      BucketMaxAge,
		MaxBytes: BucketMaxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("create kv bucket %s: %w", bucketName, err)
	}

	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:   consumerName,
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer %s: %w", consumerName, err)
	}

	return cons.Consume(func(msg jetstream.Msg) {
		var key envelopeKey
		if err := json.Unmarshal(msg.Data(), &key); err != nil || key.SpanID == "" {
			// Malformed envelopes are dropped and ACKED, not Nak'd: nothing
			// about a redelivery would make unparseable JSON parse, and
			// Nak'ing would redeliver it forever behind the traffic that is
			// fine.
			log.Error("drop malformed pubsub envelope", "subject", msg.Subject(), "err", err)
			_ = msg.Ack()
			return
		}
		if err := storeEnvelope(ctx, kv, nc, log, tenantFromSubject(msg.Subject()), key.SpanID, msg.Data()); err != nil {
			log.Error("pubsub store projection failed, will redeliver", "spanId", key.SpanID, "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
}

// tenantFromSubject reads the tenant token out of the subject the envelope
// arrived on. "monitor.{tenant}.pubsub.>" is an imported tenant export
// (BR-AC34's remap); anything else — "obs.pubsub.>" — was published inside
// PLATFORM itself and has no tenant, so it reports the platform context.
func tenantFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 && parts[0] == "monitor" && parts[2] == "pubsub" {
		return parts[1]
	}
	return kvContext
}

// storeEnvelope writes one envelope under its spanId. Unlike tracestore's
// appendSpan there is nothing to merge, so this is a plain Put — but it is
// still idempotent by construction: the same spanId overwrites its own key
// with identical content, so a redelivery after a Nak cannot produce a second
// visible entry even if the stream's Duplicates window has already expired.
func storeEnvelope(ctx context.Context, kv jetstream.KeyValue, nc *nats.Conn, log *slog.Logger, tenant, spanID string, span json.RawMessage) error {
	key := keyPrefix + spanID
	data, err := json.Marshal(pubsubRecord{Tenant: tenant, Span: span})
	if err != nil {
		return err
	}
	if _, err := kv.Put(ctx, kvContext+"."+key, data); err != nil {
		return err
	}
	publishNotify(nc, log, key, data)
	return nil
}

// publishNotify fires notify.{context}.kv.{bucket}.{key}.changed after a
// successful Put — best-effort, a publish error is logged, never returned.
// This is 43c's live-update path, and the reason the Messages panel can reuse
// the Traces panel's bootstrap-fetch-plus-subscribe shape unchanged.
func publishNotify(nc *nats.Conn, log *slog.Logger, key string, value []byte) {
	subject := "notify." + kvContext + ".kv." + bucketName + "." + key + ".changed"
	if err := nc.Publish(subject, value); err != nil && log != nil {
		log.Warn("kv notify publish failed", "subject", subject, "err", err)
	}
}
