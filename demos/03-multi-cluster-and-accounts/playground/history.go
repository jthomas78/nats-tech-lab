package main

// The history (D03-R26): a timestamped log of actions, results and
// observations, in memory. The page reads it with /state?after=<seq>, so it
// only ever fetches what is new.
//
// sink, when set, gets every event too. Step 5 points it at the session
// file under playground/.run/sessions/ (D03-R28) — never at lab/run/.

import (
	"sync"
	"time"
)

const (
	eventAction      = "action"
	eventResult      = "result"
	eventObservation = "observation"

	// historyKept is how many events stay in memory. The session file keeps
	// them all.
	historyKept = 2000
)

type event struct {
	Seq  int       `json:"seq"`
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	// Cmd is the command the event belongs to, 0 for an observation.
	Cmd int `json:"cmd,omitempty"`
	// End is set on a result: result, error, timeout or cancelled (rule 9).
	End  string `json:"end,omitempty"`
	Text string `json:"text"`
}

type history struct {
	mu     sync.Mutex
	now    func() time.Time
	seq    int
	events []event
	sink   func(event)
}

func newHistory(now func() time.Time) *history {
	return &history{now: now}
}

func (h *history) add(kind string, cmd int, end, text string) event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	e := event{Seq: h.seq, At: h.now(), Kind: kind, Cmd: cmd, End: end, Text: text}
	h.events = append(h.events, e)
	if len(h.events) > historyKept {
		h.events = h.events[len(h.events)-historyKept:]
	}
	if h.sink != nil {
		h.sink(e)
	}
	return e
}

// after returns every kept event with a sequence above seq, oldest first,
// and the newest sequence so far.
func (h *history) after(seq int) ([]event, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []event{}
	for _, e := range h.events {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out, h.seq
}
