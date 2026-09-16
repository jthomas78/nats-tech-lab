package main

// 04.7.13 — the conflict retry waits, and the waits are not identical.
//
// These specs are about shape, not about milliseconds. A test that pinned an
// exact duration would fail on the jitter that is the point of the function.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the conflict backoff", func() {
	It("always waits, so a retry is never immediate", func() {
		for attempt := 1; attempt <= 8; attempt++ {
			Expect(conflictBackoff(attempt)).To(BeNumerically(">", 0))
		}
	})

	It("grows with the attempt number", func() {
		Expect(conflictBackoff(1)).To(BeNumerically("<=", retryBase))
		Expect(conflictBackoff(4)).To(BeNumerically(">", retryBase))
	})

	// Without this a queue of writers on one hot vehicle becomes a queue of
	// sleepers, and the command API holds a request open for seconds.
	It("never waits longer than the cap, however many attempts", func() {
		for attempt := 1; attempt <= 40; attempt++ {
			Expect(conflictBackoff(attempt)).To(BeNumerically("<=", retryCap))
		}
	})

	// The half that matters. A fixed delay wakes every loser of one race at
	// the same instant, and they collide again -- the sleep then only makes
	// the collision slower. The spread is what pulls the writers apart.
	It("gives different writers different waits", func() {
		seen := map[time.Duration]bool{}
		for i := 0; i < 200; i++ {
			seen[conflictBackoff(5)] = true
		}
		Expect(len(seen)).To(BeNumerically(">", 10))
	})

	// A shift of 40 overflows int64. The cap must catch that, not wrap.
	It("does not wrap to a negative wait on a large attempt", func() {
		Expect(conflictBackoff(64)).To(BeNumerically(">", 0))
		Expect(conflictBackoff(64)).To(BeNumerically("<=", retryCap))
	})
})
