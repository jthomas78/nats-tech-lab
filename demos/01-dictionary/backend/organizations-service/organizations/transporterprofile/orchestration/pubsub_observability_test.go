package orchestration

// Pending tests for Phase 43a (BUSINESS_RULES-ORGANIZATIONS.md's BR-TP75,
// pointing at BUSINESS_RULES-SHIPPING.md's BR-045). This service had no rule
// at all in ADR-047 as originally approved — an entire service's evt.*
// traffic would have been invisible to the Messages panel; the 2026-08-25
// pre-implementation review added one (A2). Implementation on hold; skipped
// rather than asserting against the not-yet-existing hook, so
// `go test ./...` stays green until Phase 43a lands.

import "testing"

func TestAppendIsTheEvtSeamAndIsObserved(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-TP75/BR-045: every " +
		"transporter-profile event reaches JetStream through " +
		"JetStreamEventStore.append, so the obs.pubsub.* hook belongs there " +
		"— not at the workflow activities that call it. One " +
		"obs.pubsub.{context}.organizations.transporter-profile.{event} " +
		"envelope per appended event, and a new event type is observed by " +
		"construction.")
}

func TestAppendObservationNeverFailsOrDelaysTheDomainAppend(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-045 (ADR-047 A7): " +
		"append already awaits a synchronous PublishMsg PubAck and handles " +
		"ErrSequenceConflict; the observation emit must be fire-and-forget, " +
		"drop its own error, and never change append's result or timing.")
}

func TestTransporterProfilePayloadsPassTheRedactionReview(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-TP75/BR-046 (ADR-047 " +
		"A8): these payloads carry compliance documents and sit beside the " +
		"organizations-secrets bucket, making them the priority case for the " +
		"redaction review — which runs BEFORE the hook is wired. If a field " +
		"here is uncovered, the shared denylist is extended, never forked.")
}
