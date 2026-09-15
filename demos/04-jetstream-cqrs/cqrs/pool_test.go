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
