package dictionary

// Pending specs for Phase 67a (BUSINESS_RULES-SHIPPING.md's BR-045/BR-046):
// a new obs.pubsub.* envelope, sibling to obs.trace.* (BR-036/BR-037),
// published at each existing evt.*/notify.* choke point. Design approved
// (ADR-047, ARCHITECTURE-OBSERVABILITY.md); implementation is explicitly on
// hold, so these are placeholders derived directly from the rules, not from
// any implementation — no body references the not-yet-existing observation
// hook, so `ginkgo ./...` stays green (reported pending, not failing) until
// Phase 67a lands. Fill in real bodies (with gomega assertions) alongside
// the implementation, per this repo's red -> green -> refactor workflow.

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = PDescribe("obs.pubsub.* observability (Phase 67a, BR-045/BR-046)", func() {
	PContext("ShipHandler.publish — evt.* choke point (commands.go:317)", func() {
		PIt("publishes one obs.pubsub.{context}.shipping.ship.{action} envelope alongside the real evt.* publish")
		PIt("derives traceId/parentSpanId from natstrace.SpanFromContext(ctx) rather than minting an unrelated trace")
	})

	PContext("ContainerHandler.publish — evt.* choke point (container.go:232)", func() {
		PIt("publishes one obs.pubsub.{context}.shipping.container.{action} envelope alongside the real evt.* publish")
	})

	PContext("publishNotify / publishRawNotify — notify.* choke points (handler.go:187, handler.go:211)", func() {
		PIt("publishes one obs.pubsub.{context}.shipping.{entity}.{action} envelope alongside the real notify.* publish")
	})

	PContext("publishPortsChanged — notify.* choke point (adapter.go:472)", func() {
		PIt("publishes one obs.pubsub.{context}.shipping.port.changed envelope alongside the real notify.* publish")
	})

	PContext("hook placement discipline (BR-045)", func() {
		PIt("is never wired inside the shared low-level Publish/PublishWithTrace/PublishMsg primitive (internal/jstream)")
		PIt("never fires for observability-service's own tracestore.publishNotify (excluded — self-observation risk)")
	})

	PContext("relationship to the existing Phase 28d evt.*-projector-callback spans", func() {
		PIt("obs.pubsub.* (publish-side) coexists with obs.trace.* (consume-side, trace_async_test.go) without duplicating it")
	})

	PContext("payload redaction (BR-046)", func() {
		PIt("redacts obs.pubsub.* payloads using the same shared natstrace denylist as obs.trace.*, before the truncation cap")
	})
})
