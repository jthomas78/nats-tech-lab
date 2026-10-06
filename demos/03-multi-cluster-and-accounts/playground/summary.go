package main

// The meta summary (D03-R25). A pure function: fed the nine servers' last
// /raftz answers, their process states, the last agreement and the time, it
// says which of five states the readings are in. It never predicts and never
// counts voters (rules 1 and 3).
//
// The reading kinds are classify-10.py's: ok, no_leader, unreachable,
// invalid. Only fresh ok and no_leader answers are evidence.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	kindOK          = "ok"
	kindNoLeader    = "no_leader"
	kindUnreachable = "unreachable"
	kindInvalid     = "invalid"
)

// metaReading is one server's answer to /raftz?group=_meta_, parsed the way
// classify-10.py's parse_reading does. The JSON names match its .jsonl
// lines, so kept exercise 10 readings load as fixtures unchanged.
type metaReading struct {
	Server string    `json:"server"`
	At     time.Time `json:"-"`
	Kind   string    `json:"kind"`
	Leader string    `json:"leader"`
	Term   int       `json:"term"`
	State  string    `json:"state"`
	Why    string    `json:"why,omitempty"`
}

// serverObs is the monitor store's view of one server. Answer is the last
// HTTP answer (nil = never answered); a failed poll never erases it, it ages.
// AttemptFailed says the newest poll got no answer at all.
type serverObs struct {
	Server        string
	Answer        *metaReading
	AttemptFailed bool
}

// lastAgreement is the most recent Agreed summary: who, which term, and the
// time of its newest supporting reading.
type lastAgreement struct {
	Leader string    `json:"leader"`
	Term   int       `json:"term"`
	At     time.Time `json:"at"`
}

type summaryState string

const (
	stateAgreed       summaryState = "agreed"
	stateAgreedNone   summaryState = "agreed_no_leader"
	stateDisagreement summaryState = "disagreement"
	stateStale        summaryState = "stale"
	stateInsufficient summaryState = "insufficient"
)

// view is one (leader, term) claim and the servers that hold it. Leader ""
// is "no leader".
type view struct {
	Leader  string   `json:"leader"`
	Term    int      `json:"term"`
	Servers []string `json:"servers"`
}

// readingCounts are always shown beside the state. Each server is in exactly
// one bucket: fresh evidence, fresh but invalid, no answer (newest poll
// failed), stale (an old answer, newest poll not failed), or never answered.
type readingCounts struct {
	Fresh     int `json:"fresh"`
	Invalid   int `json:"invalid"`
	NoAnswer  int `json:"noAnswer"`
	Stale     int `json:"stale"`
	NeverSeen int `json:"neverAnswered"`
}

type procCounts struct {
	Running int `json:"running"`
	Stopped int `json:"stopped"`
	Gone    int `json:"gone"`
	Unknown int `json:"unknown"`
}

type summary struct {
	At      time.Time     `json:"at"`
	State   summaryState  `json:"state"`
	Text    string        `json:"text"`
	Leader  string        `json:"leader,omitempty"`
	Term    int           `json:"term,omitempty"`
	Views   []view        `json:"views,omitempty"`
	Reading readingCounts `json:"readings"`
	Procs   procCounts    `json:"processes"`
	// Majority is metaMajority, carried so the page can label it derived.
	Majority int `json:"majority"`
	// Last is the agreement to remember for the next call: this one's, if
	// this summary agreed on a leader, else the one passed in.
	Last *lastAgreement `json:"last,omitempty"`
}

// summarize applies the rules in PLAYGROUND-PLAN.md, "The meta summary
// rules", in this order:
//
//  1. Disagreement — fresh answers name more than one (leader, term), or a
//     leader and no leader. However few they are.
//  2. Agreed — at least metaMajority fresh answers, all naming one leader and
//     term, and that leader's own fresh answer says LEADER.
//  3. Agreed: no leader — at least metaMajority fresh answers, all no_leader.
//  4. Stale — none of the above, and a last agreement exists whose newest
//     supporting reading is older than metaFreshFor.
//  5. Insufficient evidence — anything else, saying what is missing.
func summarize(obs []serverObs, procs []procState, last *lastAgreement, now time.Time) summary {
	s := summary{At: now, Majority: metaMajority, Last: last}

	for _, p := range procs {
		switch p {
		case procRunning:
			s.Procs.Running++
		case procStopped:
			s.Procs.Stopped++
		case procGone:
			s.Procs.Gone++
		default:
			s.Procs.Unknown++
		}
	}

	fresh := map[string]*metaReading{}
	var evidence []*metaReading
	for _, o := range obs {
		a := o.Answer
		isFresh := a != nil && now.Sub(a.At) < metaFreshFor
		switch {
		case a == nil:
			s.Reading.NeverSeen++
		case isFresh && (a.Kind == kindOK || a.Kind == kindNoLeader):
			s.Reading.Fresh++
			fresh[o.Server] = a
			evidence = append(evidence, a)
		case isFresh:
			s.Reading.Invalid++
		case o.AttemptFailed:
			s.Reading.NoAnswer++
		default:
			s.Reading.Stale++
		}
	}

	s.Views = views(evidence)

	if len(s.Views) > 1 {
		s.State = stateDisagreement
		parts := make([]string, len(s.Views))
		for i, v := range s.Views {
			parts[i] = fmt.Sprintf("%s (%d)", describeView(v), len(v.Servers))
		}
		s.Text = "fresh readings disagree: " + strings.Join(parts, " vs ")
		return s
	}

	if len(evidence) >= metaMajority {
		v := s.Views[0]
		if v.Leader == "" {
			s.State = stateAgreedNone
			s.Text = "no server reports a leader"
			return s
		}
		own, ok := fresh[v.Leader]
		if ok && own.State == "LEADER" {
			s.State = stateAgreed
			s.Leader, s.Term = v.Leader, v.Term
			s.Text = fmt.Sprintf("%s, term %d, named by %d fresh readings", v.Leader, v.Term, len(v.Servers))
			// The agreement is as old as its newest supporting reading: once
			// that is older than metaFreshFor, all its support is stale.
			newest := own.At
			for _, srv := range v.Servers {
				if fresh[srv].At.After(newest) {
					newest = fresh[srv].At
				}
			}
			s.Last = &lastAgreement{Leader: v.Leader, Term: v.Term, At: newest}
			return s
		}
		s.State = stateInsufficient
		if !ok {
			s.Text = fmt.Sprintf("%d fresh readings name %s, term %d, but %s has no fresh answer of its own",
				len(v.Servers), v.Leader, v.Term, v.Leader)
		} else {
			s.Text = fmt.Sprintf("%d fresh readings name %s, term %d, but %s itself says %s",
				len(v.Servers), v.Leader, v.Term, v.Leader, own.State)
		}
		return s
	}

	if last != nil && now.Sub(last.At) > metaFreshFor {
		s.State = stateStale
		s.Text = fmt.Sprintf("last agreed %s, term %d, %.1f s ago", last.Leader, last.Term, now.Sub(last.At).Seconds())
		return s
	}

	s.State = stateInsufficient
	s.Text = fmt.Sprintf("%d fresh readings; an agreement needs %d", len(evidence), metaMajority)
	return s
}

// views groups evidence by (leader, term), largest group first, then by
// leader and term, so the output is stable.
func views(ev []*metaReading) []view {
	type key struct {
		leader string
		term   int
	}
	by := map[key][]string{}
	for _, r := range ev {
		k := key{r.Leader, r.Term}
		if r.Kind == kindNoLeader {
			// classify-10.py's no_leader carries a term, but "no leader" is one
			// claim whatever the term: two candidates at terms 16 and 17 do not
			// disagree about who leads.
			k = key{"", 0}
		}
		by[k] = append(by[k], r.Server)
	}
	out := make([]view, 0, len(by))
	for k, ss := range by {
		sort.Strings(ss)
		out = append(out, view{Leader: k.leader, Term: k.term, Servers: ss})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Servers) != len(out[j].Servers) {
			return len(out[i].Servers) > len(out[j].Servers)
		}
		if out[i].Leader != out[j].Leader {
			return out[i].Leader < out[j].Leader
		}
		return out[i].Term < out[j].Term
	})
	return out
}

func describeView(v view) string {
	if v.Leader == "" {
		return "no leader"
	}
	return fmt.Sprintf("%s term %d", v.Leader, v.Term)
}
