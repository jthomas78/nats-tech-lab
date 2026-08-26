import { describe, expect, it } from 'vitest'

import { spanDurationMs, spanFinishMs, spanStartMs } from './spanTiming.js'

// BR-056 — one seam for "how long did this span take" and "when did it
// start", because three consumers derived a start time independently
// (TraceWaterfall, PulsePanel, and otlp-bridge on the Go side) and all three
// were wrong in the same way at once.
describe('spanTiming (BR-056)', () => {
  const at = (isoFraction) => `2026-08-26T12:00:00.${isoFraction}Z`

  describe('spanDurationMs', () => {
    it('reads durationUs, in milliseconds, keeping the sub-millisecond part', () => {
      expect(spanDurationMs({ durationUs: 1437 })).toBeCloseTo(1.437, 6)
    })

    it('treats 0us as a measured duration, not as absence', () => {
      expect(spanDurationMs({ durationUs: 0, durationMs: 9 })).toBe(0)
    })

    it('falls back to a legacy durationMs record so a deploy is invisible', () => {
      expect(spanDurationMs({ durationMs: 41 })).toBe(41)
    })

    it('prefers durationUs when a record somehow carries both', () => {
      expect(spanDurationMs({ durationUs: 1437, durationMs: 1 })).toBeCloseTo(1.437, 6)
    })

    it('reads a span with neither field as zero rather than NaN', () => {
      expect(spanDurationMs({})).toBe(0)
    })
  })

  describe('spanFinishMs', () => {
    it('keeps the timestamp fraction below millisecond resolution', () => {
      // .123456789 -> 123.456789ms past the second. Date.parse() alone
      // truncates this to 123.
      const ms = spanFinishMs({ timestamp: at('123456789') })
      expect(ms - Math.floor(ms / 1000) * 1000).toBeCloseTo(123.456789, 5)
    })

    it('still reads a timestamp with no fractional part', () => {
      expect(spanFinishMs({ timestamp: '2026-08-26T12:00:00Z' })).toBe(Date.parse('2026-08-26T12:00:00Z'))
    })
  })

  describe('spanStartMs', () => {
    // The defect this whole rule exists for, stated as arithmetic: a span
    // that truly ran 1.9ms reported 1ms, so its start was derived 0.9ms LATE.
    // Two nested spans both reporting 1ms therefore derived starts in the
    // wrong ORDER, because the outer one had the larger truncated remainder.
    it('derives a start that is finish minus the real duration, not the truncated one', () => {
      const parent = { timestamp: at('001900000'), durationUs: 1900 }
      const child = { timestamp: at('001500000'), durationUs: 1200 }

      expect(spanStartMs(parent)).toBeLessThan(spanStartMs(child))
    })

    it('inverts exactly as observed if the duration is only millisecond-resolved', () => {
      // The regression this guards, kept as an executable statement of it:
      // the same two spans with durationMs alone derive backwards.
      const parent = { timestamp: at('001900000'), durationMs: 1 }
      const child = { timestamp: at('001500000'), durationMs: 1 }

      expect(spanStartMs(parent)).toBeGreaterThan(spanStartMs(child))
    })
  })
})
