import { describe, expect, it } from 'vitest'

import { compactCount, exactCount } from './format'

// The summary rows in ConnectionsPanel/ServicesPanel use ONE value type size,
// so these helpers are what keeps an unbounded counter from being the reason a
// card wraps — the boundary at 100,000 is the point where the grouped figure
// stops fitting a card at 20px.

describe('compactCount', () => {
  it('keeps counts under 100,000 exact and grouped', () => {
    expect(compactCount(0)).toBe('0')
    expect(compactCount(774)).toBe('774')
    expect(compactCount(2872)).toBe('2,872')
    expect(compactCount(99999)).toBe('99,999')
  })

  it('switches to magnitude at 100,000 and above', () => {
    expect(compactCount(100000)).toBe('100K')
    expect(compactCount(1234567)).toBe('1.2M')
    expect(compactCount(8901234)).toBe('8.9M')
  })

  it('treats null/undefined as zero rather than rendering NaN in a card', () => {
    expect(compactCount(null)).toBe('0')
    expect(compactCount(undefined)).toBe('0')
  })
})

describe('exactCount', () => {
  it('always groups the full figure, for tooltips', () => {
    expect(exactCount(8901234)).toBe('8,901,234')
    expect(exactCount(null)).toBe('0')
  })
})
