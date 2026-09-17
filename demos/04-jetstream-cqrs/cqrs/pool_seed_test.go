package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// jsonFields is the set of keys a value actually serialises to. Asserting on
// these rather than on Go field names is what makes a spec about the SCREEN's
// contract fail when a json tag is dropped or an omitempty is added.
func jsonFields(v any) []string {
	body, err := json.Marshal(v)
	Expect(err).ToNot(HaveOccurred())
	var m map[string]any
	Expect(json.Unmarshal(body, &m)).To(Succeed())
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Specs for lesson 02's own log (plan 04.8.2).
//
// Nothing here talks to a server. What is worth proving is the part that is a
// DECISION: how big the log is, how many vehicles it is spread over, and that
// seeding it twice leaves one log rather than two stacked on each other.

var _ = Describe("lesson 02 seeds a log of its own", func() {
	Context("the size is a fixed list, not a number a reader types", func() {
		It("offers the same three sizes the benchmark does", func() {
			// Mirrored deliberately, and kept as its own list. The two
			// fixtures answer different questions and may want to
			// diverge; sharing the variable would make that a
			// surprise rather than a choice.
			Expect(PoolSizes).To(Equal([]int{10_000, 100_000, 1_000_000}))
		})

		It("defaults to the smallest", func() {
			// 10 000 is what the starvation and drain sets are
			// priced on: about 60s for a four-run set. 100 000 would
			// be ten minutes of a reader watching a bar.
			Expect(DefaultPoolSize).To(Equal(10_000))
			Expect(PoolSizes[0]).To(Equal(DefaultPoolSize))
		})

		It("refuses a size that is not on the list, by name", func() {
			_, err := poolPlanFor(50_000)
			Expect(err).To(MatchError(ErrUnknownPoolSize))
			Expect(err.Error()).To(ContainSubstring("50000"))
		})
	})

	Context("the log is spread over several vehicles", func() {
		// One vehicle would make the odometer-pool tab a single row,
		// and that tab exists to show WHICH vehicle lost kilometres.
		It("uses three, named so `nats kv ls` sorts them", func() {
			Expect(PoolVehicles).To(HaveLen(3))
			Expect(PoolVehicles[0]).To(Equal("pool-01"))
			Expect(PoolVehicles[2]).To(Equal("pool-03"))
			for _, v := range PoolVehicles {
				Expect(v).To(HavePrefix("pool-"))
				Expect(v).To(Equal(strings.ToLower(v)))
			}
		})

		// 04.10.1 (D14). This spec asserted the OPPOSITE until today --
		// "more vehicles than the biggest worker count" -- on the
		// reasoning that a worker needs a vehicle of its own to be
		// wrong about. That reasoning is backwards. A worker alone on a
		// vehicle folds it in perfect order. The damage IS two workers
		// on one vehicle, so the lesson needs fewer vehicles than
		// workers, not more.
		It("has fewer vehicles than the biggest worker count the lesson runs", func() {
			Expect(len(PoolVehicles)).To(BeNumerically("<", 8))
		})

		// Still a table, not a single row.
		It("keeps enough vehicles for the drift table to be a table", func() {
			Expect(len(PoolVehicles)).To(BeNumerically(">", 1))
		})

		// The gap is the whole mechanism (D13). poolEvents writes round
		// robin, so two events of one vehicle sit len(PoolVehicles)
		// apart. A gap wider than the worker count means the first
		// event is folded and acked before any worker reaches the
		// second, and the fold never sees them out of order.
		It("puts two events of one vehicle closer together than the workers can absorb", func() {
			plan, err := poolPlanFor(DefaultPoolSize)
			Expect(err).ToNot(HaveOccurred())

			events := poolEvents(plan)
			first := -1
			gap := -1
			for i, e := range events[len(plan.Vehicles):] {
				if e.Vehicle != plan.Vehicles[0] {
					continue
				}
				if first < 0 {
					first = i
					continue
				}
				gap = i - first
				break
			}
			Expect(gap).To(Equal(len(plan.Vehicles)))
			Expect(gap).To(BeNumerically("<", 4))
		})
	})

	Context("a plan says exactly what will be written", func() {
		It("counts the registrations as part of the size", func() {
			// The number on the button is the LENGTH OF THE LOG, so a
			// reader comparing two runs is comparing what the screen
			// says. Same rule as the benchmark.
			p, err := poolPlanFor(10_000)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Size).To(Equal(10_000))
			Expect(p.Events).To(Equal(10_000))
			Expect(p.Vehicles).To(Equal(PoolVehicles))
			Expect(p.Trips).To(Equal(10_000 - len(PoolVehicles)))
		})

		It("spreads the trips evenly, so no vehicle is the whole log", func() {
			p, err := poolPlanFor(10_000)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.TripsPer).To(Equal(p.Trips / len(PoolVehicles)))
			// Registrations plus the trips actually written must equal
			// the size. An uneven division that silently dropped the
			// remainder would make a "10 000 events" log hold 9 994.
			Expect(len(p.Vehicles) + p.TripsPer*len(p.Vehicles) + p.Extra).To(Equal(p.Events))
			Expect(p.Extra).To(BeNumerically("<", len(p.Vehicles)))
		})

		It("plans every size on the list", func() {
			for _, n := range PoolSizes {
				p, err := poolPlanFor(n)
				Expect(err).ToNot(HaveOccurred(), "size %d", n)
				Expect(p.Events).To(Equal(n), "size %d", n)
			}
		})
	})

	Context("seeding replaces, it never appends", func() {
		// The property that makes two sessions comparable. Proved here
		// as the decision it is -- the purge itself is one client call,
		// and a spec that mocked it would only prove the mock.
		It("purges the whole stream, not one vehicle", func() {
			// The benchmark purges ONE vehicle, because three
			// fixtures share ODOMETER_BENCH and seeding one must not
			// delete the others. This log has one tenant, so a
			// per-vehicle purge would leave a vehicle behind whenever
			// the vehicle list changed.
			Expect(poolPurgeFilter()).To(Equal(Pool.StreamSubject))
		})
	})

	Context("what the screen is told", func() {
		It("never reports a count without its bytes", func() {
			// The standing rule, set 2026-09-16. Proved on the type,
			// so a field cannot be dropped without this failing.
			s := PoolState{Stream: Pool.Stream, Events: 10_000, Bytes: 830_000}
			Expect(s.Events).To(Equal(10_000))
			Expect(s.Bytes).To(BeNumerically(">", 0))
			Expect(jsonFields(s)).To(ContainElements("events", "bytes"))
		})

		It("says which stream it is talking about", func() {
			Expect(jsonFields(PoolState{})).To(ContainElements("stream", "subject", "sizes"))
		})
	})

	Context("the errors are named, not matched on text", func() {
		It("lets the HTTP shim answer 400 without reading the message", func() {
			_, err := poolPlanFor(1)
			Expect(errors.Is(err, ErrUnknownPoolSize)).To(BeTrue())
		})
	})
})
