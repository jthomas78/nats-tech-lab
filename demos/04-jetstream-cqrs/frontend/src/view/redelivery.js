// The redelivery measurement. Two runs, recorded, not computed.
//
// Like `drain.js`, this file is data, and the same rule governs it: a row may
// only be added after the run has actually been made, and the command that
// made it must be written down next to it. The live pool panel cannot measure
// this — the server never says "this is a redelivery after 30 seconds", it
// says only that the delivery count is 2, and the worker that receives the
// event was never the worker that lost it. `cqrs pool` keeps a kill clock in
// its own process for exactly this reason, and prints the wait to its log.
// These rows are read off that log.
//
// Two AckWaits, not one, because a single run cannot tell a rule from a
// coincidence. Both waits came back equal to AckWait to within 20ms, which is
// the point: the pause is not a retry policy reacting to a failure, it is a
// timer the server was always going to run out.
//
// The harder finding is the last column. In both runs the redelivered event
// was DROPPED on arrival, because the watermark had moved on during the
// silence. A redelivery after AckWait is not a recovery in a pool: the wait
// bought nothing, and the event is still lost (BR-OD07).

export const REDELIVERY_MEASURED_AT =
  'Measured 2026-09-16 · NATS 2.14.3 in Docker on a laptop · 4 workers'

// The commands, exactly as they were typed. The seed is what makes the fold
// run on past the killed sequence while the worker is silent.
export const REDELIVERY_SOURCE = Object.freeze([
  'cqrs pool -workers 4 -ack-wait 30s -kill-at 74040',
  'cqrs seed -vehicle truck-7 -n 40',
  'cqrs pool -workers 4 -ack-wait 5s -kill-at 74079',
  'cqrs seed -vehicle truck-7 -n 40',
])

// killSeq is the event the killed worker was holding. foldAt is where the
// watermark had reached by the time that event came back.
export const REDELIVERY_RUNS = Object.freeze([
  Object.freeze({
    ackWait: '30s',
    killSeq: 74040,
    killedWorker: 3,
    toWorker: 4,
    waitedSeconds: 30.02,
    delivery: 2,
    foldAt: 74069,
    outcome: 'dropped',
  }),
  Object.freeze({
    ackWait: '5s',
    killSeq: 74079,
    killedWorker: 4,
    toWorker: 1,
    waitedSeconds: 5.001,
    delivery: 2,
    foldAt: 74109,
    outcome: 'dropped',
  }),
])

// The derived columns are the three questions a reader asks of these rows:
// was the wait the AckWait, who got the event back, and what did the wait buy.
export function redeliveryRows(runs = REDELIVERY_RUNS) {
  return runs.map((r) => ({
    ...r,
    overshoot: r.waitedSeconds - Number(r.ackWait.replace('s', '')),
    handoff: `${r.killedWorker} → ${r.toWorker}`,
    ranOn: r.foldAt - r.killSeq,
    recovered: r.outcome !== 'dropped',
  }))
}
