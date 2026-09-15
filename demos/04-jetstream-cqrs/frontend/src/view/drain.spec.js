// These specs guard a table of recorded numbers. They are deliberately picky
// about arithmetic that must not drift, and about the fact that every row
// accounts for itself.

import { describe, expect, it } from 'vitest'

import { DRAIN_RUNS, drainRows } from './drain.js'

describe('DRAIN_RUNS — the recorded measurement', () => {
  it('has the four runs the README prints', () => {
    expect(DRAIN_RUNS.map((r) => r.workers)).toEqual([1, 2, 4, 8])
  })

  // If a row does not add up, somebody has typed a number rather than read it
  // off a run.
  it('accounts for every event it was handed', () => {
    for (const r of DRAIN_RUNS) {
      expect(r.folded + r.dropped).toBe(r.events)
    }
  })

  it('has one worker drop nothing — one worker cannot race itself', () => {
    expect(DRAIN_RUNS[0].dropped).toBe(0)
  })
})

describe('drainRows', () => {
  it('measures speed against the one-worker run', () => {
    const rows = drainRows()
    expect(rows[0].speedup).toBe(1)
    expect(rows[2].speedup).toBeCloseTo(3.73, 1)
  })

  it('reports loss as a share of the events handed out', () => {
    const rows = drainRows()
    expect(rows[0].lossPct).toBe(0)
    expect(rows[2].lossPct).toBeCloseTo(30.7, 1)
    expect(rows[3].lossPct).toBeCloseTo(56.7, 1)
  })

  it('draws the slowest run as a full track and the rest shorter', () => {
    const rows = drainRows()
    expect(rows[0].barPct).toBe(100)
    expect(rows[3].barPct).toBeCloseTo(20.4, 1)
  })

  it('marks the control row, so the table can say which one it is', () => {
    expect(drainRows().map((r) => r.control)).toEqual([true, false, false, false])
  })

  it('survives an empty table rather than dividing by a missing baseline', () => {
    expect(drainRows([])).toEqual([])
  })
})
