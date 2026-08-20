package kvcache

// Pending test for Phase 67a (BUSINESS_RULES-REFDATA.md's BR-D45, pointing
// at BUSINESS_RULES-SHIPPING.md's BR-045): refdata-service's own evt.*
// publish choke point (kvcache.go:146) gets the same obs.pubsub.* hook, on
// this service's publisher side. Design approved (ADR-047); implementation
// on hold. Skipped rather than asserting against the not-yet-existing hook,
// so `go test ./...` stays green until Phase 67a lands.

import "testing"

func TestKVCachePublishesObsPubsubEnvelopeAlongsideEvtPublish(t *testing.T) {
	t.Skip("pending Phase 67a implementation — see BR-D45/BR-045: " +
		"kvcache's publish-change call site (kvcache.go:146) must publish one " +
		"obs.pubsub.{context}.refdata.{typeKey}.changed envelope alongside its " +
		"real evt.{context}.refdata.{typeKey}.changed publish, using the same " +
		"shared natstrace hook and redaction discipline as BR-045.")
}
