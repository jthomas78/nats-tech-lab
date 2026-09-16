package main

// The kill clock is how 04.7.5 gets its number.
//
// `-kill-at` stops a worker while it is holding an event. Nothing anywhere
// reports that: the server finds out only when AckWait expires, and the log
// line for the redelivery has no idea how long the wait was. The clock is the
// one thing in the process that remembers when the silence started.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the kill clock", func() {

	Context("a sequence nobody killed", func() {
		It("reports no wait, rather than a wait of zero", func() {
			c := newKillClock()
			_, ok := c.since(94, time.Now())
			Expect(ok).To(BeFalse())
		})
	})

	Context("a sequence that was killed", func() {
		It("measures from the kill to the redelivery", func() {
			c := newKillClock()
			at := time.Now()
			c.killed(94, at)

			waited, ok := c.since(94, at.Add(31*time.Second))
			Expect(ok).To(BeTrue())
			Expect(waited).To(Equal(31 * time.Second))
		})

		It("keeps the FIRST kill, because a later one is a different silence", func() {
			c := newKillClock()
			at := time.Now()
			c.killed(94, at)
			c.killed(94, at.Add(10*time.Second))

			waited, _ := c.since(94, at.Add(30*time.Second))
			Expect(waited).To(Equal(30 * time.Second))
		})

		It("answers for that sequence only", func() {
			c := newKillClock()
			c.killed(94, time.Now())
			_, ok := c.since(95, time.Now())
			Expect(ok).To(BeFalse())
		})
	})
})

// MaxAckPending is one number on the CONSUMER, and the whole starvation
// lesson is that it does not divide up among the workers. Eight workers on a
// cap of three is still three messages in flight.
//
// Saying that is easy. Proving it needs the pool to report where the work
// landed, and until 04.7.6 it reported only totals — which look identical
// whether one worker did everything or eight shared it evenly.
var _ = Describe("how the work landed across the workers", func() {
	state := func(worker, acked int) *WorkerState {
		return &WorkerState{Worker: worker, Acked: acked}
	}

	It("calls a worker that acked nothing idle", func() {
		s := shareOf([]*WorkerState{state(1, 10), state(2, 0), state(3, 0)})

		Expect(s.Workers).To(Equal(3))
		Expect(s.Busy).To(Equal(1))
		Expect(s.Idle).To(Equal(2))
	})

	It("counts every worker exactly once", func() {
		s := shareOf([]*WorkerState{state(1, 5), state(2, 0), state(3, 7), state(4, 1)})

		Expect(s.Busy + s.Idle).To(Equal(s.Workers))
	})

	It("keeps the workers in their own order, so worker 1 is first", func() {
		s := shareOf([]*WorkerState{state(1, 5), state(2, 0), state(3, 7)})

		Expect(s.Acked).To(Equal([]int{5, 0, 7}))
	})

	// The question this answers is "did this worker do any work", and a
	// killed worker that never acked did not. Conflating it with a starved
	// one would be wrong on the Redelivery tab, which is why that tab reads
	// status instead — but a drain run has no kill in it.
	It("judges on acks alone, not on status", func() {
		killed := &WorkerState{Worker: 2, Acked: 0, Status: "killed"}
		s := shareOf([]*WorkerState{state(1, 5), killed})

		Expect(s.Idle).To(Equal(1))
	})

	It("says nothing rather than something about an empty pool", func() {
		s := shareOf(nil)

		Expect(s.Workers).To(BeZero())
		Expect(s.Busy).To(BeZero())
		Expect(s.Acked).To(BeEmpty())
	})
})

// Which message the kill lands on (04.9.8). Found by clicking, not by a spec.
//
// `-kill-at` used to name a STREAM SEQUENCE. A re-seed does not reset those:
// `cqrs pool -seed 10000` deletes the old messages and the new ones start
// where the old ones stopped, so after a few seeds the log ran from 490 001
// to 500 000 and `-kill-at 94` named a message that no longer existed. The
// Redelivery lesson then injected NO fault at all and still reported a clean
// run — the worst kind of broken, because it looks like it worked.
//
// So the number counts the messages of THIS RUN instead. 1 is the first
// message any worker fetches, whatever the stream calls it.
var _ = Describe("the kill switch", func() {

	Context("no kill asked for", func() {
		It("never fires, however many messages go past", func() {
			k := newKillSwitch(0)
			for i := 0; i < 500; i++ {
				Expect(k.fires(true)).To(BeFalse())
			}
		})
	})

	Context("a kill asked for", func() {
		It("fires on that message of the run and on no other", func() {
			k := newKillSwitch(3)
			Expect(k.fires(true)).To(BeFalse())
			Expect(k.fires(true)).To(BeFalse())
			Expect(k.fires(true)).To(BeTrue())
			Expect(k.fires(true)).To(BeFalse())
		})

		// Two inputs, because a switch that always fired on the third
		// message would pass the spec above.
		It("counts to the number it was given", func() {
			k := newKillSwitch(1)
			Expect(k.fires(true)).To(BeTrue())
		})

		// A redelivery is the thing the kill CAUSES. Counting it would
		// move the target while the run is under way.
		It("does not count a redelivery", func() {
			k := newKillSwitch(2)
			Expect(k.fires(false)).To(BeFalse())
			Expect(k.fires(false)).To(BeFalse())
			Expect(k.fires(true)).To(BeFalse())
			Expect(k.fires(true)).To(BeTrue())
		})
	})
})
