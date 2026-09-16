package main

// 04.8.5 — what `cqrs pool` does when you type it.
//
// One subcommand, four jobs: report, seed, remove, run. Which one happens is
// decided by WHICH FLAGS WERE TYPED, not by their values -- `-workers 4` is
// the default and still means "run", because a reader who typed it asked for
// a run.
//
// That distinction is the whole reason this is a function with specs instead
// of an if-chain in main.go. Comparing a flag against its default cannot tell
// "-workers 4" from "-workers was never mentioned", and the bare command must
// start nothing: a reader asking what the log holds has not asked to fold it.
//
// The dangerous pairs get refused rather than resolved. `-seed -rm` has no
// sensible reading, and guessing one would delete a log the reader was in the
// middle of building.

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("what `cqrs pool` was asked to do", func() {
	// set() spells a command line the way flag.FlagSet.Visit reports it:
	// the names that were actually typed.
	set := func(names ...string) map[string]bool {
		out := map[string]bool{}
		for _, n := range names {
			out[n] = true
		}
		return out
	}
	act := func(names ...string) poolAction {
		a, err := poolActionFor(set(names...))
		Expect(err).NotTo(HaveOccurred())
		return a
	}

	Context("with no flags at all", func() {
		// The one that matters. `cqrs pool` on its own is a question,
		// and a question must not create a consumer, fold an event or
		// write a key.
		It("reports, and does nothing else", func() {
			Expect(act()).To(Equal(poolReport))
		})

		It("still reports when only -url was typed", func() {
			Expect(act("url")).To(Equal(poolReport))
		})
	})

	Context("with a run flag", func() {
		// Each one alone. Any of them means the reader wants a run,
		// with today's defaults for whichever they left out (D12).
		for _, f := range []string{"workers", "max-pending", "ack-wait", "kill-at", "drain"} {
			flagName := f
			It("runs the pool for -"+flagName, func() {
				Expect(act(flagName)).To(Equal(poolRun))
			})
		}

		It("runs the pool for several of them together", func() {
			Expect(act("workers", "drain", "max-pending")).To(Equal(poolRun))
		})

		It("lists every run flag it knows, and no others", func() {
			Expect(poolRunFlags()).To(ConsistOf("workers", "max-pending", "ack-wait", "kill-at", "drain"))
		})
	})

	Context("with -seed or -rm", func() {
		It("seeds", func() {
			Expect(act("seed")).To(Equal(poolSeed))
		})

		It("removes", func() {
			Expect(act("rm")).To(Equal(poolRemove))
		})

		// They run no workers. A seed that also started a pool would
		// have the pool folding a log that was still being written,
		// and the run would measure the seed.
		It("refuses a seed that also asks for a run", func() {
			_, err := poolActionFor(set("seed", "workers"))
			Expect(err).To(MatchError(ErrPoolFlagClash))
		})

		It("refuses a removal that also asks for a run", func() {
			_, err := poolActionFor(set("rm", "drain"))
			Expect(err).To(MatchError(ErrPoolFlagClash))
		})

		// No sensible reading. Guessing one would delete a log the
		// reader was in the middle of building.
		It("refuses -seed and -rm together", func() {
			_, err := poolActionFor(set("seed", "rm"))
			Expect(err).To(MatchError(ErrPoolFlagClash))
			Expect(err.Error()).To(ContainSubstring("seed"))
			Expect(err.Error()).To(ContainSubstring("rm"))
		})
	})

	// The standing rule the user set 2026-09-16. A length nobody can price
	// is how 100 000 000 events sounds reasonable until you learn it is
	// 7.6 GB.
	Context("what the report prints", func() {
		st := PoolState{
			Stream: Pool.Stream, Subject: Pool.StreamSubject, TruthKV: PoolTruthKV,
			Exists: true, Events: 10_000, Bytes: 810_120, Sizes: PoolSizes,
			Vehicles: PoolVehicles,
		}

		It("never prints a count without its bytes", func() {
			out := poolReportLines(st)
			joined := ""
			for _, l := range out {
				joined += l + "\n"
			}
			Expect(joined).To(ContainSubstring("10000"))
			Expect(joined).To(ContainSubstring(humanBytes(st.Bytes)))
		})

		It("names the log and the bucket holding the right answer", func() {
			joined := ""
			for _, l := range poolReportLines(st) {
				joined += l + "\n"
			}
			Expect(joined).To(ContainSubstring(Pool.Stream))
			Expect(joined).To(ContainSubstring(PoolTruthKV))
		})

		// An unseeded log is a normal state, not a failure. The screen
		// and the terminal both have to be able to say "nothing yet".
		It("says the log is not there yet, and how to make it", func() {
			joined := ""
			for _, l := range poolReportLines(PoolState{Stream: Pool.Stream, Sizes: PoolSizes}) {
				joined += l + "\n"
			}
			Expect(joined).To(ContainSubstring("not seeded"))
			Expect(joined).To(ContainSubstring("-seed"))
		})
	})
})
