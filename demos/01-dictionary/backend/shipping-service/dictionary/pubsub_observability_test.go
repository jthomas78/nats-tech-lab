package dictionary

// Pending specs for Phase 43a (BUSINESS_RULES-SHIPPING.md's BR-045/BR-046/
// BR-049): a new obs.pubsub.* envelope, sibling to obs.trace.* (BR-036/
// BR-037), published from the evt.* seam and from each notify.* call site.
// Design approved (ADR-047) and amended 2026-08-25 by a pre-implementation
// review — the hook placement below reflects that amendment (A3), not the
// original per-call-site-everywhere rule. Implementation is explicitly on
// hold for the remaining call sites, so these are placeholders derived
// directly from the rules, not from any implementation — no body references the not-yet-existing observation
// hook, so `ginkgo ./...` stays green (reported pending, not failing) until
// Phase 43a lands. Fill in real bodies (with gomega assertions) alongside
// the implementation, per this repo's red -> green -> refactor workflow.

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = PDescribe("obs.pubsub.* observability (Phase 43a, BR-045/BR-046/BR-049)", func() {
	// The evt.* seam itself LANDED in the Phase 43a vertical slice and is
	// covered by real specs next to the code it tests, in
	// internal/jstream/stream_test.go: TestPublishWithTraceObservesShipEvent,
	// ...ObservesContainerEvent, ...ContinuesTheCausingTrace,
	// ...RedactsBeforeObserving, and TestPublisherWithoutObservationStaysSilent.
	// Nothing is pending for it here.

	PContext("notify.* call sites — wired individually, no seam exists", func() {
		PIt("publishNotify (handler.go:191) publishes one obs.pubsub.{context}.shipping.{entity}.changed envelope")
		PIt("publishRawNotify (handler.go:215) publishes one obs.pubsub.{context}.shipping.raw.{entity}.{event} envelope")
		PIt("publishPortsChanged (adapter.go:484) publishes one obs.pubsub.{context}.shipping.port.changed envelope")
		PIt("publishChange (internal/kvstore/kv.go:114) publishes one envelope for the KV-change notify")
		PIt("publishRefdataChanged (platform_notify.go:120) publishes one envelope for the notify._platform.refdata.* re-publish")
	})

	PContext("hook placement discipline (BR-045)", func() {
		PIt("is never wired inside the generic Publish/PublishMsg primitives (internal/jstream)")
		PIt("never fires for observability-service's own tracestore.publishNotify (excluded — internal plumbing, not a domain event)")
		PIt("does fire for internal/kvstore's publishChange, the original that copy was made from")
	})

	PContext("failure semantics (BR-045, ADR-047 A7)", func() {
		PIt("drops its own publish error and never fails the domain publish it observes")
		PIt("does not measurably delay a command already awaiting a synchronous PubAck")
	})

	PContext("coverage is a checked convention (BR-049)", func() {
		PIt("fails when a notify.* publish literal exists that is on neither the instrumented list nor the documented exclusion list")
	})

	PContext("relationship to the existing Phase 28d evt.*-projector-callback spans", func() {
		PIt("obs.pubsub.* (publish-side) coexists with obs.trace.* (consume-side, trace_async_test.go) without duplicating it")
	})

	PContext("payload redaction (BR-046)", func() {
		PIt("redacts obs.pubsub.* payloads using the same shared natstrace denylist as obs.trace.*, before the truncation cap")
	})
})
