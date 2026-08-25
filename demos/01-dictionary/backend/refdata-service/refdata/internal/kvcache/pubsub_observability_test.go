package kvcache

// Pending tests for Phase 43a (BUSINESS_RULES-REFDATA.md's BR-D45, pointing
// at BUSINESS_RULES-SHIPPING.md's BR-045). Design approved (ADR-047) and
// amended 2026-08-25: this service's evt.* traffic is now observed in the
// PublishWithTrace seam rather than wired at kvcache.go:146 itself (A3),
// and notifybridge.go's notify.* publish — missed in the original rule —
// is in scope (A2). Implementation on hold; skipped rather than asserting
// against the not-yet-existing hook, so `go test ./...` stays green until
// Phase 43a lands.

import "testing"

func TestEvtPublishIsObservedViaTheSeamNotTheCallSite(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-D45/BR-045: the " +
		"evt.{context}.refdata.{typeKey}.changed publish at kvcache.go:146 " +
		"must yield one obs.pubsub.{context}.refdata.{typeKey}.changed " +
		"envelope, emitted from the shared PublishWithTrace seam — not from " +
		"per-call-site wiring here — so a future evt.* publisher in this " +
		"service is covered by construction.")
}

func TestNotifyBridgePublishIsInstrumented(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-D45/BR-045 (ADR-047 " +
		"A2): notifybridge.go:94's notify.{context}.refdata.{typeKey}.changed " +
		"publish has no seam, so it is wired individually and appears on " +
		"BR-049's checked instrumented list.")
}
