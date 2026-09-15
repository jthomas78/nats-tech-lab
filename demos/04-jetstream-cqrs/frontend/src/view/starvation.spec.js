import { describe, expect, it } from 'vitest'

import {
  STARVATION_MEASURED_AT,
  STARVATION_REPEAT,
  STARVATION_RUNS,
  STARVATION_SOURCE,
  starvationRows,
} from './starvation.js'

describe('the recorded max-pending runs', () => {
  it('names when and where they were measured', () => {
    expect(STARVATION_MEASURED_AT).toMatch(/Measured \d{4}-\d{2}-\d{2}/)
    expect(STARVATION_MEASURED_AT).toMatch(/NATS/)
  })

  it('records the command for every run, so no row is unsourced', () => {
    for (const r of STARVATION_RUNS) {
      expect(STARVATION_SOURCE.join('\n')).toContain(`-max-pending ${r.maxPending}`)
    }
  })

  it('cannot be edited into a different answer at runtime', () => {
    expect(Object.isFrozen(STARVATION_RUNS)).toBe(true)
    expect(Object.isFrozen(STARVATION_RUNS[0])).toBe(true)
  })

  it('holds every worker count fixed, so the cap is the only variable', () => {
    const counts = new Set(STARVATION_RUNS.map((r) => r.workers))
    expect(counts.size).toBe(1)
  })

  it('drains the same log every time, so the runs compare', () => {
    const events = new Set(STARVATION_RUNS.map((r) => r.events))
    expect(events.size).toBe(1)
  })

  // The finding. Every run is here to be checked against the claim the tab
  // used to make, and every run refutes it.
  it('never starved a worker, at any cap', () => {
    for (const r of STARVATION_RUNS) {
      expect(r.busy).toBe(r.workers)
    }
  })

  it('gets slower as the cap gets smaller', () => {
    const byCap = [...STARVATION_RUNS].sort((a, b) => a.maxPending - b.maxPending)
    for (let i = 1; i < byCap.length; i += 1) {
      expect(byCap[i].seconds).toBeLessThan(byCap[i - 1].seconds)
    }
  })

  it('loses less as the cap gets smaller, which is the trade', () => {
    const byCap = [...STARVATION_RUNS].sort((a, b) => a.maxPending - b.maxPending)
    for (let i = 1; i < byCap.length; i += 1) {
      expect(byCap[i].dropped).toBeGreaterThan(byCap[i - 1].dropped)
    }
  })

  it('has a cap of 1 that loses nothing at all', () => {
    const one = STARVATION_RUNS.find((r) => r.maxPending === 1)
    expect(one.dropped).toBe(0)
    expect(one.folded).toBe(one.events)
  })

  it('carries a repeat of one run, so "it reproduces" is not a claim', () => {
    expect(STARVATION_REPEAT.maxPending).toBe(3)
    expect(STARVATION_REPEAT.seconds).toBeGreaterThan(0)
    expect(STARVATION_REPEAT.dropped).toBeGreaterThan(0)
  })
})

describe('starvationRows', () => {
  it('counts idle workers, and finds none', () => {
    for (const row of starvationRows()) expect(row.idle).toBe(0)
  })

  it('measures loss as a share of what the consumer handed out', () => {
    const row = starvationRows().find((r) => r.maxPending === 1000)
    expect(row.lossPct).toBeCloseTo((row.dropped / row.events) * 100, 5)
  })

  it('reads speed against the smallest cap, because that is the correct run', () => {
    const rows = starvationRows()
    const one = rows.find((r) => r.maxPending === 1)
    expect(one.control).toBe(true)
    expect(one.speedup).toBe(1)
    expect(rows.find((r) => r.maxPending === 1000).speedup).toBeGreaterThan(1)
  })

  it('gives the bar a width against the slowest run', () => {
    const rows = starvationRows()
    expect(Math.max(...rows.map((r) => r.barPct))).toBe(100)
    for (const r of rows) expect(r.barPct).toBeGreaterThan(0)
  })
})
