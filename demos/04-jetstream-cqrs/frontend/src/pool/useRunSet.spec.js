import { beforeEach, describe, expect, it } from 'vitest'

import { useRunSet } from './useRunSet.js'

// A set of runs, one after another (plan 04.9.6 and 04.9.7, decisions D5, D7,
// D12).
//
// Starvation varies the cap, Performance varies the worker count, and the
// SEQUENCE is the same for both: seed, run, record, re-seed, run again. So it
// is written once. Two copies would drift, and the tab that drifted would be
// the one nobody re-read.
//
// D7 is why the re-seed is here and not left to the reader: each run DRAINS
// the log. Without a re-seed, run 2 folds an empty stream and reports a
// beautiful, meaningless zero.
//
// D5 is why a row starts empty and only fills from a result: a row that kept
// a recorded number as a fallback would show numbers after a run that failed.

const store = new Map()
const session = {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: (k) => store.delete(k),
}

// A fake shim that records the order it was asked to do things.
const shim = (over = {}) => {
  const log = []
  return {
    log,
    seed: async (size) => { log.push(`seed ${size}`); return { kind: 'ok', events: size } },
    run: async (cfg) => {
      log.push(`run ${cfg.maxPending}`)
      return over.run
        ? over.run(cfg)
        : { kind: 'ok', events: 10_000, acked: 10_000, dropped: 0, seconds: 10, rate: 1000 }
    },
  }
}

const PLANS = [{ maxPending: 1 }, { maxPending: 3 }]

const make = (s, over = {}) =>
  useRunSet({
    label: 'Starvation',
    events: 10_000,
    ackWaitMs: 30_000,
    plans: PLANS,
    seed: s.seed,
    run: s.run,
    session,
    ...over,
  })

beforeEach(() => store.clear())

describe('before anybody presses Run', () => {
  it('has one empty row per plan, and fills none of them', () => {
    const set = make(shim())
    expect(set.rows.value).toHaveLength(2)
    expect(set.rows.value.every((r) => r.result === null)).toBe(true)
  })

  it('keeps the plan on the row, so an empty row still says what it will do', () => {
    expect(make(shim()).rows.value[1].plan.maxPending).toBe(3)
  })
})

describe('the sequence one press makes', () => {
  it('runs each plan once, re-seeding between them but not before the first', async () => {
    const s = shim()
    await make(s).press()
    expect(s.log).toEqual(['run 1', 'seed 10000', 'run 3'])
  })

  it('fills each row as its run ends, in order', async () => {
    const s = shim()
    const set = make(s)
    await set.press()
    expect(set.rows.value.map((r) => r.result?.rate)).toEqual([1000, 1000])
  })

  it('is finished, and says nothing is running, once the set ends', async () => {
    const set = make(shim())
    await set.press()
    expect(set.running.value).toBe(false)
    expect(set.progress.percent.value).toBe(100)
  })
})

describe('a run that does not come back clean', () => {
  // D5 again: a set that failed halfway leaves the rows it never ran EMPTY.
  // Filling them from anything at all would be a screen inventing a number.
  it('stops, keeps the rows it did fill, and leaves the rest empty', async () => {
    const s = shim({ run: async (cfg) => (cfg.maxPending === 3
      ? { kind: 'broken', error: 'Unreachable', message: 'no answer' }
      : { kind: 'ok', events: 10_000, acked: 10_000, dropped: 0, seconds: 10, rate: 1000 }) })
    const set = make(s)
    await set.press()
    expect(set.rows.value[0].result).not.toBeNull()
    expect(set.rows.value[1].result).toBeNull()
    expect(set.broken.value.message).toContain('no answer')
    expect(set.running.value).toBe(false)
  })

  // D8 — a 409 is somebody else measuring, not a fault, and it is worth
  // saying in the words the shim used.
  it('says who is holding the shim when it is refused', async () => {
    const s = shim({ run: async () => ({ kind: 'busy', error: 'PoolRunning', message: '8 workers since 12:01' }) })
    const set = make(s)
    await set.press()
    expect(set.broken.value.message).toContain('8 workers')
    expect(set.rows.value.every((r) => r.result === null)).toBe(true)
  })

  it('stops if the re-seed fails, rather than measuring an empty log', async () => {
    const s = shim()
    s.seed = async () => ({ kind: 'broken', error: 'Unreachable', message: 'seed failed' })
    const set = make(s)
    await set.press()
    expect(set.rows.value[0].result).not.toBeNull()
    expect(set.rows.value[1].result).toBeNull()
    expect(set.broken.value.message).toContain('seed failed')
  })
})

describe('pressing it twice', () => {
  // One press, one set. A second press while the first is still going would
  // be refused by the shim anyway -- but the screen should not have offered.
  it('ignores a press while the set is already running', async () => {
    const s = shim()
    const set = make(s)
    const first = set.press()
    await set.press()
    await first
    expect(s.log.filter((l) => l.startsWith('run'))).toHaveLength(2)
  })

  // A second press is a fresh measurement, not an addition to the last one.
  it('clears the old results before running again', async () => {
    const s = shim()
    const set = make(s)
    await set.press()
    const second = set.press()
    expect(set.rows.value.every((r) => r.result === null)).toBe(true)
    await second
  })
})
