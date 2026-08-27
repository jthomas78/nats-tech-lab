import { describe, expect, it } from 'vitest'

import { NKEY_GLYPH, compactCount, elideNKey, exactCount } from './format'

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

// BR-061 — an NKey is never rendered in full. These are the boundary cases a
// component spec covers badly, so they live with the helper: what happens to a
// value too short to elide, to nothing at all, and — the assertion that is
// actually the rule rather than the format — that the middle never survives.
describe('elideNKey (BR-061)', () => {
  const REAL = 'ADD65MOJPAWSPKI4EAGTJXBWRWRXTEGKMSHTMDXHVDCH2Q2RTQM'

  it('renders a real NKey as [FIRST5...LAST5] with the literal three-period glyph', () => {
    expect(elideNKey(REAL)).toBe('[ADD65...2RTQM]')
    expect(NKEY_GLYPH).toBe('...')
    // Not the single ellipsis character the old per-panel truncations used.
    expect(elideNKey(REAL)).not.toContain('…')
  })

  it('never lets the middle of the key reach the screen', () => {
    const out = elideNKey(REAL)
    expect(out).not.toContain(REAL.slice(5, -5))
    expect(out).not.toContain(REAL)
    // Ten significant characters, no more: the key is for recognition here.
    expect(out.replace(/[^A-Z0-9]/g, '')).toHaveLength(10)
  })

  it('returns a value too short to elide unchanged, rather than a longer "short" form', () => {
    expect(elideNKey('UASXO6QQZGVB')).toBe('UASXO6QQZGVB')
    expect(elideNKey('ABC')).toBe('ABC')
  })

  it('renders nothing for an absent key instead of empty brackets', () => {
    expect(elideNKey('')).toBe('')
    expect(elideNKey(null)).toBe('')
    expect(elideNKey(undefined)).toBe('')
  })
})
