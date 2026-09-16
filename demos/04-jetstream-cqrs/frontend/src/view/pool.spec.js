import { describe, expect, it } from 'vitest'

import {
  ackBars,
  foldDamage,
  poolCaughtUp,
  poolHealth,
  poolLaneRows,
  redelivery,
  workerRows,
} from './pool.js'
import { POOL_STREAM, STREAM } from '../config.js'

// Lesson 02's arithmetic, kept out of the Vue file so it can be specced
// without a browser — the same split lane.js already uses.
//
// Everything here is shaped from what a worker actually writes into KV
// odometer-pool-workers (WorkerState in cqrs/pool.go). Nothing is invented:
// a number this demo has not measured must not appear on the screen at all,
// and these functions return null or 0 rather than a plausible-looking guess.

const W = (worker, over = {}) => ({
  id: `worker.${String(worker).padStart(2, '0')}`,
  worker,
  status: 'waiting',
  holding: 0,
  acked: 0,
  dropped: 0,
  behind: 0,
  at: '',
  ...over,
})

describe('workerRows', () => {
  it('sorts by worker number, not by the order KV replayed the keys', () => {
    const rows = workerRows(new Map([['worker.03', W(3)], ['worker.01', W(1)], ['worker.02', W(2)]]))
    expect(rows.map((r) => r.worker)).toEqual([1, 2, 3])
  })

  it('survives an empty bucket, because nothing is running yet', () => {
    expect(workerRows(new Map())).toEqual([])
  })
})

describe('poolHealth', () => {
  it('adds up what the pool did and what it lost', () => {
    const h = poolHealth([W(1, { acked: 31 }), W(2, { acked: 28, dropped: 2 }), W(3, { acked: 30, dropped: 1 })])
    expect(h.workers).toBe(3)
    expect(h.acked).toBe(89)
    expect(h.dropped).toBe(3)
  })

  it('counts a worker that has acked nothing as idle — that is starvation', () => {
    const h = poolHealth([W(1, { acked: 41 }), W(2, { acked: 0 }), W(3, { acked: 0 })])
    expect(h.idle).toBe(2)
    expect(h.busy).toBe(1)
  })

  it('counts a killed worker apart from an idle one', () => {
    const h = poolHealth([W(1, { acked: 9 }), W(2, { acked: 4, status: 'killed' })])
    expect(h.killed).toBe(1)
    expect(h.idle).toBe(0)
  })

  it('says nothing is running when the bucket is empty', () => {
    const h = poolHealth([])
    expect(h.workers).toBe(0)
    expect(h.running).toBe(false)
  })
})

describe('ackBars', () => {
  it('measures every bar against the busiest worker, so the shape is the point', () => {
    const bars = ackBars([W(1, { acked: 40 }), W(2, { acked: 20 }), W(3, { acked: 0 })])
    expect(bars.map((b) => b.pct)).toEqual([100, 50, 0])
  })

  it('marks a worker that never got work, so starvation is visible not inferred', () => {
    const bars = ackBars([W(1, { acked: 40 }), W(2, { acked: 0 })])
    expect(bars.map((b) => b.idle)).toEqual([false, true])
  })

  it('does not divide by zero before the first ack', () => {
    const bars = ackBars([W(1), W(2)])
    expect(bars.every((b) => b.pct === 0)).toBe(true)
  })
})

// 04.8.8 — the damage is measured against the RIGHT fold (D3).
//
// Both folds must read the SAME log, or the subtraction is meaningless. Until
// 04.8 the pool folded ODOMETER and was compared with odometer-read, which
// folds ODOMETER too, so it happened to work. Lesson 02 now has its own log,
// ODOMETER_POOL, and odometer-read has never seen a single event from it --
// subtracting it would report the whole pool total as damage.
//
// The correct fold is odometer-pool-truth: the same log, folded one message at
// a time, in order, by a consumer capped at one.
describe('foldDamage', () => {
  const pool = [{ id: 'pool-01', totalKm: 41902, lastSeq: 118 }]
  const truth = [{ id: 'pool-01', totalKm: 42180, lastSeq: 118 }]

  it('reports the drift between the pool fold and the correct one', () => {
    const d = foldDamage(pool, truth)
    expect(d.poolKm).toBe(41902)
    expect(d.truthKm).toBe(42180)
    expect(d.drift).toBe(-278)
  })

  // The read model is a different question about a different log. Naming it
  // here at all was the defect.
  it('has no notion of a read model left in what it reports', () => {
    const d = foldDamage(pool, truth)
    expect(Object.keys(d).sort()).toEqual(['damaged', 'drift', 'poolKm', 'truthKm', 'vehicles'])
    expect(d.readKm).toBeUndefined()
  })

  it('is short, never over — a dropped event is gone, it is never counted twice', () => {
    expect(foldDamage(pool, truth).drift).toBeLessThan(0)
  })

  it('reports no damage when the two folds agree', () => {
    const d = foldDamage(truth, truth)
    expect(d.drift).toBe(0)
    expect(d.damaged).toBe(false)
  })

  it('refuses to compare against a correct fold that is not there', () => {
    expect(foldDamage(pool, [])).toBeNull()
  })

  it('refuses to compare when the pool has folded nothing', () => {
    expect(foldDamage([], truth)).toBeNull()
  })
})

describe('poolLaneRows', () => {
  // A worker heartbeat carries `holding` — the sequence in flight — and
  // nothing else positional. It does NOT carry "the last sequence I acked".
  // So a waiting worker has no honest place on the lane, and drawing it at
  // sequence 0 would say it is 30 events behind when it is simply idle.
  // It is left out instead.
  it('draws the log, then the pool fold, then one row per worker in flight', () => {
    const rows = poolLaneRows(30, [W(1, { status: 'working', holding: 28 }), W(2, { status: 'working', holding: 25 })], 27)
    expect(rows[0].kind).toBe('log')
    expect(rows[1].id).toBe('pool')
    expect(rows[1].seq).toBe(27)
    expect(rows).toHaveLength(4)
    expect(rows[2].seq).toBe(28)
  })

  it('leaves out a worker holding nothing, because it has no position to draw', () => {
    const rows = poolLaneRows(30, [W(1, { status: 'working', holding: 28 }), W(2, { status: 'waiting', holding: 0 })], 27)
    expect(rows).toHaveLength(3)
  })

  it('tones a killed worker as lost, so the one bad thing does not look normal', () => {
    const rows = poolLaneRows(30, [W(1, { status: 'killed', holding: 19 })], 27)
    expect(rows[2].tone).toBe('lost')
  })

  it('tones a working worker as a worker, not as a side of CQRS', () => {
    const rows = poolLaneRows(30, [W(1, { status: 'working', holding: 19 })], 27)
    expect(rows[2].tone).toBe('worker')
  })

  it('draws only the log when nothing is running', () => {
    expect(poolLaneRows(30, [], 0).map((r) => r.kind)).toEqual(['log'])
  })
})

describe('redelivery', () => {
  it('finds the worker that went silent holding a sequence', () => {
    const r = redelivery([W(1, { acked: 9 }), W(2, { status: 'killed', holding: 94, behind: 0 })])
    expect(r.worker).toBe(2)
    expect(r.seq).toBe(94)
  })

  it('is null while nothing has been killed — there is nothing to show yet', () => {
    expect(redelivery([W(1, { status: 'working', holding: 3 })])).toBeNull()
  })

  it('is null when the pool is not running at all', () => {
    expect(redelivery([])).toBeNull()
  })
})

// An idle pool is a correct picture, not a blank one. These pin the one
// signal the panel uses to tell "nothing to fold" apart from "nothing here".
describe('poolCaughtUp', () => {
  const running = { running: true }

  it('is false when no pool is running', () => {
    expect(poolCaughtUp(100, 100, { running: false })).toBe(false)
  })

  it('is true when the fold has reached the head', () => {
    expect(poolCaughtUp(100, 100, running)).toBe(true)
  })

  it('is false when the fold is behind the head', () => {
    expect(poolCaughtUp(100, 94, running)).toBe(false)
  })

  // A fresh pool has acked nothing yet. That must not read as "caught up",
  // or the panel would tell you to seed while the pool is busy folding.
  it('is false when nothing has been folded at all', () => {
    expect(poolCaughtUp(100, 0, running)).toBe(false)
  })

  it('is false when there is no head to compare against', () => {
    expect(poolCaughtUp(0, 0, running)).toBe(false)
  })
})

// 04.8.9 — lesson 02 says which log it is reading.
//
// The lane is the one place on the panel that names a stream in prose, and it
// named ODOMETER. Lesson 02 has not folded ODOMETER since 04.8.6, so that line
// was telling the reader the wrong thing about the thing in front of them.
describe('the lane names the log lesson 02 actually folds', () => {
  it('names the pool log, never the demo own log', () => {
    const log = poolLaneRows(120, [], 118).find((r) => r.kind === 'log')
    expect(log.text).toContain(POOL_STREAM)
    expect(log.text).not.toMatch(new RegExp(`${STREAM}(?!_)`))
  })
})
