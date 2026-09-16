package main

// 04.8.3 — the correct fold for lesson 02.
//
// Lesson 02 shows damage, and damage is a DIFFERENCE. The screen subtracts a
// correct total from the pool's total and calls the remainder lost kilometres.
// Until 04.8 the correct total came from odometer-read, which folds a
// DIFFERENT log. Once lesson 02 has its own log that subtraction stops meaning
// anything -- it would be two unrelated histories differenced and printed as a
// defect.
//
// So ODOMETER_POOL gets its own right answer, folded by one worker, one at a
// time, in order, into odometer-pool-truth. Two logs, two folds, one
// comparison that is fair.
//
// What these specs can and cannot prove:
//
//	The fold ITSELF is project(), the same function the read model uses,
//	and it is already spec'd in read.go's own suite. Running it again here
//	would test the same code twice.
//
//	What is new is the CONSUMER that feeds it and the HISTORY it is fed.
//	Those are pinned below, and then the arithmetic is checked against the
//	fold's output -- not used in place of it.

import (
	"time"

	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("lesson 02's correct fold", func() {
	plan, planErr := poolPlanFor(DefaultPoolSize)

	It("plans before it folds", func() {
		Expect(planErr).NotTo(HaveOccurred())
	})

	Context("the consumer that produces it", func() {
		cfg := poolTruthConsumerConfig()

		// One at a time, in order. A fold with a cap above 1 is the
		// lesson's OWN defect, and building the right answer with it
		// would leave nothing correct to compare against.
		It("folds one event at a time", func() {
			Expect(cfg.MaxAckPending).To(Equal(1))
		})

		It("replays the whole log from the start", func() {
			Expect(cfg.DeliverPolicy).To(Equal(jetstream.DeliverAllPolicy))
			Expect(cfg.AckPolicy).To(Equal(jetstream.AckExplicitPolicy))
		})

		It("is durable and named apart from the pool's own consumer", func() {
			Expect(cfg.Durable).To(Equal(PoolTruthConsumer))
			Expect(cfg.Durable).NotTo(Equal(PoolConsumer))
		})

		// The whole point of 04.8. A truth consumer on ODOMETER would
		// put lesson 02 back on the demo's own log by the back door.
		It("reads lesson 02's log and never the demo's", func() {
			Expect(poolTruthStream()).To(Equal(Pool.Stream))
			Expect(poolTruthStream()).NotTo(Equal(Live.Stream))
			Expect(cfg.FilterSubject).To(Equal(Pool.StreamSubject))
			Expect(cfg.FilterSubject).NotTo(Equal(Live.StreamSubject))
		})
	})

	Context("the history the seed writes", func() {
		It("is exactly as long as the plan says", func() {
			Expect(poolEvents(plan)).To(HaveLen(plan.Events))
		})

		It("registers every vehicle first, once each", func() {
			evs := poolEvents(plan)
			seen := []string{}
			for _, e := range evs[:len(plan.Vehicles)] {
				Expect(e.Event).To(BeAssignableToTypeOf(Registered{}))
				seen = append(seen, e.Vehicle)
			}
			Expect(seen).To(Equal(plan.Vehicles))
		})

		It("makes every later event a trip", func() {
			for _, e := range poolEvents(plan)[len(plan.Vehicles):] {
				Expect(e.Event).To(BeAssignableToTypeOf(Travelled{}))
			}
		})

		// Round robin, and this is the spec that keeps it that way. A
		// history grouped by vehicle would let one worker take a whole
		// vehicle in one contiguous run and fold it in perfect order --
		// the one arrangement lesson 02 must never make by accident.
		It("never puts two trips for one vehicle side by side", func() {
			evs := poolEvents(plan)
			trips := evs[len(plan.Vehicles):]
			for i := 1; i < len(trips); i++ {
				Expect(trips[i].Vehicle).NotTo(Equal(trips[i-1].Vehicle))
			}
		})

		It("publishes on lesson 02's subjects, and they parse back", func() {
			for _, e := range poolEvents(plan) {
				Expect(e.Subject).To(HavePrefix("evt.odometer-pool."))
				id, err := vehicleIDFrom(e.Subject)
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(e.Vehicle))
			}
		})
	})

	Context("what the fold produces", func() {
		// The same three moves project() makes per message, with the
		// NATS parts removed: ask the rules, apply, advance.
		fold := func() (map[string]ReadEntry, int, int) {
			at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
			out := map[string]ReadEntry{}
			refused, ignored := 0, 0
			for i, e := range poolEvents(plan) {
				seq := uint64(i + 1)
				cur := out[e.Vehicle]
				apply, err := cur.Next(seq)
				if err != nil {
					refused++
					continue
				}
				if !apply {
					ignored++
					continue
				}
				cur.Odometer = cur.Odometer.Apply(e.Event, at)
				cur.Fold = cur.Advance(seq)
				out[e.Vehicle] = cur
			}
			return out, refused, ignored
		}

		It("keeps every event: none refused, none ignored", func() {
			_, refused, ignored := fold()
			Expect(refused).To(Equal(0))
			Expect(ignored).To(Equal(0))
		})

		It("holds one document per vehicle, registered and plated", func() {
			got, _, _ := fold()
			Expect(got).To(HaveLen(len(plan.Vehicles)))
			for _, v := range plan.Vehicles {
				Expect(got[v].Status).To(Equal(StatusRegistered))
				Expect(got[v].Plate).To(Equal(v))
			}
		})

		// The arithmetic checks the fold. It does not replace it: these
		// totals come out of Odometer.Apply, and the plan is only what
		// they are held against.
		It("totals the seeded distance exactly", func() {
			got, _, _ := fold()
			var km float64
			var trips int
			for _, v := range plan.Vehicles {
				km += got[v].TotalKm
				trips += got[v].Trips
			}
			Expect(trips).To(Equal(plan.Trips))
			Expect(km).To(BeNumerically("~", float64(plan.Trips)*poolKm, 0.0001))
		})

		// The remainder is spread, not dropped. Ten vehicles into 9 990
		// trips divides cleanly today; a vehicle count that did not
		// would otherwise lose trips here in silence.
		It("spreads the trips evenly, to within one", func() {
			got, _, _ := fold()
			low, high := got[plan.Vehicles[0]].Trips, got[plan.Vehicles[0]].Trips
			for _, v := range plan.Vehicles {
				if n := got[v].Trips; n < low {
					low = n
				} else if n > high {
					high = n
				}
			}
			Expect(high - low).To(BeNumerically("<=", 1))
		})
	})
})
