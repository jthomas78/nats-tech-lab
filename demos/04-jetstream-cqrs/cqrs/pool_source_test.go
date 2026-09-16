package main

// 04.8.6 — the pool binds to its OWN log.
//
// This is the defect the whole of 04.8 exists to remove, so it gets specs of
// its own rather than being assumed once the names exist.
//
// Until now runPool spelled StreamName and StreamSubject into itself. It
// published nothing to ODOMETER, which is why nobody noticed -- what it
// shared was a CONSUMER. But that consumer is deliberately broken: it
// reorders, it starves and it drops, and it is the point of the lesson that
// it does. Pointing it at the log every other screen is drawn from was one
// press away from a reader who had not read the warning.
//
// The fix is the one 04.7.16 already made to rehydrate(): take a Source, do
// not name a stream. These specs hold the config to that -- it must be built
// FROM the source, which is provable by handing it two different sources and
// getting two different answers. A function that returned Pool's names
// whatever it was given would pass a spec that only ever tried Pool.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("which log the pool folds", func() {
	cfg := PoolConfig{Workers: 8, MaxPending: 64, AckWait: 7 * time.Second}

	Context("bound to lesson 02's log", func() {
		c := poolConsumerConfig(Pool, cfg)

		It("filters on lesson 02's subjects", func() {
			Expect(c.FilterSubject).To(Equal(Pool.StreamSubject))
		})

		// The whole phase in one assertion.
		It("never filters on the demo's own log", func() {
			Expect(c.FilterSubject).NotTo(Equal(Live.StreamSubject))
			Expect(poolStreamFor(Pool)).NotTo(Equal(Live.Stream))
		})

		It("hangs off lesson 02's stream", func() {
			Expect(poolStreamFor(Pool)).To(Equal(Pool.Stream))
		})
	})

	// Handed a different source it must answer differently. This is what
	// separates "parameterised" from "hardcoded to the right answer".
	Context("handed a different source", func() {
		It("follows the source it was given, not a constant", func() {
			live := poolConsumerConfig(Live, cfg)
			pool := poolConsumerConfig(Pool, cfg)
			Expect(live.FilterSubject).To(Equal(Live.StreamSubject))
			Expect(live.FilterSubject).NotTo(Equal(pool.FilterSubject))
			Expect(poolStreamFor(Live)).To(Equal(Live.Stream))
			Expect(poolStreamFor(Live)).NotTo(Equal(poolStreamFor(Pool)))
		})
	})

	Context("what the reader asked for is passed through", func() {
		c := poolConsumerConfig(Pool, cfg)

		// MaxAckPending is the lesson. It is ONE number on the consumer,
		// shared by every worker, and it must arrive at the server
		// exactly as typed -- a cap the pool quietly adjusted would make
		// the starvation tab a chart of this function's opinions.
		It("passes the cap through untouched", func() {
			Expect(c.MaxAckPending).To(Equal(cfg.MaxPending))
		})

		It("passes the ack wait through untouched", func() {
			Expect(c.AckWait).To(Equal(cfg.AckWait))
		})

		It("is the pool's durable, and not the correct fold's", func() {
			Expect(c.Durable).To(Equal(PoolConsumer))
			Expect(c.Durable).NotTo(Equal(PoolTruthConsumer))
		})

		// Every run replays the whole log. A drain that started where
		// the last one stopped would report a speed-up that was really
		// an empty queue.
		It("replays from the start of the log", func() {
			Expect(c.DeliverPolicy).To(Equal(poolTruthConsumerConfig().DeliverPolicy))
		})
	})

	// The pool and the correct fold read the SAME log and must differ in
	// exactly one way: the cap. If they ever agreed on that too, lesson 02
	// would be comparing a number against itself.
	Context("beside the correct fold", func() {
		It("reads the same log", func() {
			Expect(poolStreamFor(Pool)).To(Equal(poolTruthStream()))
			Expect(poolConsumerConfig(Pool, cfg).FilterSubject).
				To(Equal(poolTruthConsumerConfig().FilterSubject))
		})

		It("is a different consumer with a different cap", func() {
			Expect(poolConsumerConfig(Pool, cfg).Durable).
				NotTo(Equal(poolTruthConsumerConfig().Durable))
			Expect(poolConsumerConfig(Pool, cfg).MaxAckPending).
				NotTo(Equal(poolTruthConsumerConfig().MaxAckPending))
		})
	})
})
