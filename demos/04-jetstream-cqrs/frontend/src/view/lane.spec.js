import { describe, expect, it } from 'vitest'

import { AXIS, labelPlacement, laneDescription, lanePoints, positionOf } from './lane.js'

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
