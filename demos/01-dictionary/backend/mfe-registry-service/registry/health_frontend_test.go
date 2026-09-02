package registry_test

// Phase 15 — frontend health ARRIVES; the registry does not go and get it
// (BR-AS61, decision 14).
//
// This file replaces the Phase 5d outbound-probe suite. That suite's
// interesting specs were all about what the registry refused to DIAL — an
// unmapped origin, a redirect, an oversized body. None of those risks exist
// any more, because the registry no longer dials anything for health: the
// map of origins is gone and so is the outbound HTTP client.
//
// What replaces them is a different set of refusals, on the receiving side.
// A report is a message from a party the registry does not control, so the
// interesting specs are: a body that speaks for a different plugin, a state
// or cause outside the closed vocabulary, a timestamp that would buy
// permanent freshness, and a redelivery that would refresh a dead plugin's
// lease.
//
// A believed report is also deliberately narrow: it says a plugin looked at
// its own /healthz and got an answer, and nothing at all about whether a
// browser can fetch remoteEntry.js from that origin. The loader still owns
// that.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

var _ = Describe("BR-AS61 — a plugin reports its own frontend health", func() {
	const pluginID = "plugin-a"
	at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	healthy := func(t time.Time) mferegistry.HealthReport {
		return mferegistry.HealthReport{PluginID: pluginID, State: mferegistry.HealthReportHealthy, At: t.UnixMilli()}
	}

	Context("what the inbox believes", func() {
		It("records a healthy report as healthy, timestamped by the plugin", func() {
			inbox := domain.NewHealthInbox()
			Expect(inbox.Accept(pluginID, healthy(at), at)).To(BeTrue())
			signal := inbox.Signal(pluginID, at)
			Expect(signal.State).To(Equal(domain.HealthHealthy))
			Expect(signal.Cause).To(BeEmpty())
			Expect(signal.LastCheckAt).To(BeTemporally("==", at))
		})

		It("records a plugin's own unhealthy verdict as unavailable, keeping the cause it gave", func() {
			inbox := domain.NewHealthInbox()
			report := mferegistry.HealthReport{PluginID: pluginID, State: mferegistry.HealthReportUnhealthy, Cause: mferegistry.HealthCauseHTTPStatus, At: at.UnixMilli()}
			Expect(inbox.Accept(pluginID, report, at)).To(BeTrue())
			signal := inbox.Signal(pluginID, at)
			Expect(signal.State).To(Equal(domain.HealthUnavailable))
			Expect(signal.Cause).To(Equal(mferegistry.HealthCauseHTTPStatus))
		})

		It("reports unknown for a plugin nothing has ever been heard from, never healthy and never absent", func() {
			signal := domain.NewHealthInbox().Signal(pluginID, at)
			Expect(signal.State).To(Equal(domain.HealthUnknown))
			Expect(signal.Cause).To(BeEmpty())
		})
	})

	Context("what the inbox refuses", func() {
		It("refuses a body that speaks for a plugin other than the one the subject named", func() {
			inbox := domain.NewHealthInbox()
			report := healthy(at)
			report.PluginID = "plugin-b"
			Expect(inbox.Accept(pluginID, report, at)).To(BeFalse())
			Expect(inbox.Signal(pluginID, at).State).To(Equal(domain.HealthUnknown))
			Expect(inbox.Signal("plugin-b", at).State).To(Equal(domain.HealthUnknown))
		})

		It("refuses a state outside the two a plugin may claim about itself", func() {
			inbox := domain.NewHealthInbox()
			for _, state := range []string{"", "stale", "unknown", "not configured", "ok"} {
				report := healthy(at)
				report.State = state
				Expect(inbox.Accept(pluginID, report, at)).To(BeFalse(), state)
			}
		})

		It("refuses a cause outside the closed vocabulary, including the one only a receiver may conclude", func() {
			inbox := domain.NewHealthInbox()
			for _, cause := range []string{"connection refused to 10.0.0.4:8080", mferegistry.HealthCauseAbsent} {
				report := mferegistry.HealthReport{PluginID: pluginID, State: mferegistry.HealthReportUnhealthy, Cause: cause, At: at.UnixMilli()}
				Expect(inbox.Accept(pluginID, report, at)).To(BeFalse(), cause)
			}
		})

		It("refuses a redelivered or out-of-order report, so nothing can refresh a dead plugin's lease", func() {
			inbox := domain.NewHealthInbox()
			Expect(inbox.Accept(pluginID, healthy(at), at)).To(BeTrue())
			Expect(inbox.Accept(pluginID, healthy(at), at)).To(BeFalse())
			Expect(inbox.Accept(pluginID, healthy(at.Add(-time.Second)), at)).To(BeFalse())
			Expect(inbox.Signal(pluginID, at).LastCheckAt).To(BeTemporally("==", at))
		})
	})

	Context("BR-AS64 — freshness is the detection mechanism, not a backstop", func() {
		It("holds a plugin healthy right up to the window", func() {
			inbox := domain.NewHealthInbox()
			inbox.Accept(pluginID, healthy(at), at)
			Expect(inbox.Signal(pluginID, at.Add(mferegistry.HealthFrontendFreshness)).State).To(Equal(domain.HealthHealthy))
		})

		It("calls a plugin stale past the window, with absent as the cause", func() {
			inbox := domain.NewHealthInbox()
			inbox.Accept(pluginID, healthy(at), at)
			signal := inbox.Signal(pluginID, at.Add(mferegistry.HealthFrontendFreshness+time.Millisecond))
			Expect(signal.State).To(Equal(domain.HealthStale))
			Expect(signal.Cause).To(Equal(mferegistry.HealthCauseAbsent))
		})

		It("keeps absent and unhealthy separate, so the common case is not shown as the rare one", func() {
			inbox := domain.NewHealthInbox()
			inbox.Accept(pluginID, mferegistry.HealthReport{PluginID: pluginID, State: mferegistry.HealthReportUnhealthy, Cause: mferegistry.HealthCauseUnreachable, At: at.UnixMilli()}, at)
			live := inbox.Signal(pluginID, at)
			gone := inbox.Signal(pluginID, at.Add(time.Minute))
			Expect(live.State).NotTo(Equal(gone.State))
			Expect(live.Cause).To(Equal(mferegistry.HealthCauseUnreachable))
			Expect(gone.Cause).To(Equal(mferegistry.HealthCauseAbsent))
		})

		It("clamps a report stamped in the future to the receiver's clock, so no plugin can buy permanent freshness", func() {
			inbox := domain.NewHealthInbox()
			Expect(inbox.Accept(pluginID, healthy(at.Add(time.Hour)), at)).To(BeTrue())
			Expect(inbox.Signal(pluginID, at.Add(time.Minute)).State).To(Equal(domain.HealthStale))
		})

		It("relates the heartbeat to the window rather than pinning either, so one cannot be moved past the other", func() {
			Expect(mferegistry.HealthHeartbeat).To(BeNumerically("<", mferegistry.HealthFrontendFreshness))
		})
	})

	Context("BR-AS65 — a reading never outlives the plugin it is about", func() {
		It("forgets a plugin that has left the catalogue", func() {
			inbox := domain.NewHealthInbox()
			inbox.Accept(pluginID, healthy(at), at)
			inbox.Forget(map[string]bool{"plugin-b": true})
			Expect(inbox.Signal(pluginID, at).State).To(Equal(domain.HealthUnknown))
		})
	})
})
