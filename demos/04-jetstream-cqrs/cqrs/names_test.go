package main

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Specs for the three logs this demo keeps apart (plan 04.8.1).
//
// This file proves ONE property, and it is the property that would silently
// undo the whole of phase 04.8: no source's events land inside another
// source's filter. Get it wrong and nothing fails. The stream is created, the
// consumer binds, events flow -- and lesson 02's deliberately broken consumer
// is quietly chewing on the demo's own log again, which is the exact defect
// 04.8 exists to remove.
//
// The classic way to get it wrong is a DOT where a hyphen belongs.
// `evt.odometer.pool.>` looks like a separate subject and is not: the second
// token is still `odometer`, so `evt.odometer.>` matches every event of it.
// `evt.odometer-pool.>` is a different second token and does not overlap.

// matchesFilter reports whether a NATS subject is matched by a subject filter,
// using the two wildcards JetStream understands: `*` for exactly one token and
// `>` for one or more trailing tokens.
//
// Spelled out here rather than imported so the spec proves the rule itself,
// not that a library agrees with another library.
func matchesFilter(filter, subject string) bool {
	f := strings.Split(filter, ".")
	s := strings.Split(subject, ".")
	for i, tok := range f {
		if tok == ">" {
			return i < len(s)
		}
		if i >= len(s) {
			return false
		}
		if tok != "*" && tok != s[i] {
			return false
		}
	}
	return len(f) == len(s)
}

var _ = Describe("the three logs never overlap", func() {
	// Named so a failure says which log broke the rule.
	sources := map[string]Source{"Live": Live, "Bench": Bench, "Pool": Pool}

	Context("the pool has a log of its own", func() {
		It("names it for what it holds, not for the lesson number", func() {
			// ODOMETER_02 would name it after where it is shown. Every
			// other stream here is named after what is in it, and a
			// lesson can be renumbered.
			Expect(Pool.Stream).To(Equal("ODOMETER_POOL"))
			Expect(Pool.StreamSubject).To(Equal("evt.odometer-pool.>"))
		})

		It("keeps the correct fold beside it", func() {
			// The correct fold IS the pool log's snapshot -- one bucket,
			// one job: the right answer. `-truth` and not `-read`,
			// because "read model" is lesson 01's idea and this is not
			// one (D8).
			Expect(Pool.WriteKV).To(Equal("odometer-pool-truth"))
			Expect(PoolTruthKV).To(Equal("odometer-pool-truth"))
			Expect(PoolTruthConsumer).To(Equal("odometer-pool-truth"))
		})

		It("leaves the demo's own log exactly as it was", func() {
			Expect(Live.Stream).To(Equal("ODOMETER"))
			Expect(Live.StreamSubject).To(Equal("evt.odometer.>"))
			Expect(Live.WriteKV).To(Equal("odometer-write"))
		})
	})

	Context("no source's events fall inside another source's filter", func() {
		It("keeps every pair apart, both ways round", func() {
			for anme, a := range sources {
				for bnme, b := range sources {
					if anme == bnme {
						continue
					}
					// A real event, published the way the code
					// publishes it -- not a hand-written string
					// that could be kept true while the prefix
					// drifts.
					evt := a.VehicleSubject("truck-7", "trip")
					Expect(matchesFilter(b.StreamSubject, evt)).To(BeFalse(),
						"%s's event %q is matched by %s's filter %q", anme, evt, bnme, b.StreamSubject)
				}
			}
		})

		It("still matches its OWN filter, so the separation is not just a typo", func() {
			for nme, s := range sources {
				evt := s.VehicleSubject("truck-7", "trip")
				Expect(matchesFilter(s.StreamSubject, evt)).To(BeTrue(),
					"%s does not hold its own event %q", nme, evt)
				Expect(matchesFilter(s.VehicleFilter("truck-7"), evt)).To(BeTrue(),
					"%s's vehicle filter does not match its own event", nme)
			}
		})

		// The hyphen rule, stated directly. The spec above would catch a
		// dot, but it would report it as an overlap and leave the reader
		// to work out why.
		It("separates by a hyphen, never a dot", func() {
			for nme, s := range sources {
				second := strings.Split(s.StreamSubject, ".")[1]
				Expect(second).ToNot(ContainSubstring("*"), "%s", nme)
				Expect(strings.HasPrefix(second, "odometer")).To(BeTrue(),
					"%s's second token %q is not an odometer log", nme, second)
			}
			Expect(Bench.StreamSubject).To(HavePrefix("evt.odometer-"))
			Expect(Pool.StreamSubject).To(HavePrefix("evt.odometer-"))
		})
	})

	Context("the names follow the repo's casing", func() {
		It("shouts stream names and whispers bucket names", func() {
			for nme, s := range sources {
				Expect(s.Stream).To(Equal(strings.ToUpper(s.Stream)), "%s stream", nme)
				Expect(s.WriteKV).To(Equal(strings.ToLower(s.WriteKV)), "%s bucket", nme)
				Expect(s.WriteKV).ToNot(ContainSubstring("_"), "%s bucket", nme)
			}
		})
	})
})
