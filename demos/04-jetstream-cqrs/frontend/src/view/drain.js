// The 1-vs-4 measurement. Four runs, recorded, not computed.
//
// This file is data, and it is the only place in the UI that holds a number
// the page did not watch happen. Everything else on lesson 02 is read live
// from KV. These four rows are a MEASUREMENT: they were produced by the
// command in `source` below, on the machine and date in `MEASURED_AT`, and
// they are repeated verbatim in the demo's README.
//
// The rule that governs this file: a row may only be added here after the run
// has actually been made, and the run must be named. A row nobody ran is the
// one thing this demo must not put on a screen. That is why `-drain` exists at
// all — it rebuilds the pool's projection from sequence 1, so every row in the
// table answers the same question against the same log.

export const MEASURED_AT =
  'Measured 2026-09-15 · NATS 2.14.3 in Docker on a laptop · 10029 events'

// The runs, exactly as they were typed. One line per command, so the panel
// can draw them as a terminal rather than as one unreadable one-liner.
export const DRAIN_SOURCE = Object.freeze([
  'cqrs seed -vehicle truck-7 -n 10000',
  'cqrs pool -workers 1 -drain',
  'cqrs pool -workers 2 -drain',
  'cqrs pool -workers 4 -drain',
  'cqrs pool -workers 8 -drain',
])

// events is what the consumer handed out; folded + dropped account for it.
export const DRAIN_RUNS = Object.freeze([
  Object.freeze({ workers: 1, seconds: 23.5, rate: 426, events: 10029, folded: 10029, dropped: 0 }),
  Object.freeze({ workers: 2, seconds: 12.2, rate: 824, events: 10029, folded: 10025, dropped: 4 }),
  Object.freeze({ workers: 4, seconds: 6.3, rate: 1600, events: 10029, folded: 6947, dropped: 3082 }),
  Object.freeze({ workers: 8, seconds: 4.8, rate: 2100, events: 10029, folded: 4338, dropped: 5691 }),
])

// Both derived columns are measured against the ONE-worker run, because that
// is the control: one worker cannot race itself, so it is the only row that
// folds the whole log. Speed is what you gained. Loss is what it cost.
export function drainRows(runs = DRAIN_RUNS) {
  const base = runs[0]
  if (!base) return []
  return runs.map((r) => ({
    ...r,
    speedup: base.seconds / r.seconds,
    lossPct: r.events > 0 ? (r.dropped / r.events) * 100 : 0,
    // Bar width. The slowest run is the full track, so the bars shorten as
    // the pool gets faster — the shape of the win, read left to right.
    barPct: base.seconds > 0 ? (r.seconds / base.seconds) * 100 : 0,
    control: r === base,
  }))
}
