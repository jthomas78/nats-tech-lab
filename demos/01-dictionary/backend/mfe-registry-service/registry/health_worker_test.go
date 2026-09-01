package registry_test

// Phase 5d — the health worker's timing, as a machine that is STEPPED rather
// than one that sleeps (BR-AS63).
//
// A worker built out of goroutines and real timers can only be tested by
// waiting, and a spec that waits is a spec that is flaky on a loaded machine
// and slow on every machine. So the cadence, the failure counting and the
// freshness window live in a pure state machine that is handed a `now`: the
// clock is the fake clock, and the thin runner that owns a real ticker has no
// decisions left in it to get wrong.
//
// The three numbers are Q5 and Q14: probe every 5 seconds, give a probe 2
// seconds, two consecutive failures make a target unavailable, one success
// brings it back, and an observation older than 15 seconds is stale rather
// than current.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

var _ = Describe("BR-AS63 — health transitions use deterministic thresholds", func() {
	var (
		worker *domain.HealthWorker
		t0     time.Time
	)

	BeforeEach(func() {
		t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		worker = domain.NewHealthWorker([]string{"fleet-ops", "pricing"})
	})

	// probe runs one full cycle for a target: claim it, then report.
	probe := func(id string, ok bool, at time.Time) {
		Expect(worker.Due(at)).To(ContainElement(id))
		if ok {
			worker.Record(id, domain.HealthProbeOK(at))
		} else {
			worker.Record(id, domain.HealthProbeFailed("timeout", at))
		}
	}

	stateOf := func(id string, now time.Time) domain.HealthSignal {
		return worker.Snapshot(now)[id]
	}

	Context("the cadence", func() {
		It("offers every target on the first tick, because nothing has been checked yet", func() {
			Expect(worker.Due(t0)).To(ConsistOf("fleet-ops", "pricing"))
		})

		It("does not offer the same target again until the interval has passed", func() {
			worker.Due(t0)
			worker.Record("fleet-ops", domain.HealthProbeOK(t0))

			Expect(worker.Due(t0.Add(4 * time.Second))).To(BeEmpty())
			Expect(worker.Due(t0.Add(5 * time.Second))).To(ContainElement("fleet-ops"))
		})

		It("never offers a target whose probe is still running, so probes cannot overlap", func() {
			worker.Due(t0)

			// A whole interval later the earlier probe has still not answered.
			Expect(worker.Due(t0.Add(10 * time.Second))).To(BeEmpty())
		})

		It("offers it again once the slow probe finally answers", func() {
			worker.Due(t0)
			worker.Record("fleet-ops", domain.HealthProbeOK(t0.Add(1500*time.Millisecond)))

			Expect(worker.Due(t0.Add(10 * time.Second))).To(ContainElement("fleet-ops"))
		})

		It("gives a probe two seconds, which is shorter than the interval on purpose", func() {
			Expect(domain.HealthProbeTimeout).To(Equal(2 * time.Second))
			Expect(domain.HealthProbeTimeout).To(BeNumerically("<", domain.HealthProbeInterval))
			Expect(domain.HealthProbeInterval).To(Equal(5 * time.Second))
		})
	})

	Context("the thresholds", func() {
		It("starts unknown, with no invented timestamp", func() {
			got := stateOf("fleet-ops", t0)
			Expect(got.State).To(Equal(domain.HealthUnknown))
			Expect(got.LastCheckAt.IsZero()).To(BeTrue(), "nothing has been checked, so there is no time to show")
		})

		It("does not claim healthy on a first failure from unknown", func() {
			probe("fleet-ops", false, t0)

			Expect(stateOf("fleet-ops", t0).State).To(Equal(domain.HealthUnknown))
		})

		It("makes a target unavailable on the second consecutive failure", func() {
			probe("fleet-ops", false, t0)
			probe("fleet-ops", false, t0.Add(5*time.Second))

			got := stateOf("fleet-ops", t0.Add(5*time.Second))
			Expect(got.State).To(Equal(domain.HealthUnavailable))
			Expect(got.Cause).To(Equal("timeout"))
		})

		It("keeps a healthy target healthy through ONE failure", func() {
			probe("fleet-ops", true, t0)
			probe("fleet-ops", false, t0.Add(5*time.Second))

			Expect(stateOf("fleet-ops", t0.Add(5*time.Second)).State).To(Equal(domain.HealthHealthy))
		})

		It("recovers on a single success", func() {
			probe("fleet-ops", false, t0)
			probe("fleet-ops", false, t0.Add(5*time.Second))
			probe("fleet-ops", true, t0.Add(10*time.Second))

			got := stateOf("fleet-ops", t0.Add(10*time.Second))
			Expect(got.State).To(Equal(domain.HealthHealthy))
			Expect(got.Cause).To(BeEmpty())
		})

		It("lets an intervening success reset the count, so failures must be CONSECUTIVE", func() {
			probe("fleet-ops", false, t0)
			probe("fleet-ops", true, t0.Add(5*time.Second))
			probe("fleet-ops", false, t0.Add(10*time.Second))

			Expect(stateOf("fleet-ops", t0.Add(10*time.Second)).State).To(Equal(domain.HealthHealthy))
		})

		It("counts each target on its own", func() {
			probe("fleet-ops", false, t0)
			probe("fleet-ops", false, t0.Add(5*time.Second))

			Expect(stateOf("fleet-ops", t0.Add(5*time.Second)).State).To(Equal(domain.HealthUnavailable))
			Expect(stateOf("pricing", t0.Add(5*time.Second)).State).To(Equal(domain.HealthUnknown))
		})
	})

	Context("freshness (BR-AS64)", func() {
		It("keeps an observation current inside the window", func() {
			probe("fleet-ops", true, t0)

			Expect(stateOf("fleet-ops", t0.Add(15*time.Second)).State).To(Equal(domain.HealthHealthy))
		})

		It("goes stale past the window, and still says when it last looked", func() {
			probe("fleet-ops", true, t0)

			got := stateOf("fleet-ops", t0.Add(15*time.Second+time.Nanosecond))
			Expect(got.State).To(Equal(domain.HealthStale))
			Expect(got.LastCheckAt).To(Equal(t0), "a stale reading still knows when it was taken")
		})

		It("goes stale from unavailable too — an old failure is not a current fact either", func() {
			probe("fleet-ops", false, t0)
			probe("fleet-ops", false, t0.Add(5*time.Second))

			Expect(stateOf("fleet-ops", t0.Add(30*time.Second)).State).To(Equal(domain.HealthStale))
		})

		It("leaves an unchecked target unknown rather than stale", func() {
			// Stale means "this was true once". Unknown means "we have never
			// looked". Collapsing them would invent a past that never happened.
			Expect(stateOf("pricing", t0.Add(1*time.Hour)).State).To(Equal(domain.HealthUnknown))
		})

		It("ages each signal separately", func() {
			// One tick claims both targets; the second answer comes back much
			// later, so the two readings are of different ages.
			Expect(worker.Due(t0)).To(ConsistOf("fleet-ops", "pricing"))
			worker.Record("fleet-ops", domain.HealthProbeOK(t0))
			worker.Record("pricing", domain.HealthProbeOK(t0.Add(20*time.Second)))

			at := t0.Add(20 * time.Second)
			Expect(stateOf("fleet-ops", at).State).To(Equal(domain.HealthStale))
			Expect(stateOf("pricing", at).State).To(Equal(domain.HealthHealthy))
		})
	})

	Context("shutdown", func() {
		It("drops probes in flight so a late answer cannot land after the stop", func() {
			worker.Due(t0)
			worker.Stop()

			worker.Record("fleet-ops", domain.HealthProbeOK(t0.Add(time.Second)))
			Expect(stateOf("fleet-ops", t0.Add(time.Second)).State).To(Equal(domain.HealthUnknown))
		})

		It("offers nothing after it has stopped", func() {
			worker.Stop()

			Expect(worker.Due(t0.Add(time.Hour))).To(BeEmpty())
		})
	})
})

var _ = Describe("BR-AS62 — a backend summary is derived from every dependency", func() {
	healthy := domain.HealthSignal{State: domain.HealthHealthy}
	unknown := domain.HealthSignal{State: domain.HealthUnknown}
	stale := domain.HealthSignal{State: domain.HealthStale}
	down := domain.HealthSignal{State: domain.HealthUnavailable}

	It("reports not configured when the deployment mapped nothing", func() {
		// Absent is not the same as empty, and neither is health. A plugin
		// nobody mapped has not been judged (BR-AS62).
		Expect(domain.SummarizeBackend(nil).State).To(Equal(domain.HealthNotConfigured))
	})

	It("reports not applicable for an explicitly empty list", func() {
		Expect(domain.SummarizeBackend([]domain.HealthSignal{}).State).To(Equal(domain.HealthNotApplicable))
	})

	It("lets one unavailable dependency make the summary unavailable", func() {
		Expect(domain.SummarizeBackend([]domain.HealthSignal{healthy, down, healthy}).State).
			To(Equal(domain.HealthUnavailable))
	})

	It("prefers unavailable over unknown, because the worse fact is the true one", func() {
		Expect(domain.SummarizeBackend([]domain.HealthSignal{unknown, down}).State).
			To(Equal(domain.HealthUnavailable))
	})

	It("falls back to unknown when nothing is down but something is unjudged", func() {
		Expect(domain.SummarizeBackend([]domain.HealthSignal{healthy, unknown}).State).
			To(Equal(domain.HealthUnknown))
	})

	It("treats a stale dependency as not current, never as healthy", func() {
		Expect(domain.SummarizeBackend([]domain.HealthSignal{healthy, stale}).State).
			To(Equal(domain.HealthStale))
	})

	It("only says healthy when every dependency is healthy", func() {
		Expect(domain.SummarizeBackend([]domain.HealthSignal{healthy, healthy}).State).
			To(Equal(domain.HealthHealthy))
	})
})

var _ = Describe("BR-AS64 — an old snapshot never looks current", func() {
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	It("keeps the newer of two readings", func() {
		held := domain.HealthSignal{State: domain.HealthHealthy, LastCheckAt: t0.Add(5 * time.Second)}
		old := domain.HealthSignal{State: domain.HealthUnavailable, LastCheckAt: t0}

		Expect(held.Merge(old)).To(Equal(held), "an out-of-order snapshot is dropped, not applied")
	})

	It("applies a newer reading", func() {
		held := domain.HealthSignal{State: domain.HealthHealthy, LastCheckAt: t0}
		next := domain.HealthSignal{State: domain.HealthUnavailable, LastCheckAt: t0.Add(5 * time.Second)}

		Expect(held.Merge(next)).To(Equal(next))
	})

	It("does not let a duplicate refresh the check time", func() {
		// The failure this exists to stop: a hint or a re-read redelivering
		// the same observation, and the shell treating each arrival as proof
		// that the target was alive just now (BR-AS65 — a hint is not an
		// observation).
		held := domain.HealthSignal{State: domain.HealthHealthy, LastCheckAt: t0}

		Expect(held.Merge(held).LastCheckAt).To(Equal(t0))
	})

	It("accepts any first reading over an empty one", func() {
		next := domain.HealthSignal{State: domain.HealthHealthy, LastCheckAt: t0}

		Expect(domain.HealthSignal{}.Merge(next)).To(Equal(next))
	})
})
