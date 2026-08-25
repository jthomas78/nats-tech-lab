package tracestore

// Pending tests for Phase 43b (BUSINESS_RULES-SHIPPING.md's BR-047):
// observability-service gains a sibling consumer to Register (this file) for
// obs.pubsub.>. Design approved (ADR-047) and amended 2026-08-25 — the
// separate-stream, measured-caps and explicit-Duplicates assertions below
// are that amendment (A5, A6). Implementation on hold; skipped rather than
// asserting against the not-yet-existing sibling consumer/stream, so
// `go test ./...` stays green until Phase 43b lands. Whether ingestion needs
// a KV bucket (like trace-request-reply) or is stream-only is left to
// implementation time per BR-047 — not asserted here either way.

import "testing"

func TestPubsubUsesItsOwnStreamNotASecondSubjectOnTraces(t *testing.T) {
	t.Skip("pending Phase 43b implementation — see BR-047 (ADR-047 A5): " +
		"obs.pubsub.> must live on its own stream, named per CLAUDE.md's " +
		"SCREAMING_SNAKE rule — not as a second subject set on TRACES, where " +
		"an evt.* burst could evict RPC traces.")
}

func TestPubsubStreamProvisionedWithBoundedRetention(t *testing.T) {
	t.Skip("pending Phase 43b implementation — see BR-047: the stream must " +
		"use LimitsPolicy retention with MaxAge/MaxBytes sized from a measured " +
		"seed run, not inherited unexamined from TRACES's 1h / 64 MiB — at " +
		"~2 KiB an envelope that is ~32k messages, minutes of history under " +
		"load rather than the advertised hour.")
}

func TestPubsubRedeliveryIsDeduplicatedByMessageID(t *testing.T) {
	t.Skip("pending Phase 43b implementation — see BR-047 (ADR-047 A6): the " +
		"stream must set an explicit Duplicates window rather than relying on " +
		"the 2-minute default, and BR-045's emit must set Nats-Msg-Id to the " +
		"envelope's spanId — without both, dedup is unenforceable. A " +
		"redelivery of the same envelope must not produce a duplicate entry.")
}

func TestPubsubIngestionIsBestEffortAndSaysSo(t *testing.T) {
	t.Skip("pending Phase 43b implementation — see BR-047 (ADR-047 A7): " +
		"BR-045's emit is a core-NATS fire-and-forget publish, lossy under a " +
		"slow consumer or reconnect, so the feed is best-effort. The panel " +
		"must not imply completeness it cannot deliver.")
}
