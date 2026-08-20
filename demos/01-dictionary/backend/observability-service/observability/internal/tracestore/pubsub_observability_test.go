package tracestore

// Pending tests for Phase 67b (BUSINESS_RULES-SHIPPING.md's BR-047):
// observability-service gains a sibling consumer to Register (this file) for
// obs.pubsub.>, mirroring TRACES's stream/retention shape. Design approved
// (ADR-047); implementation on hold. Skipped rather than asserting against
// the not-yet-existing sibling consumer/stream, so `go test ./...` stays
// green until Phase 67b lands. Whether ingestion needs a KV bucket (like
// trace-request-reply) or is stream-only is left to implementation time
// per BR-047 — not asserted here either way.

import "testing"

func TestPubsubStreamProvisionedWithBoundedRetention(t *testing.T) {
	t.Skip("pending Phase 67b implementation — see BR-047: a sibling stream " +
		"for obs.pubsub.> must exist with LimitsPolicy retention, defaulting " +
		"to StreamMaxAge/StreamMaxBytes (1h / 64 MiB) unless evt.* volume " +
		"needs a tighter cap.")
}

func TestPubsubEnvelopeBecomesVisibleInMessagesFeed(t *testing.T) {
	t.Skip("pending Phase 67b implementation — see BR-047: every published " +
		"obs.pubsub.* envelope must become visible in the Messages feed the " +
		"Admin UI reads (BR-048).")
}

func TestPubsubRedeliveryIsDeduplicatedByMessageID(t *testing.T) {
	t.Skip("pending Phase 67b implementation — see BR-047: an at-least-once " +
		"redelivery of the same obs.pubsub.* envelope (same span/message id) " +
		"must not produce a duplicate visible entry, mirroring appendSpan's " +
		"existing spanId dedup for obs.trace.*.")
}
