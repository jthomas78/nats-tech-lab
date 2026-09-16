package main

// 04.8.4 — `cqrs pool -rm` takes lesson 02 back to nothing.
//
// Lesson 02 is the one part of this demo a reader can make a mess of on
// purpose. The way to undo that has to be as easy to find as the way to do
// it, and it has to be COMPLETE: a removal that left the durable consumer
// behind would have the next seed fold nothing and report success.
//
// The dangerous half is what it must NOT touch. `pool -rm` runs against a
// server that also holds ODOMETER, odometer-write and odometer-read, and
// those are the demo. A name in the drop list that belonged to the live side
// would delete the whole demo and print "dropped".
//
// These specs are about the LIST, not the deleting. jetstream.DeleteStream is
// not this demo's code and does not need a spec; which names are handed to it
// very much does.

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("removing lesson 02", func() {
	plan := poolRemovalPlanFor()

	Context("what it drops", func() {
		It("drops lesson 02's log, and no other stream", func() {
			Expect(plan.Streams).To(Equal([]string{Pool.Stream}))
		})

		// Both. The pool's own consumer is the deliberately broken one;
		// the truth consumer is the right answer beside it. Leaving
		// either behind leaves it parked at the end of a log that is
		// gone.
		It("drops both of lesson 02's consumers", func() {
			Expect(plan.Consumers).To(ConsistOf(PoolConsumer, PoolTruthConsumer))
			Expect(plan.Consumers).To(HaveLen(2))
		})

		// Three, not two. odometer-pool is the wrong fold,
		// odometer-pool-workers is what the screen watches, and
		// odometer-pool-truth is the right answer. A leftover bucket
		// would draw a screen full of yesterday's run.
		It("drops all three of lesson 02's buckets", func() {
			Expect(plan.Buckets).To(ConsistOf(PoolKV, PoolWorkersKV, PoolTruthKV))
			Expect(plan.Buckets).To(HaveLen(3))
		})

		It("looks for its consumers on lesson 02's log", func() {
			Expect(plan.ConsumerStream).To(Equal(Pool.Stream))
		})
	})

	// The spec this task exists for. Everything above is a list; this is
	// the promise that the list is safe to run.
	Context("what it must never touch", func() {
		live := []string{StreamName, WriteKV, ReadKV, SnapshotConsumer, ReadConsumer}

		It("names nothing that belongs to the demo itself", func() {
			all := append(append(append([]string{}, plan.Streams...), plan.Consumers...), plan.Buckets...)
			for _, name := range all {
				Expect(live).NotTo(ContainElement(name))
			}
		})

		It("names nothing that belongs to the rehydrate fixture", func() {
			all := append(append(append([]string{}, plan.Streams...), plan.Consumers...), plan.Buckets...)
			Expect(all).NotTo(ContainElement(Bench.Stream))
			Expect(all).NotTo(ContainElement(Bench.WriteKV))
		})

		// ODOMETER is one character away from ODOMETER_POOL and a
		// prefix of it. A drop written with a prefix match instead of
		// an exact name would take the demo with it.
		It("drops a name that only looks like the demo's", func() {
			Expect(plan.Streams[0]).To(HavePrefix(StreamName))
			Expect(plan.Streams[0]).NotTo(Equal(StreamName))
		})
	})

	// Idempotent means a second `-rm` is a no-op, not an error. A reader
	// who presses Delete twice has done nothing wrong.
	Context("running it twice", func() {
		It("treats every already-gone as done, not as failure", func() {
			for _, err := range poolAlreadyGone() {
				Expect(poolDropTolerates(err)).To(BeTrue())
			}
		})

		It("still reports a real failure", func() {
			Expect(poolDropTolerates(ErrUnknownPoolSize)).To(BeFalse())
			Expect(poolDropTolerates(nil)).To(BeFalse())
		})
	})
})
