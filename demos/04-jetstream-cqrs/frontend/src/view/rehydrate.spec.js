import { describe, expect, it } from 'vitest'

import { agree, formatMs, formatSpeedup, speedup, trailsBy, verdict } from './rehydrate.js'
import { formatCount } from './format.js'

const cold = { elapsedMs: 25, eventsRead: 10001, status: 'registered', plate: 'ABC-123' }
const warm = { elapsedMs: 1.1, eventsRead: 2, status: 'registered', plate: 'ABC-123' }

describe('formatMs', () => {
  it('keeps two decimals under a millisecond, which is the snapshot side', () => {
    expect(formatMs(0.42)).toBe('0.42')
  })

  it('keeps one decimal in single digits', () => {
    expect(formatMs(1.14)).toBe('1.1')
  })

  it('drops decimals once nobody is arguing about them', () => {
    expect(formatMs(25.4)).toBe('25')
  })

  it('draws a dash rather than a wrong number', () => {
    expect(formatMs(undefined)).toBe('—')
    expect(formatMs(-1)).toBe('—')
  })
})

describe('agree', () => {
  it('is true when both sides rebuilt the same state', () => {
    expect(agree(cold, warm)).toBe(true)
  })

  it('is false when the states differ', () => {
    expect(agree(cold, { ...warm, status: 'retired' })).toBe(false)
  })

  // The two runs are milliseconds apart. A trip in between is the log
  // working, not a disagreement.
  it('ignores a lastSeq that moved between the two runs', () => {
    expect(agree({ ...cold, lastSeq: 10003 }, { ...warm, lastSeq: 10004 })).toBe(true)
  })
})

describe('speedup', () => {
  it('reports how many times faster the snapshot side was', () => {
    expect(speedup(cold, warm)).toBeCloseTo(22.7, 1)
  })

  it('refuses to divide by a zero timing', () => {
    expect(speedup(cold, { ...warm, elapsedMs: 0 })).toBeNull()
  })

  it('has no answer until both sides have run', () => {
    expect(speedup(cold, null)).toBeNull()
  })
})

describe('formatSpeedup', () => {
  it('keeps a decimal in single digits', () => {
    expect(formatSpeedup(4.25)).toBe('4.3x')
  })

  it('rounds above ten, because the last digit is not the point', () => {
    expect(formatSpeedup(22.7)).toBe('23x')
  })

  it('prints nothing when there is no honest ratio', () => {
    expect(formatSpeedup(null)).toBe('')
  })
})

describe('verdict', () => {
  it('asks for both sides before it says anything', () => {
    expect(verdict(cold, null).kind).toBe('partial')
  })

  // The important one. A disagreement must never be dressed up as a result.
  it('voids the comparison when the two sides disagree', () => {
    const v = verdict(cold, { ...warm, status: 'retired' })
    expect(v.kind).toBe('void')
    expect(v.ratio).toBeUndefined()
  })

  it('says so when a side was too fast to time', () => {
    expect(verdict(cold, { ...warm, elapsedMs: 0 }).kind).toBe('unmeasurable')
  })

  it('reports the measurement and the events saved', () => {
    const v = verdict(cold, warm)
    expect(v.kind).toBe('measured')
    expect(v.ratio).toBeCloseTo(22.7, 1)
    // Grouped, like every other count this demo prints. formatCount groups
    // with a non-breaking thin space, so spelling it by hand here would pass
    // for the wrong reason.
    expect(v.text).toContain(`${formatCount(9999)} fewer events`)
    expect(v.text).not.toContain('9999 ')
  })
})

describe('trailsBy', () => {
  it('is how many events the snapshot side had to replay', () => {
    expect(trailsBy(warm)).toBe(2)
  })

  it('is zero, never negative, when there is nothing to say', () => {
    expect(trailsBy(null)).toBe(0)
    expect(trailsBy({ eventsRead: -4 })).toBe(0)
  })
})
