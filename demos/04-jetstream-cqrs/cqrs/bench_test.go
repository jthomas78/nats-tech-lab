package main

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Specs for the rehydrate benchmark fixture (plan 04.7.16).
//
// Nothing here talks to a server. What is worth proving in a spec is the part
// that is a DECISION -- which stream a rehydration reads, how big a fixture
// is, and how much of it is deliberately left unfolded. The publishing itself
// is a handful of client calls and a spec that mocked them would only prove
// the mock.

var _ = Describe("a rehydration reads the source it was handed", func() {
	Context("the two sources are separate logs", func() {
		It("gives the live demo its own stream", func() {
			Expect(Live.Stream).To(Equal("ODOMETER"))
			Expect(Live.WriteKV).To(Equal("odometer-write"))
		})

		It("gives the benchmark a stream of its own", func() {
			Expect(Bench.Stream).To(Equal("ODOMETER_BENCH"))
			Expect(Bench.WriteKV).To(Equal("odometer-bench-write"))
		})

		// The point of the whole task. A million-event fixture on ODOMETER
		// would bury the log every other tab is drawn from.
		It("never lets the benchmark write to the demo's log", func() {
			Expect(Bench.Stream).NotTo(Equal(Live.Stream))
			Expect(Bench.WriteKV).NotTo(Equal(Live.WriteKV))
			Expect(Bench.VehicleSubject("v", "travelled")).
				NotTo(Equal(Live.VehicleSubject("v", "travelled")))
		})
	})

	Context("subjects a server will accept", func() {
		// names.go says why: an open first token textually overlaps $SYS.>
		// and $JS.API.>, and JetStream refuses such a stream without NoAck.
		It("starts every subject with the fixed literal evt", func() {
			for _, s := range []Source{Live, Bench} {
				Expect(s.StreamSubject).To(HavePrefix("evt."))
				Expect(s.VehicleSubject("v", "travelled")).To(HavePrefix("evt."))
				Expect(s.VehicleFilter("v")).To(HavePrefix("evt."))
			}
		})

		// Two streams may share a prefix only if a later token differs. This
		// is the line that lets both exist on one server.
		It("keeps the two stream filters from overlapping", func() {
			Expect(Bench.StreamSubject).NotTo(Equal(Live.StreamSubject))
			Expect(strings.HasPrefix(Bench.StreamSubject, strings.TrimSuffix(Live.StreamSubject, ">"))).
				To(BeFalse(), "evt.odometer.> would swallow the benchmark's subjects")
		})

		It("scopes a vehicle filter to one vehicle", func() {
			Expect(Bench.VehicleFilter("bench-10k")).To(Equal("evt.odometer-bench.vehicle.bench-10k.>"))
			Expect(Bench.VehicleSubject("bench-10k", "travelled")).
				To(Equal("evt.odometer-bench.vehicle.bench-10k.travelled"))
		})
	})
})

var _ = Describe("the benchmark fixture", func() {
	Context("the sizes a reader may ask for", func() {
		It("offers three, and they rise by ten", func() {
			Expect(BenchSizes).To(Equal([]int{10_000, 100_000, 1_000_000}))
		})

		// A fixed list is the cap. There is no validator to get wrong.
		It("refuses a size that is not on the list", func() {
			_, err := benchPlanFor(12_345)
			Expect(err).To(MatchError(ErrUnknownBenchSize))
		})
	})

	Context("what one size describes", func() {
		It("gives each size a vehicle of its own", func() {
			small, err := benchPlanFor(10_000)
			Expect(err).NotTo(HaveOccurred())
			big, err := benchPlanFor(1_000_000)
			Expect(err).NotTo(HaveOccurred())
			Expect(small.Vehicle).NotTo(Equal(big.Vehicle))
		})

		// The size on the button is the number of events in the log, so a
		// reader comparing 10 000 with 100 000 is comparing what it says.
		It("counts the registration as one of the events", func() {
			p, err := benchPlanFor(10_000)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Events).To(Equal(10_000))
			Expect(p.Trips).To(Equal(9_999))
		})

		// CLAUDE.md: "The snapshot is always stale." A fixture whose snapshot
		// were perfectly current would measure a snapshot read, not a
		// rehydration, and would flatter the snapshot side.
		It("leaves a tail the snapshot side must still replay", func() {
			for _, n := range BenchSizes {
				p, err := benchPlanFor(n)
				Expect(err).NotTo(HaveOccurred())
				Expect(p.Tail).To(BeNumerically(">", 0))
				Expect(p.Tail).To(BeNumerically("<", p.Events))
			}
		})

		// The tail is the same at every size on purpose: it is what the
		// snapshot side pays, and holding it still is what makes the three
		// measurements comparable.
		It("uses the same tail at every size", func() {
			small, _ := benchPlanFor(10_000)
			big, _ := benchPlanFor(1_000_000)
			Expect(small.Tail).To(Equal(big.Tail))
		})

		It("folds everything except the tail into the snapshot", func() {
			p, err := benchPlanFor(100_000)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Folded).To(Equal(p.Events - p.Tail))
		})
	})
})
