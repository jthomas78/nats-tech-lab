// The MaxAckPending measurement. Three runs, recorded, not computed.
//
// Same rule as drain.js and redelivery.js: a row may only be added after the
// run was made, and the command that made it is written beside it.
//
// THIS FILE EXISTS BECAUSE THE TAB WAS WRONG. Before 04.7.6 the Starvation tab
// said "eight workers on a cap of three — start it and watch five of them do
// nothing". It was a reasonable guess and it is false. Every run below, down
// to a cap of ONE message in flight for the whole consumer, had all eight
// workers acking, and the spread between them is under 2%. They take turns:
// a worker acks, a slot frees, the next worker's fetch is served.
//
// What MaxAckPending actually does is throttle the consumer, and that turns
// out to be the more useful lesson — it is the loss dial. A smaller cap is
// slower and drops less, because there is less concurrency to reorder with.
// At a cap of 1 the pool folds the entire log and drops nothing at all: the
// same eight workers, the same code, correct output, at a third of the speed.
//
// Read beside drain.js, the two halves meet: 04.7.4 varied the worker count
// and found speed costs correctness. This varies the cap at ONE worker count
// and finds the same trade, on the knob you would actually reach for in
// production.

export const STARVATION_MEASURED_AT =
  'Measured 2026-09-16 · NATS 2.14.3 in Docker on a laptop · 8 workers · 74 109 events'

// The commands, exactly as they were typed. `-drain` rebuilds the pool's
// projection from sequence 1, so all three answer the same question against
// the same log — see drain.js.
export const STARVATION_SOURCE = Object.freeze([
  'cqrs pool -workers 8 -max-pending 1 -drain',
  'cqrs pool -workers 8 -max-pending 3 -drain',
  'cqrs pool -workers 8 -max-pending 1000 -drain',
])

// busy is how many of the workers acked at least one event. It is the column
// the tab's old claim lived or died by, and it is the pool's own count
// (PoolShare in cqrs/pool.go), not a reading off the heartbeats.
export const STARVATION_RUNS = Object.freeze([
  Object.freeze({
    maxPending: 1,
    workers: 8,
    seconds: 94.1,
    rate: 788,
    events: 74109,
    folded: 74109,
    dropped: 0,
    busy: 8,
  }),
  Object.freeze({
    maxPending: 3,
    workers: 8,
    seconds: 50.0,
    rate: 1481,
    events: 74109,
    folded: 57529,
    dropped: 16580,
    busy: 8,
  }),
  Object.freeze({
    maxPending: 1000,
    workers: 8,
    seconds: 33.2,
    rate: 2229,
    events: 74109,
    folded: 31912,
    dropped: 42197,
    busy: 8,
  }),
])

// The cap-3 run, made a second time. A race that is quoted to four figures
// should be shown to wobble, and this is the wobble: same shape, same busy
// count, times and drops within a few per cent.
export const STARVATION_REPEAT = Object.freeze({
  maxPending: 3,
  seconds: 54.6,
  rate: 1357,
  dropped: 16722,
  busy: 8,
})

// Speed is read against the SMALLEST cap, not the largest, because the
// smallest cap is the run that is correct. Everything faster than it is
// faster by losing something.
export function starvationRows(runs = STARVATION_RUNS) {
  const base = runs.find((r) => r.maxPending === 1) ?? runs[0]
  const slowest = Math.max(...runs.map((r) => r.seconds))
  return runs.map((r) => ({
    ...r,
    idle: r.workers - r.busy,
    lossPct: r.events > 0 ? (r.dropped / r.events) * 100 : 0,
    speedup: base.seconds / r.seconds,
    barPct: slowest > 0 ? (r.seconds / slowest) * 100 : 0,
    control: r === base,
  }))
}
