import { describe, expect, it } from 'vitest'

import { AXIS, ROW, labelPlacement, laneDescription, laneRows, lanePoints, positionOf } from './lane.js'

describe('positionOf', () => {
  it('puts the stream head at the right end', () => {
    expect(positionOf(128, 128)).toBe(AXIS.x1)
  })

  it('puts the middle of the log in the middle of the axis', () => {
    expect(positionOf(64, 128)).toBe((AXIS.x0 + AXIS.x1) / 2)
  })

  it('puts an empty stream at the left end instead of dividing by zero', () => {
    expect(positionOf(0, 0)).toBe(AXIS.x0)
  })

  it('clamps a side that is ahead of this browser back to the head', () => {
    expect(positionOf(130, 128)).toBe(AXIS.x1)
  })
})

describe('lanePoints', () => {
  it('draws both sides behind the head, each at its own distance', () => {
    const p = lanePoints({ head: 128, writeSeq: 124, readSeq: 126 })
    expect(p.write.lag).toBe(4)
    expect(p.read.lag).toBe(2)
    expect(p.write.x).toBeLessThan(p.read.x)
    expect(p.read.x).toBeLessThan(AXIS.x1)
  })

  it('draws both markers on the head when everything is caught up', () => {
    const p = lanePoints({ head: 9, writeSeq: 9, readSeq: 9 })
    expect(p.write.x).toBe(AXIS.x1)
    expect(p.read.x).toBe(AXIS.x1)
    expect(p.write.lag).toBe(0)
  })

  it('survives being given nothing at all', () => {
    const p = lanePoints()
    expect(p.head).toBe(0)
    expect(p.write.x).toBe(AXIS.x0)
  })
})

describe('laneDescription', () => {
  it('reads out all three positions', () => {
    const text = laneDescription(lanePoints({ head: 128, writeSeq: 124, readSeq: 126 }), 'truck-7')
    expect(text).toContain('from 1 to 128')
    expect(text).toContain('truck-7')
    expect(text).toContain('sequence 124, 4 behind')
    expect(text).toContain('sequence 126, 2 behind')
  })

  it('says so plainly when the stream is empty', () => {
    expect(laneDescription(lanePoints(), 'all vehicles')).toContain('Nothing on the stream yet')
  })
})

describe('labelPlacement', () => {
  it('puts the label left of a marker that has room', () => {
    expect(labelPlacement(900)).toEqual({ anchor: 'end', x: 892 })
  })

  it('flips the label right of a marker sitting at sequence 1', () => {
    expect(labelPlacement(AXIS.x0)).toEqual({ anchor: 'start', x: AXIS.x0 + 8 })
  })

  it('keeps both labels inside the drawing on an empty stream', () => {
    const p = lanePoints()
    expect(p.write.label.anchor).toBe('start')
    expect(p.read.label.anchor).toBe('start')
  })
})

// Phase 04.7 — two markers become N. A worker pool has one row per worker, and
// the pool panel does not know how many there are until it reads the bucket.
describe('laneRows', () => {
  const rows = (n) =>
    Array.from({ length: n }, (_, i) => ({ id: `worker.${i}`, text: `worker ${i}`, seq: 10 + i }))

  it('gives every row its own line, evenly spaced', () => {
    const p = laneRows({ head: 20, rows: rows(4) })
    expect(p.rows).toHaveLength(4)
    expect(p.rows.map((r) => r.y)).toEqual([ROW.top, ROW.top + ROW.gap, ROW.top + 2 * ROW.gap, ROW.top + 3 * ROW.gap])
  })

  it('gives every row its own lag', () => {
    const p = laneRows({ head: 20, rows: rows(3) })
    expect(p.rows.map((r) => r.lag)).toEqual([10, 9, 8])
  })

  it('grows the drawing with the row count', () => {
    const small = laneRows({ head: 20, rows: rows(2) })
    const big = laneRows({ head: 20, rows: rows(9) })
    expect(big.axisY).toBeGreaterThan(small.axisY)
    expect(big.height).toBe(big.axisY + 16)
  })

  it('draws a log row full width whatever its sequence says', () => {
    const p = laneRows({ head: 20, rows: [{ id: 'log', kind: 'log', text: 'ODOMETER', seq: 3 }] })
    expect(p.rows[0].x).toBe(AXIS.x1)
    expect(p.rows[0].seq).toBe(20)
    expect(p.rows[0].lag).toBe(0)
  })

  it('places each row label on its own side of its own marker', () => {
    const p = laneRows({ head: 100, rows: [{ id: 'a', text: 'a', seq: 1 }, { id: 'b', text: 'b', seq: 100 }] })
    expect(p.rows[0].label.anchor).toBe('start')
    expect(p.rows[1].label.anchor).toBe('end')
  })

  it('keeps the four-worker drawing the same shape as one worker, only taller', () => {
    const one = laneRows({ head: 20, rows: rows(1) })
    expect(one.rows[0].y).toBe(ROW.top)
    expect(one.axisY).toBe(ROW.top + 18)
  })

  it('survives being given nothing at all', () => {
    const p = laneRows()
    expect(p.rows).toEqual([])
    expect(p.head).toBe(0)
    expect(p.axisY).toBeGreaterThan(0)
  })

  it('carries a row tone through untouched, so the drawing can colour it', () => {
    const p = laneRows({ head: 9, rows: [{ id: 'w', text: 'w', seq: 9, tone: 'killed' }] })
    expect(p.rows[0].tone).toBe('killed')
  })
})

describe('the three-row lane is the two-marker lane', () => {
  it('puts write, log and read exactly where the drawing already has them', () => {
    const p = lanePoints({ head: 128, writeSeq: 124, readSeq: 126 })
    expect(p.rows.map((r) => r.y)).toEqual([18, 44, 70])
    expect(p.axisY).toBe(88)
    expect(p.height).toBe(104)
  })
})

describe('laneDescription with N rows', () => {
  it('reads out every worker, not just two', () => {
    const p = laneRows({
      head: 30,
      rows: [
        { id: 'log', kind: 'log', text: 'ODOMETER' },
        { id: 'w0', text: 'worker 0', seq: 28 },
        { id: 'w1', text: 'worker 1', seq: 25 },
        { id: 'w2', text: 'worker 2', seq: 30 },
      ],
    })
    const text = laneDescription(p, 'the pool')
    expect(text).toContain('from 1 to 30')
    expect(text).toContain('worker 0 sits at sequence 28, 2 behind')
    expect(text).toContain('worker 1 sits at sequence 25, 5 behind')
    expect(text).toContain('worker 2 sits at sequence 30, 0 behind')
    expect(text).not.toContain('ODOMETER sits at')
  })
})
