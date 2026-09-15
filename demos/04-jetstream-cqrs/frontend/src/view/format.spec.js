import { describe, expect, it } from 'vitest'

import { foldedInto, formatClock, formatCount, formatKm } from './format.js'

const THIN = ' '

describe('formatKm', () => {
  it('always shows one decimal', () => {
    expect(formatKm(42)).toBe('42.0')
  })

  it('groups the thousands', () => {
    expect(formatKm(48210.44)).toBe(`48${THIN}210.4`)
  })

  it('draws a missing number as zero rather than NaN', () => {
    expect(formatKm(undefined)).toBe('0.0')
  })
})

describe('formatCount', () => {
  it('groups the thousands', () => {
    expect(formatCount(10000)).toBe(`10${THIN}000`)
  })

  it('drops a fraction — a trip count is whole', () => {
    expect(formatCount(12.9)).toBe('12')
  })
})

describe('formatClock', () => {
  it('shows the time of day only', () => {
    expect(formatClock(new Date(2026, 8, 15, 10, 54, 2))).toBe('10:54:02')
  })

  it('draws an empty timestamp as a dash', () => {
    expect(formatClock('')).toBe('—')
  })

  it('draws rubbish as a dash, never as Invalid Date', () => {
    expect(formatClock('not a time')).toBe('—')
  })

  it("says never for Go's zero time, not a clock reading", () => {
    expect(formatClock('0001-01-01T00:00:00Z')).toBe('(never)')
  })
})

describe('foldedInto', () => {
  it('says neither side when the event is newer than both', () => {
    expect(foldedInto(129, { writeSeq: 124, readSeq: 126 })).toEqual([])
  })

  it('says the read side only when it is ahead of the write side', () => {
    expect(foldedInto(126, { writeSeq: 124, readSeq: 126 })).toEqual(['read'])
  })

  it('says both sides once the event is behind both', () => {
    expect(foldedInto(124, { writeSeq: 124, readSeq: 126 })).toEqual(['write', 'read'])
  })

  it('says neither side when no positions are known', () => {
    expect(foldedInto(1)).toEqual([])
  })
})
