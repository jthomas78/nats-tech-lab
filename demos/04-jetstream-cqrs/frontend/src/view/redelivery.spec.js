import { describe, expect, it } from 'vitest'

import {
  REDELIVERY_RUNS,
  REDELIVERY_MEASURED_AT,
  REDELIVERY_SOURCE,
  redeliveryRows,
} from './redelivery.js'

describe('the recorded redelivery runs', () => {
  it('names when and where they were measured', () => {
    expect(REDELIVERY_MEASURED_AT).toMatch(/Measured \d{4}-\d{2}-\d{2}/)
    expect(REDELIVERY_MEASURED_AT).toMatch(/NATS/)
  })

  it('keeps the commands as they were typed, one per line', () => {
    expect(REDELIVERY_SOURCE.length).toBeGreaterThan(0)
    for (const line of REDELIVERY_SOURCE) {
      expect(line).toMatch(/^cqrs /)
      expect(line).not.toContain('\n')
    }
  })

  it('records the kill command for every run, so no row is unsourced', () => {
    for (const r of REDELIVERY_RUNS) {
      expect(REDELIVERY_SOURCE.join('\n')).toContain(`-kill-at ${r.killSeq}`)
    }
  })

  it('cannot be edited into a different answer at runtime', () => {
    expect(Object.isFrozen(REDELIVERY_RUNS)).toBe(true)
    expect(Object.isFrozen(REDELIVERY_RUNS[0])).toBe(true)
  })

  it('has more than one AckWait, because one run cannot show a pattern', () => {
    const waits = new Set(REDELIVERY_RUNS.map((r) => r.ackWait))
    expect(waits.size).toBeGreaterThan(1)
  })

  it('redelivers to a worker that is not the one that went silent', () => {
    for (const r of REDELIVERY_RUNS) {
      expect(r.toWorker).not.toBe(r.killedWorker)
    }
  })

  it('waited the AckWait out, to within a small margin', () => {
    for (const r of REDELIVERY_RUNS) {
      const wait = Number(r.ackWait.replace('s', ''))
      expect(r.waitedSeconds).toBeGreaterThanOrEqual(wait)
      expect(r.waitedSeconds - wait).toBeLessThan(0.5)
    }
  })

  it('shows the fold had already moved past the killed sequence', () => {
    for (const r of REDELIVERY_RUNS) {
      expect(r.foldAt).toBeGreaterThan(r.killSeq)
      expect(r.outcome).toBe('dropped')
    }
  })
})

describe('redeliveryRows', () => {
  it('says the wait was exactly the AckWait, not a guess', () => {
    const [row] = redeliveryRows()
    expect(row.overshoot).toBeCloseTo(row.waitedSeconds - 30, 2)
    expect(row.handoff).toBe('3 → 4')
  })

  it('counts how far the fold ran on while the event was silent', () => {
    for (const row of redeliveryRows()) {
      expect(row.ranOn).toBe(row.foldAt - row.killSeq)
      expect(row.ranOn).toBeGreaterThan(0)
    }
  })

  it('says plainly that the wait bought nothing', () => {
    for (const row of redeliveryRows()) {
      expect(row.recovered).toBe(false)
    }
  })
})
