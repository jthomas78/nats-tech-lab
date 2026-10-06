package main

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the transition timer (D03-R29)", func() {
	t0 := time.Unix(1_800_000_000, 0)
	arb15 := &lastAgreement{Leader: "t-arb-1", Term: 15, At: t0.Add(-time.Second)}

	agreedOn := func(leader string, term int, at time.Time) summary {
		return summary{At: at, State: stateAgreed, Leader: leader, Term: term}
	}
	inState := func(st summaryState, at time.Time) summary {
		return summary{At: at, State: st}
	}

	It("times exercise 10's ML51 za resume to the poll that agreed on t-au-3", func() {
		baseline := fixtureRounds("ML51-baseline.jsonl")
		watch := fixtureRounds("ML51-watch.jsonl")
		thaw := fixtureThaw()

		var procs []procState
		for range servers {
			procs = append(procs, procRunning)
		}
		before := summarize(storeOf(baseline[1]), procs, nil, roundTime(baseline[1]))
		Expect(before.State).To(Equal(stateAgreed))

		var l transitionLog
		l.start(change{Kind: changeResume, Cluster: clusterZA, At: thaw}, before.Last)
		var agreedAt time.Time
		for _, round := range watch {
			s := summarize(storeOf(round), procs, before.Last, roundTime(round))
			l.observe(s)
			if l.Open == nil {
				agreedAt = s.At
				break
			}
		}

		Expect(l.Open).To(BeNil())
		Expect(l.Closed).To(HaveLen(1))
		got := l.Closed[0]
		Expect(got.Outcome).To(Equal(outcomeAgreed))
		Expect(got.Before.Leader).To(Equal("t-arb-1"))
		Expect(got.Before.Term).To(Equal(15))
		Expect(got.After.Leader).To(Equal("t-au-3"))
		Expect(got.After.Term).To(Equal(17))
		Expect(got.Delta).To(Equal("term +2, leader moved"))
		Expect(agreedAt).To(Equal(roundTime(watch[2])))
		Expect(got.Elapsed).To(Equal(roundTime(watch[2]).Sub(thaw)))
		Expect(got.LeftAgreed).To(BeTrue())
	})

	It("closes on a return to the same leader and term, once it has left agreement", func() {
		var l transitionLog
		l.start(change{Kind: changeFreeze, Cluster: clusterAU, At: t0}, arb15)
		l.observe(agreedOn("t-arb-1", 15, t0.Add(500*time.Millisecond)))
		Expect(l.Open).NotTo(BeNil(), "still agreed on the same leader: nothing changed yet")

		l.observe(inState(stateDisagreement, t0.Add(time.Second)))
		l.observe(agreedOn("t-arb-1", 15, t0.Add(1900*time.Millisecond)))

		Expect(l.Open).To(BeNil())
		Expect(l.Closed[0].Text).To(Equal("agreed after 1.9 s"))
		Expect(l.Closed[0].Delta).To(Equal("unchanged"))
	})

	It("closes on an agreement that names a new term, even without leaving agreement", func() {
		var l transitionLog
		l.start(change{Kind: changeLeadership, Cluster: clusterAU, At: t0}, arb15)
		l.observe(agreedOn("t-au-2", 16, t0.Add(700*time.Millisecond)))

		Expect(l.Closed[0].Outcome).To(Equal(outcomeAgreed))
		Expect(l.Closed[0].Delta).To(Equal("term +1, leader moved"))
	})

	It("closes as no change after 15 s of the same agreement", func() {
		var l transitionLog
		l.start(change{Kind: changeFreeze, Cluster: clusterAU, At: t0}, arb15)
		for ms := 500; ms < 15000; ms += 500 {
			l.observe(agreedOn("t-arb-1", 15, t0.Add(time.Duration(ms)*time.Millisecond)))
		}
		Expect(l.Open).NotTo(BeNil())

		l.observe(agreedOn("t-arb-1", 15, t0.Add(15*time.Second)))
		Expect(l.Open).To(BeNil())
		Expect(l.Closed[0].Outcome).To(Equal(outcomeNoChange))
		Expect(l.Closed[0].Text).To(Equal("no change in 15 s"))
		Expect(l.Closed[0].After).To(BeNil())
	})

	It("closes as no agreement after 15 s without one", func() {
		var l transitionLog
		l.start(change{Kind: changeFreeze, Cluster: clusterZA, At: t0}, arb15)
		l.observe(inState(stateDisagreement, t0.Add(time.Second)))
		l.observe(inState(stateStale, t0.Add(15*time.Second)))

		Expect(l.Closed[0].Outcome).To(Equal(outcomeNoAgreement))
		Expect(l.Closed[0].Text).To(Equal("no agreement in 15 s"))
	})

	It("closes the open transition as superseded when the next change comes first", func() {
		var l transitionLog
		l.start(change{Kind: changeFreeze, Cluster: clusterZA, At: t0}, arb15)
		l.start(change{Kind: changeResume, Cluster: clusterZA, At: t0.Add(3 * time.Second)}, arb15)

		Expect(l.Open.ID).To(Equal(2))
		Expect(l.Closed[0].ID).To(Equal(1))
		Expect(l.Closed[0].Outcome).To(Equal(outcomeSuperseded))
		Expect(l.Closed[0].Text).To(Equal("next change came first"))
		Expect(l.Closed[0].Elapsed).To(Equal(3 * time.Second))
	})

	It("ignores a summary taken before the change", func() {
		var l transitionLog
		l.start(change{Kind: changeFreeze, Cluster: clusterZA, At: t0}, arb15)
		l.observe(inState(stateDisagreement, t0.Add(-100*time.Millisecond)))

		Expect(l.Open.LeftAgreed).To(BeFalse())
	})

	It("closes on the first agreement when there was none before", func() {
		var l transitionLog
		l.start(change{Kind: changeResume, Cluster: clusterZA, At: t0}, nil)
		l.observe(agreedOn("t-arb-1", 15, t0.Add(time.Second)))

		Expect(l.Closed[0].Outcome).To(Equal(outcomeAgreed))
		Expect(l.Closed[0].Delta).To(Equal("no agreement before"))
	})

	It("keeps the five newest closed transitions, newest first", func() {
		var l transitionLog
		for i := 0; i < 7; i++ {
			l.start(change{Kind: changeFreeze, Cluster: clusterZA, At: t0.Add(time.Duration(i) * time.Second)}, arb15)
		}
		l.observe(agreedOn("t-au-1", 16, t0.Add(10*time.Second)))

		Expect(l.Closed).To(HaveLen(5))
		ids := []int{}
		for _, c := range l.Closed {
			ids = append(ids, c.ID)
		}
		Expect(ids).To(Equal([]int{7, 6, 5, 4, 3}))
	})

	It("copies the agreement before, so a later change to the caller's value does not move it", func() {
		before := *arb15
		var l transitionLog
		l.start(change{Kind: changeFreeze, Cluster: clusterZA, At: t0}, &before)
		before.Leader = "t-za-1"

		Expect(l.Open.Before.Leader).To(Equal("t-arb-1"))
	})
})
