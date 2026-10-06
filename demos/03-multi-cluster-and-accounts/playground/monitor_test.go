package main

import (
	"context"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// raftzBody is a /raftz?group=_meta_ answer in the shape nats-server 2.14.6
// prints, cut to the fields parseRaftz reads.
func raftzBody(meta string) []byte {
	return []byte(`{"$SYS":{"_meta_":` + meta + `}}`)
}

const peersOfZA1 = `"peers":{"P2":{"name":"t-za-2"},"P3":{"name":"t-arb-1"}}`

var _ = Describe("the monitor (D03-R19, rules 1-3)", func() {
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	Describe("parseRaftz, rule for rule as classify-10.py parse_reading", func() {
		DescribeTable("one answer becomes one reading",
			func(status int, body string, fetchErr error, kind, leader string, term int, why string) {
				r := parseRaftz("t-za-1", t0, status, []byte(body), fetchErr)

				Expect(r.Kind).To(Equal(kind))
				Expect(r.Leader).To(Equal(leader))
				Expect(r.Term).To(Equal(term))
				Expect(r.Why).To(ContainSubstring(why))
				Expect(r.At).To(Equal(t0))
			},
			Entry("its own id leads: ok, the leader is itself",
				200, string(raftzBody(`{"id":"P1","leader":"P1","term":16,"state":"LEADER",`+peersOfZA1+`}`)), nil,
				kindOK, "t-za-1", 16, ""),
			Entry("a peer id leads: ok, named from the peer list",
				200, string(raftzBody(`{"id":"P1","leader":"P3","term":16,"state":"FOLLOWER",`+peersOfZA1+`}`)), nil,
				kindOK, "t-arb-1", 16, ""),
			Entry("leader null: no_leader, with its term",
				200, string(raftzBody(`{"id":"P1","leader":null,"term":17,"state":"CANDIDATE",`+peersOfZA1+`}`)), nil,
				kindNoLeader, "", 17, ""),
			Entry("leader empty: no_leader",
				200, string(raftzBody(`{"id":"P1","leader":"","term":17,"state":"CANDIDATE",`+peersOfZA1+`}`)), nil,
				kindNoLeader, "", 17, ""),
			Entry("no HTTP answer: unreachable, never invalid",
				0, "", errors.New("no answer in 1.0 s"),
				kindUnreachable, "", 0, "no answer in 1.0 s"),
			Entry("an HTTP error: invalid",
				503, "", nil,
				kindInvalid, "", 0, "HTTP 503"),
			Entry("not JSON: invalid",
				200, "<html>", nil,
				kindInvalid, "", 0, "no $SYS._meta_"),
			Entry("no meta group: invalid",
				200, `{"$SYS":{}}`, nil,
				kindInvalid, "", 0, "no $SYS._meta_"),
			Entry("_meta_ not an object: invalid",
				200, `{"$SYS":{"_meta_":[1]}}`, nil,
				kindInvalid, "", 0, "not an object"),
			Entry("term a string: invalid",
				200, string(raftzBody(`{"id":"P1","leader":"P1","term":"16"}`)), nil,
				kindInvalid, "", 0, "is not a number"),
			Entry("term 16.0: invalid, as Python's isinstance(term, int) says",
				200, string(raftzBody(`{"id":"P1","leader":"P1","term":16.0}`)), nil,
				kindInvalid, "", 0, "is not a number"),
			Entry("term true: invalid",
				200, string(raftzBody(`{"id":"P1","leader":"P1","term":true}`)), nil,
				kindInvalid, "", 0, "is not a number"),
			Entry("its own name among its peers: the wrong source, invalid",
				200, string(raftzBody(`{"id":"P1","leader":"P2","term":16,"peers":{"P2":{"name":"t-za-1"}}}`)), nil,
				kindInvalid, "", 16, "source mismatch"),
			Entry("a leader id with no name: invalid",
				200, string(raftzBody(`{"id":"P1","leader":"P9","term":16,`+peersOfZA1+`}`)), nil,
				kindInvalid, "", 16, "has no name"),
		)
	})

	Describe("the store", func() {
		ok := func(at time.Time, leader string, term int) metaReading {
			return metaReading{Server: "t-za-1", At: at, Kind: kindOK, Leader: leader, Term: term, State: "FOLLOWER"}
		}
		lost := func(at time.Time) metaReading {
			return metaReading{Server: "t-za-1", At: at, Kind: kindUnreachable, Why: "no answer in 1.0 s"}
		}
		za1 := func(st *store, now time.Time) serverView { return st.view(now).servers[0] }

		It("keeps the last answer when a poll fails, and lets it age (rule 3)", func() {
			st := newStore()
			st.putMeta(ok(t0, "t-arb-1", 16))
			st.putMeta(lost(t0.Add(2 * time.Second)))

			v := za1(st, t0.Add(2*time.Second))
			Expect(v.Reading).NotTo(BeNil())
			Expect(v.Reading.Leader).To(Equal("t-arb-1"))
			Expect(v.ReadingAge).To(Equal(2 * time.Second))
			Expect(v.Fresh).To(BeFalse())
			Expect(v.AttemptFailed).To(BeTrue())
		})

		It("calls an answer fresh for under metaFreshFor, and no longer", func() {
			st := newStore()
			st.putMeta(ok(t0, "t-arb-1", 16))

			Expect(za1(st, t0.Add(metaFreshFor-time.Millisecond)).Fresh).To(BeTrue())
			Expect(za1(st, t0.Add(metaFreshFor)).Fresh).To(BeFalse())
		})

		It("reports a change once, and a poll that changed nothing not at all", func() {
			st := newStore()

			Expect(st.putMeta(ok(t0, "t-arb-1", 16))).To(ConsistOf(
				"t-za-1 monitor answers",
				"t-za-1 reads leader t-arb-1, term 16, role FOLLOWER"))
			Expect(st.putMeta(ok(t0.Add(time.Second), "t-arb-1", 16))).To(BeEmpty())
			Expect(st.putMeta(ok(t0.Add(2*time.Second), "t-za-2", 17))).To(ConsistOf(
				"t-za-1 reads leader t-za-2, term 17, role FOLLOWER"))
			Expect(st.putMeta(lost(t0.Add(3 * time.Second)))).To(ConsistOf(
				"t-za-1 monitor gives no answer: no answer in 1.0 s"))
			Expect(st.putMeta(lost(t0.Add(4 * time.Second)))).To(BeEmpty())
			Expect(st.putMeta(ok(t0.Add(5*time.Second), "t-za-2", 17))).To(ConsistOf(
				"t-za-1 monitor answers"))
		})

		It("keeps process state and monitor state apart (rule 2)", func() {
			st := newStore()
			st.putMeta(ok(t0, "t-arb-1", 16))
			Expect(st.putProc("t-za-1", procStopped, t0)).To(ConsistOf("t-za-1 process stopped (ps), was unknown"))

			v := za1(st, t0)
			Expect(v.Proc).To(Equal(procStopped))
			Expect(v.Fresh).To(BeTrue(), "a stopped process does not erase or age a reading")
		})
	})

	Describe("the pollers", func() {
		It("time a reading when its answer arrives, and write each change to the history", func() {
			now := t0
			hist := newHistory(func() time.Time { return now })
			m := &monitor{
				fetch: func(_ context.Context, url string) (int, []byte, error) {
					Expect(url).To(Equal("http://127.0.0.1:8231/raftz?group=_meta_"))
					now = now.Add(800 * time.Millisecond) // the answer is slow
					return 200, raftzBody(`{"id":"P1","leader":"P3","term":16,"state":"FOLLOWER",` + peersOfZA1 + `}`), nil
				},
				st: newStore(), hist: hist, now: func() time.Time { return now },
			}

			m.pollMeta(context.Background(), servers[0])

			Expect(m.st.view(now).servers[0].Reading.At).To(Equal(t0.Add(800 * time.Millisecond)))
			evs, _ := hist.after(0)
			texts := []string{}
			for _, e := range evs {
				Expect(e.Kind).To(Equal(eventObservation))
				texts = append(texts, e.Text)
			}
			Expect(strings.Join(texts, "\n")).To(ContainSubstring("t-za-1 reads leader t-arb-1, term 16"))
		})
	})
})
