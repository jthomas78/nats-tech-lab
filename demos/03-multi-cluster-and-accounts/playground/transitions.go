package main

// The transition timer (D03-R29). Pure: no clock, no I/O. It is told when a
// change was confirmed and fed every summary; it says how long the change
// took to reach the next agreement, and what that agreement was.
//
// A transition opens when a freeze or resume is fully confirmed, or when a
// leadership request is accepted. It records the last agreed leader and
// term before it. It closes:
//
//   - at the first Agreed summary after the summary has left Agreed, or that
//     names a different leader or term ("agreed");
//   - after transitionWatchFor with no change ("no_change");
//   - after transitionWatchFor with no agreement ("no_agreement");
//   - when the next transition opens ("superseded").
//
// Times run from the confirmed change to the poll that saw agreement, on the
// service clock. Polls are metaPollEvery apart, so a time is good to about
// that much. These are playground numbers, never measurements (rule 8).

import (
	"fmt"
	"time"
)

const (
	changeFreeze     = "freeze"
	changeResume     = "resume"
	changeLeadership = "leadership"

	outcomeAgreed      = "agreed"
	outcomeNoChange    = "no_change"
	outcomeNoAgreement = "no_agreement"
	outcomeSuperseded  = "superseded"

	// transitionsKept is how many closed transitions the page shows.
	transitionsKept = 5
)

type change struct {
	Kind    string    `json:"kind"`
	Cluster string    `json:"cluster"`
	At      time.Time `json:"at"`
}

type transition struct {
	ID     int            `json:"id"`
	Change change         `json:"change"`
	Before *lastAgreement `json:"before,omitempty"`

	// LeftAgreed is set once a summary that is not Agreed has been seen.
	LeftAgreed bool `json:"leftAgreed"`

	Open    bool           `json:"open"`
	Outcome string         `json:"outcome,omitempty"`
	Text    string         `json:"text,omitempty"`
	After   *lastAgreement `json:"after,omitempty"`
	Elapsed time.Duration  `json:"elapsedNs,omitempty"`
	// Delta compares After with Before: "unchanged", or the term step and
	// whether the leader moved.
	Delta string `json:"delta,omitempty"`
}

type transitionLog struct {
	nextID int
	Open   *transition  `json:"open,omitempty"`
	Closed []transition `json:"closed"` // newest first, at most transitionsKept
}

// start opens a transition for a confirmed change. An open one closes first,
// as superseded.
func (l *transitionLog) start(c change, before *lastAgreement) {
	if l.Open != nil {
		l.close(outcomeSuperseded, "next change came first", nil, c.At)
	}
	l.nextID++
	var b *lastAgreement
	if before != nil {
		cp := *before
		b = &cp
	}
	l.Open = &transition{ID: l.nextID, Change: c, Before: b, Open: true}
}

// observe feeds one summary. It may close the open transition.
func (l *transitionLog) observe(s summary) {
	t := l.Open
	if t == nil || s.At.Before(t.Change.At) {
		return
	}
	if s.State != stateAgreed {
		t.LeftAgreed = true
	} else {
		differs := t.Before == nil || s.Leader != t.Before.Leader || s.Term != t.Before.Term
		if t.LeftAgreed || differs {
			after := &lastAgreement{Leader: s.Leader, Term: s.Term, At: s.At}
			l.close(outcomeAgreed, fmt.Sprintf("agreed after %.1f s", s.At.Sub(t.Change.At).Seconds()), after, s.At)
			return
		}
	}
	if s.At.Sub(t.Change.At) >= transitionWatchFor {
		secs := int(transitionWatchFor / time.Second)
		if t.LeftAgreed {
			l.close(outcomeNoAgreement, fmt.Sprintf("no agreement in %d s", secs), nil, s.At)
		} else {
			l.close(outcomeNoChange, fmt.Sprintf("no change in %d s", secs), nil, s.At)
		}
	}
}

func (l *transitionLog) close(outcome, text string, after *lastAgreement, at time.Time) {
	t := *l.Open
	t.Open = false
	t.Outcome = outcome
	t.Text = text
	t.After = after
	t.Elapsed = at.Sub(t.Change.At)
	if after != nil {
		t.Delta = delta(t.Before, after)
	}
	l.Open = nil
	l.Closed = append([]transition{t}, l.Closed...)
	if len(l.Closed) > transitionsKept {
		l.Closed = l.Closed[:transitionsKept]
	}
}

func delta(before, after *lastAgreement) string {
	if before == nil {
		return "no agreement before"
	}
	if before.Leader == after.Leader && before.Term == after.Term {
		return "unchanged"
	}
	moved := "leader same"
	if before.Leader != after.Leader {
		moved = "leader moved"
	}
	return fmt.Sprintf("term %+d, %s", after.Term-before.Term, moved)
}
