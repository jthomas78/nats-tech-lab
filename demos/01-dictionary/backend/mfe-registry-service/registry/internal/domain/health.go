package domain

import (
	"sort"
	"time"
)

/*
	Health is an OBSERVATION, and it is kept as far away from the catalogue as
	the code can put it (BR-AS65). Nothing in this file touches an Entry, a
	revision, a signed manifest or the audit trail: a probe result is what the
	platform noticed a moment ago, and a catalogue is what an operator and a
	publisher agreed to. Letting the first write to the second would mean a
	slow service could edit the registry.

	The timing lives here too, as a machine that is STEPPED rather than one
	that sleeps. Everything that decides anything — when a target is next due,
	whether a failure is the second one, whether a reading is still current —
	takes `now` as an argument, so a spec drives it with a fake clock and the
	thin runner that owns the real ticker has no decisions left in it. That is
	also why probes cannot overlap by construction rather than by luck: a
	target claimed by `Due` is not offered again until `Record` answers for it.
*/

// The three numbers, from Q5 and Q14. Interval and timeout are deliberately
// different: a probe that takes longer than the gap between probes would
// queue behind itself, so the timeout is the shorter of the two and a target
// with a hung probe is simply skipped rather than piled up.
const (
	HealthProbeInterval = 5 * time.Second
	HealthProbeTimeout  = 2 * time.Second
	HealthFreshness     = 15 * time.Second
	// Two, not one. A single failed probe is as likely to be a dropped packet
	// as a dead service, and this decoration sits next to a plugin the user is
	// looking at.
	HealthFailureThreshold = 2
)

// The state vocabulary. Six values, and the last three exist because "we did
// not check" must never be spelled the same way as "we checked and it is
// fine".
//
// HealthNotConfigured is BACKEND-ONLY since Phase 15. On the frontend plane
// there is nothing left to configure — a plugin reports about itself on a
// subject derived from its own id — so an enabled plugin is either heard from
// or absent, and never "not configured".
const (
	HealthUnknown       = "unknown"
	HealthHealthy       = "healthy"
	HealthUnavailable   = "unavailable"
	HealthStale         = "stale"
	HealthNotConfigured = "not configured"
	HealthNotApplicable = "not applicable"
)

// HealthSignal is one target's answer as a reader sees it. Cause is from a
// closed vocabulary, like Drift's: a probe failure carries the address that
// was dialled and sometimes text the remote chose, and neither belongs in a
// browser reply (BR-AS60).
type HealthSignal struct {
	State       string    `json:"state"`
	Cause       string    `json:"cause,omitempty"`
	LastCheckAt time.Time `json:"lastCheckAt,omitzero"`
}

// Merge keeps the newer of two readings and drops the other.
//
// It is the answer to a redelivered snapshot and to a reconnect catch-up
// arriving out of order: without it, every arrival of the same observation
// would look like fresh proof that the target was alive just now, and a
// service that died would stay green for as long as something kept resending
// its last good reading (BR-AS64).
func (s HealthSignal) Merge(next HealthSignal) HealthSignal {
	if next.LastCheckAt.After(s.LastCheckAt) {
		return next
	}
	return s
}

// HealthProbe is one probe's outcome, timestamped by whoever ran it.
type HealthProbe struct {
	OK    bool
	Cause string
	At    time.Time
}

func HealthProbeOK(at time.Time) HealthProbe { return HealthProbe{OK: true, At: at} }

func HealthProbeFailed(cause string, at time.Time) HealthProbe {
	return HealthProbe{Cause: cause, At: at}
}

// SummarizeBackend derives one answer from a plugin's dependencies (BR-AS62).
//
// The order is worst-first on purpose. A plugin with one dead dependency is
// not partly healthy, and reporting the majority verdict would hide the one
// fact an operator needs. `nil` and an empty slice mean different things and
// are the reason this takes a slice rather than a count: no mapping is a
// deployment that never said, and an empty mapping is a deployment that said
// "this one is frontend-only".
func SummarizeBackend(dependencies []HealthSignal) HealthSignal {
	if dependencies == nil {
		return HealthSignal{State: HealthNotConfigured}
	}
	if len(dependencies) == 0 {
		return HealthSignal{State: HealthNotApplicable}
	}
	summary := HealthSignal{State: HealthHealthy}
	for _, d := range dependencies {
		if d.LastCheckAt.After(summary.LastCheckAt) {
			summary.LastCheckAt = d.LastCheckAt
		}
	}
	for _, d := range dependencies {
		if d.State == HealthUnavailable {
			return HealthSignal{State: HealthUnavailable, Cause: d.Cause, LastCheckAt: summary.LastCheckAt}
		}
	}
	for _, d := range dependencies {
		if d.State != HealthHealthy {
			summary.State = d.State
			summary.Cause = d.Cause
			break
		}
	}
	return summary
}

// HealthWorker schedules probes and folds their answers into signals. It is
// not concurrent and owns no clock: the runner calls Due, probes whatever it
// is handed, and calls Record.
type HealthWorker struct {
	targets map[string]*healthTarget
	stopped bool
}

type healthTarget struct {
	signal   HealthSignal
	failures int
	// nextDue is zero until the first probe, which is what makes every target
	// due on the very first tick rather than one interval after boot.
	nextDue  time.Time
	inFlight bool
}

func NewHealthWorker(targets []string) *HealthWorker {
	w := &HealthWorker{targets: map[string]*healthTarget{}}
	for _, id := range targets {
		w.targets[id] = &healthTarget{signal: HealthSignal{State: HealthUnknown}}
	}
	return w
}

// Due claims every target that is ready to be probed and not already being
// probed. Claiming is the point: the returned ids are off the schedule until
// Record answers for them, so a hung probe delays its own target and nothing
// else. Sorted so a runner's work is stable to read in a log.
func (w *HealthWorker) Due(now time.Time) []string {
	if w.stopped {
		return nil
	}
	due := []string{}
	for id, t := range w.targets {
		if t.inFlight || now.Before(t.nextDue) {
			continue
		}
		t.inFlight = true
		t.nextDue = now.Add(HealthProbeInterval)
		due = append(due, id)
	}
	sort.Strings(due)
	return due
}

// Record folds one probe's answer in. A result for a target that was not
// claimed — a late answer after Stop, or a duplicate — is dropped rather than
// applied: it would otherwise be able to revive a stopped worker's state.
func (w *HealthWorker) Record(id string, probe HealthProbe) {
	t, ok := w.targets[id]
	if !ok || !t.inFlight {
		return
	}
	t.inFlight = false

	if probe.OK {
		t.failures = 0
		t.signal = HealthSignal{State: HealthHealthy, LastCheckAt: probe.At}
		return
	}
	t.failures++
	if t.failures >= HealthFailureThreshold {
		t.signal = HealthSignal{State: HealthUnavailable, Cause: probe.Cause, LastCheckAt: probe.At}
		return
	}
	// One failure. A target that was healthy stays healthy — the freshness
	// window is what stops that being indefinite — and a target nothing has
	// ever proved healthy stays unknown, because a first failure is not
	// evidence of a previous good state.
	if t.signal.State == HealthHealthy {
		t.signal.LastCheckAt = probe.At
		return
	}
	t.signal = HealthSignal{State: HealthUnknown, LastCheckAt: probe.At}
}

// Snapshot is every signal as of now, with the freshness window applied.
//
// Applied at READ time rather than written into the state, because staleness
// is a fact about the gap between the last check and this moment, and a
// worker whose runner has stopped ticking must go stale on its own rather
// than needing one more tick to say so.
func (w *HealthWorker) Snapshot(now time.Time) map[string]HealthSignal {
	out := map[string]HealthSignal{}
	for id, t := range w.targets {
		out[id] = staleAfter(t.signal, now)
	}
	return out
}

func (w *HealthWorker) Stop() {
	w.stopped = true
	for _, t := range w.targets {
		t.inFlight = false
	}
}

// staleAfter ages one reading. Unknown is left alone: stale means "this was
// true once", unknown means "we have never looked", and collapsing the two
// would invent a past that never happened.
func staleAfter(s HealthSignal, now time.Time) HealthSignal {
	if s.LastCheckAt.IsZero() {
		return s
	}
	switch s.State {
	case HealthNotConfigured, HealthNotApplicable, HealthUnknown:
		return s
	}
	if now.Sub(s.LastCheckAt) > HealthFreshness {
		return HealthSignal{State: HealthStale, Cause: s.Cause, LastCheckAt: s.LastCheckAt}
	}
	return s
}

// HealthOrigins is gone (Phase 15c). It answered "which address may the
// registry dial for this plugin's frontend", and since decision 14 nothing
// dials one: the plugin checks itself and publishes on a subject derived from
// its own id. FetchOrigins, its deliberate near-twin, stays — the drift check
// really does fetch a manifest (BR-AS45), and the two were separate types
// precisely so one could be retired without the other.

// HealthTargets is gone too. It answered "which backend services may the
// registry dial for this plugin", out of a deployment map, and the answer now
// comes from the entry itself: the plugin declares in its signed manifest and
// an operator approves at curation. See backendservices.go for why the split
// is a declaration and an approval rather than one list.

// subjectSafe keeps a service id inside one subject token. A dot would
// split the token and change which subject is dialled; a wildcard would widen
// it past the one service the grant is for (CLAUDE.md, subject-safe IDs).
func subjectSafe(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '/', r == '=':
		default:
			return false
		}
	}
	return true
}

// Watch reconciles the worker's target set with the one the catalogue now
// implies. Targets that are already watched keep their signal AND their
// failure count: rebuilding the worker every pass would reset every counter,
// so a service that failed once per pass forever would never reach the second
// consecutive failure and would never be reported unavailable.
func (w *HealthWorker) Watch(targets []string) {
	if w.stopped {
		return
	}
	wanted := map[string]bool{}
	for _, id := range targets {
		wanted[id] = true
		if _, ok := w.targets[id]; !ok {
			w.targets[id] = &healthTarget{signal: HealthSignal{State: HealthUnknown}}
		}
	}
	for id, t := range w.targets {
		// A target that has gone away is dropped, except while its probe is
		// still running: dropping it there would let the late answer create
		// the entry again with no schedule behind it.
		if !wanted[id] && !t.inFlight {
			delete(w.targets, id)
		}
	}
}
