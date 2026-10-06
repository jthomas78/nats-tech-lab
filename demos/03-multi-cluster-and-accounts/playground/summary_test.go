package main

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the meta summary (D03-R25)", func() {
	var baseline, watch [][]metaReading

	BeforeEach(func() {
		baseline = fixtureRounds("ML51-baseline.jsonl")
		watch = fixtureRounds("ML51-watch.jsonl")
	})

	running := func(n int) []procState {
		out := make([]procState, n)
		for i := range out {
			out[i] = procRunning
		}
		return out
	}

	Context("on exercise 10's kept ML51 readings", func() {
		It("agrees on t-arb-1, term 15, with za dark and six fresh readings", func() {
			round := baseline[0]
			procs := append([]procState{procStopped, procStopped, procStopped}, running(6)...)
			s := summarize(storeOf(round), procs, nil, roundTime(round))

			Expect(s.State).To(Equal(stateAgreed))
			Expect(s.Leader).To(Equal("t-arb-1"))
			Expect(s.Term).To(Equal(15))
			Expect(s.Text).To(Equal("t-arb-1, term 15, named by 6 fresh readings"))
			Expect(s.Reading).To(Equal(readingCounts{Fresh: 6, NeverSeen: 3}))
			Expect(s.Procs).To(Equal(procCounts{Running: 6, Stopped: 3}))
			Expect(s.Majority).To(Equal(5))
			// The agreement is as old as its newest supporting reading — not
			// the poll's end, which waits on za's 1 s timeouts.
			var support time.Time
			for _, r := range round {
				if r.Kind == kindOK && r.At.After(support) {
					support = r.At
				}
			}
			Expect(s.Last).To(Equal(&lastAgreement{Leader: "t-arb-1", Term: 15, At: support}))
		})

		It("reports the first poll after the thaw as a disagreement, not a no-leader", func() {
			round := watch[0]
			s := summarize(storeOf(round), running(9), nil, roundTime(round))

			Expect(s.State).To(Equal(stateDisagreement))
			Expect(s.Views).To(Equal([]view{
				{Leader: "t-arb-1", Term: 15, Servers: []string{"t-arb-1", "t-arb-2", "t-arb-3", "t-au-1", "t-au-2", "t-au-3"}},
				{Leader: "", Term: 0, Servers: []string{"t-za-1", "t-za-2", "t-za-3"}},
			}))
			Expect(s.Text).To(Equal("fresh readings disagree: t-arb-1 term 15 (6) vs no leader (3)"))
		})

		It("does not agree when five readings name a leader that itself says no leader", func() {
			// Round 1: au and two arb servers still name t-arb-1 at term 16,
			// while t-arb-1 itself and za report no leader.
			round := watch[1]
			s := summarize(storeOf(round), running(9), nil, roundTime(round))

			Expect(s.State).To(Equal(stateDisagreement))
			Expect(s.Views[0]).To(Equal(view{Leader: "t-arb-1", Term: 16,
				Servers: []string{"t-arb-2", "t-arb-3", "t-au-1", "t-au-2", "t-au-3"}}))
			Expect(s.Views[1].Servers).To(ConsistOf("t-arb-1", "t-za-1", "t-za-2", "t-za-3"))
		})

		It("agrees on t-au-3, term 17, two polls after the thaw", func() {
			round := watch[2]
			s := summarize(storeOf(round), running(9), nil, roundTime(round))

			Expect(s.State).To(Equal(stateAgreed))
			Expect(s.Leader).To(Equal("t-au-3"))
			Expect(s.Term).To(Equal(17))
			Expect(s.Reading.Fresh).To(Equal(9))
		})
	})

	Context("when the named leader has no fresh word of its own", func() {
		It("is insufficient when the leader did not answer", func() {
			// Round 1 with t-arb-1 and za silent: five fresh readings, all
			// naming t-arb-1 term 16 — but nothing from t-arb-1.
			var obs []serverObs
			for _, o := range storeOf(watch[1]) {
				if o.Server == "t-arb-1" || clusterOf(o.Server) == clusterZA {
					obs = append(obs, serverObs{Server: o.Server, AttemptFailed: true})
					continue
				}
				obs = append(obs, o)
			}
			s := summarize(obs, running(9), nil, roundTime(watch[1]))

			Expect(s.State).To(Equal(stateInsufficient))
			Expect(s.Text).To(Equal("5 fresh readings name t-arb-1, term 16, but t-arb-1 has no fresh answer of its own"))
		})

		It("is insufficient when the leader answers but not as LEADER", func() {
			round := append([]metaReading(nil), baseline[0]...)
			for i := range round {
				if round[i].Server == "t-arb-1" {
					round[i].State = "FOLLOWER"
				}
			}
			s := summarize(storeOf(round), running(9), nil, roundTime(round))

			Expect(s.State).To(Equal(stateInsufficient))
			Expect(s.Text).To(Equal("6 fresh readings name t-arb-1, term 15, but t-arb-1 itself says FOLLOWER"))
		})
	})

	Context("missing or stale readings", func() {
		It("goes stale, not empty, when every answer ages past 1.5 s", func() {
			round := baseline[0]
			first := summarize(storeOf(round), running(9), nil, roundTime(round))
			later := first.Last.At.Add(4200 * time.Millisecond)

			s := summarize(storeOf(round), running(9), first.Last, later)

			Expect(s.State).To(Equal(stateStale))
			Expect(s.Text).To(Equal("last agreed t-arb-1, term 15, 4.2 s ago"))
			Expect(s.Reading).To(Equal(readingCounts{Stale: 6, NeverSeen: 3}))
			Expect(s.Last).To(Equal(first.Last))
		})

		It("counts a server whose newest poll failed as no answer, not as stale", func() {
			round := baseline[0]
			obs := storeOf(round)
			for i := range obs {
				obs[i].AttemptFailed = true
			}
			s := summarize(obs, running(9), nil, roundTime(round).Add(2*time.Second))

			Expect(s.Reading).To(Equal(readingCounts{NoAnswer: 6, NeverSeen: 3}))
		})

		It("is insufficient with fewer than five fresh readings and no last agreement", func() {
			obs := storeOf(baseline[0])
			for i := range obs {
				if obs[i].Server == "t-au-1" || obs[i].Server == "t-au-2" {
					obs[i] = serverObs{Server: obs[i].Server, AttemptFailed: true}
				}
			}
			s := summarize(obs, running(9), nil, roundTime(baseline[0]))

			Expect(s.State).To(Equal(stateInsufficient))
			Expect(s.Text).To(Equal("4 fresh readings; an agreement needs 5"))
		})

		It("is insufficient, not stale, while the last agreement is still under 1.5 s old", func() {
			round := baseline[0]
			first := summarize(storeOf(round), running(9), nil, roundTime(round))
			obs := storeOf(round)
			for i := range obs {
				if obs[i].Server == "t-au-1" || obs[i].Server == "t-au-2" {
					obs[i] = serverObs{Server: obs[i].Server, AttemptFailed: true}
				}
			}
			s := summarize(obs, running(9), first.Last, first.Last.At.Add(time.Second))

			Expect(s.State).To(Equal(stateInsufficient))
		})

		It("never counts an invalid answer as evidence", func() {
			round := append([]metaReading(nil), baseline[0]...)
			for i := range round {
				if round[i].Server == "t-au-1" || round[i].Server == "t-au-2" {
					round[i].Kind, round[i].Leader, round[i].State = kindInvalid, "", ""
				}
			}
			s := summarize(storeOf(round), running(9), nil, roundTime(round))

			Expect(s.State).To(Equal(stateInsufficient))
			Expect(s.Reading).To(Equal(readingCounts{Fresh: 4, Invalid: 2, NeverSeen: 3}))
		})
	})

	It("agrees on no leader when five or more fresh readings all say so, whatever their terms", func() {
		now := time.Unix(1_800_000_000, 0)
		var obs []serverObs
		for i, s := range servers[:5] {
			r := metaReading{Server: s.Name, At: now, Kind: kindNoLeader, Term: 16 + i%2, State: "CANDIDATE"}
			obs = append(obs, serverObs{Server: s.Name, Answer: &r})
		}
		s := summarize(obs, running(9), nil, now)

		Expect(s.State).To(Equal(stateAgreedNone))
		Expect(s.Text).To(Equal("no server reports a leader"))
		Expect(s.Last).To(BeNil())
	})

	It("never speaks of available voters (rule 3)", func() {
		for _, round := range append(baseline, watch...) {
			s := summarize(storeOf(round), running(9), nil, roundTime(round))
			Expect(s.Text).NotTo(ContainSubstring("voter"))
		}
	})
})
