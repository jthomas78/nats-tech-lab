import { describe, expect, it } from 'vitest'

import {
  ackBars,
  foldDamage,
  poolHealth,
  poolLaneRows,
  redelivery,
  workerRows,
} from './pool.js'

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

describe('foldDamage', () => {
  const pool = [{ id: 'truck-7', totalKm: 41902, lastSeq: 118 }]
  const read = [{ id: 'truck-7', totalKm: 42180, lastSeq: 118 }]

  it('reports the drift between the pool fold and the correct one', () => {
    const d = foldDamage(pool, read)
    expect(d.poolKm).toBe(41902)
    expect(d.readKm).toBe(42180)
    expect(d.drift).toBe(-278)
  })

  it('is short, never over — a dropped event is gone, it is never counted twice', () => {
    expect(foldDamage(pool, read).drift).toBeLessThan(0)
  })

  it('reports no damage when the two folds agree', () => {
    const d = foldDamage(read, read)
    expect(d.drift).toBe(0)
    expect(d.damaged).toBe(false)
  })

  it('refuses to compare against a read model that is not there', () => {
    expect(foldDamage(pool, [])).toBeNull()
  })

  it('refuses to compare when the pool has folded nothing', () => {
    expect(foldDamage([], read)).toBeNull()
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
