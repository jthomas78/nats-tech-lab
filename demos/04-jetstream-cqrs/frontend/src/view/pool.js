// Lesson 02's arithmetic. Pure, so it is specced without a browser — the same
// split view/lane.js uses, and for the same reason.
//
// Every number here is shaped from what a worker writes into KV
// odometer-pool-workers (WorkerState, cqrs/pool.go): worker, status, holding,
// acked, dropped, behind. That list is short on purpose, and this file must
// not pretend it is longer. A heartbeat does NOT say "the last sequence I
// acked", so nothing here can place an idle worker on the lane; it does not
// say how long a redelivery took, so nothing here reports a duration.
//
// A demo that prints a plausible number it did not measure has broken the one
// promise it makes. Where there is no measurement these functions return
// `null` and the panel says so in words.

import { STREAM } from '../config.js'

// A worker that has never acked anything is idle. It is the whole of the
// starvation lesson: MaxAckPending belongs to the CONSUMER, so a cap of three
// leaves five of eight workers with nothing to do, and they look exactly like
// this.
const isIdle = (w) => w.status !== 'killed' && Number(w.acked) === 0

export function workerRows(workers) {
  const rows = workers instanceof Map ? [...workers.values()] : [...(workers ?? [])]
  return rows.slice().sort((a, b) => a.worker - b.worker)
}

export function poolHealth(rows = []) {
  let acked = 0
  let dropped = 0
  let idle = 0
  let killed = 0
  for (const w of rows) {
    acked += Number(w.acked) || 0
    dropped += Number(w.dropped) || 0
    if (w.status === 'killed') killed += 1
    else if (isIdle(w)) idle += 1
  }
  return {
    workers: rows.length,
    running: rows.length > 0,
    acked,
    dropped,
    idle,
    killed,
    busy: rows.length - idle - killed,
  }
}

// Is the pool running but out of work?
//
// A pool that has folded up to the head has nothing left to do, so every
// worker sits at `waiting` with 0 acked — which looks exactly like a broken
// screen. It is not: it is the correct picture of an idle pool, and the panel
// says so and prints the command that gives it something to fold.
//
// The signal is the FOLD POSITION against the head, not the ack counts.
// Counters are per-process and reset to 0 every time somebody restarts the
// pool, so "0 acked" is true of a busy pool in its first second too. The fold
// position is in KV and survives the restart, so it does not flicker.
//
// Null head or an empty pool bucket means there is nothing to compare, and
// that is reported as "not caught up" rather than guessed at.
export function poolCaughtUp(head = 0, foldSeq = 0, health = {}) {
  if (!health.running) return false
  const h = Number(head) || 0
  const f = Number(foldSeq) || 0
  return h > 0 && f >= h
}

// Every bar is measured against the busiest worker, not against the total.
// The lesson is the SHAPE of the distribution — three workers doing the work
// and five doing none — and a share-of-total bar flattens exactly that.
export function ackBars(rows = []) {
  const top = rows.reduce((m, w) => Math.max(m, Number(w.acked) || 0), 0)
  return rows.map((w) => {
    const acked = Number(w.acked) || 0
    return {
      worker: w.worker,
      acked,
      status: w.status,
      idle: isIdle(w),
      pct: top > 0 ? Math.round((acked / top) * 100) : 0,
    }
  })
}

// The pool's fold against the correct one. The pool is deliberately damaged —
// it folds the same log out of order and BR-OD08 refuses what arrives behind
// the watermark — so its total is SHORT. It can never be over: a dropped
// event is a fact that is gone, and nothing counts one twice.
//
// Null when there is nothing to compare. A drift of 0 against an empty read
// model is not "no damage", it is "no measurement", and the two must not look
// the same on screen.
export function foldDamage(poolRows = [], readRows = []) {
  if (!poolRows.length || !readRows.length) return null
  const sum = (rows) => rows.reduce((t, r) => t + (Number(r.totalKm) || 0), 0)
  const poolKm = sum(poolRows)
  const readKm = sum(readRows)
  const drift = poolKm - readKm
  return {
    poolKm,
    readKm,
    drift,
    damaged: drift !== 0,
    vehicles: poolRows.length,
  }
}

// The rows for LagLane — the N-marker generalisation task 04.7.8 added.
//
// The log, then the pool's own fold, then one row per worker that is actually
// holding a sequence. An idle worker is left out rather than drawn at zero:
// `holding` is 0 when idle, and a marker at sequence 0 says "30 events
// behind" about a worker that is simply waiting.
export function poolLaneRows(head = 0, rows = [], foldSeq = 0) {
  const h = Number(head) || 0
  const out = [
    { id: 'log', kind: 'log', tone: 'log', text: `${STREAM} · ${h} events, the only source of truth` },
  ]
  if (Number(foldSeq) > 0) {
    out.push({
      id: 'pool',
      tone: 'read',
      seq: Number(foldSeq),
      text: `odometer-pool · folded to ${foldSeq}`,
      say: 'The pool fold',
    })
  }
  for (const w of rows) {
    const holding = Number(w.holding) || 0
    if (holding <= 0) continue
    const killed = w.status === 'killed'
    out.push({
      id: `worker-${w.worker}`,
      tone: killed ? 'lost' : 'worker',
      seq: holding,
      text: `worker ${w.worker} · ${killed ? `silent, still holding #${holding}` : `folding #${holding}`}`,
      say: `Worker ${w.worker}`,
    })
  }
  return out
}

// The redelivery lesson needs a worker that went silent while holding a
// sequence. `-kill-at` produces exactly one. Null until it does — the tab
// then prints the command instead of a drawing, which is honest.
export function redelivery(rows = []) {
  const dead = rows.find((w) => w.status === 'killed' && Number(w.holding) > 0)
  if (!dead) return null
  return { worker: dead.worker, seq: Number(dead.holding), behind: Number(dead.behind) || 0 }
}
