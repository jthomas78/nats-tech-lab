// The comparison, as numbers and words. No fetching, no Vue.
//
// This is lesson 01's payoff: "rehydrating an aggregate — how much does a
// snapshot buy you?" The two halves are the SAME rebuild of the SAME vehicle,
// one from sequence 1 and one from the snapshot plus the tail.
//
// Two rules of honesty live here, and both were learned the hard way in
// 04.7.12, when the demo's headline number turned out to be mostly client
// overhead:
//
//   1. A ratio is only reported when both halves actually ran and both
//      timings are real. A speed-up computed against a zero is a made-up
//      number, and this demo has already published one of those.
//   2. The two halves must be shown to AGREE. If they rebuilt different
//      states, the comparison is void and the screen says so instead of
//      printing a flattering multiple.
//
// It imports formatCount for one reason: a count this module prints must be
// grouped the same way a count the components print is. An ungrouped 9950 in
// the middle of a screen full of grouped numbers reads as a different kind of
// number.

import { formatCount } from './format.js'

// formatMs prints a duration the way the demo argues with it.
//
// Sub-millisecond results are the snapshot side's whole point, so they keep
// two decimals; anything past 10 ms keeps none, because nobody is arguing
// about a tenth of a millisecond at that size.
export function formatMs(ms) {
  const n = Number(ms)
  if (!Number.isFinite(n) || n < 0) return '—'
  if (n < 1) return n.toFixed(2)
  if (n < 10) return n.toFixed(1)
  return String(Math.round(n))
}

// agree reports whether both halves rebuilt the same vehicle.
//
// lastSeq is deliberately NOT compared. The two runs happen a few
// milliseconds apart, so a trip recorded in between moves one side on — that
// is the log working, not a disagreement. The STATE is what a rule is checked
// against, and the state is what has to match.
export function agree(cold, warm) {
  if (!cold || !warm) return true
  return cold.status === warm.status && cold.plate === warm.plate
}

// speedup reports how many times faster the snapshot side was.
//
// It returns null rather than a number whenever the honest answer is "we
// cannot say": a half is missing, or a timing is zero or negative. A caller
// that gets null prints nothing, and that is the correct outcome.
export function speedup(cold, warm) {
  if (!cold || !warm) return null
  const a = Number(cold.elapsedMs)
  const b = Number(warm.elapsedMs)
  if (!Number.isFinite(a) || !Number.isFinite(b) || a <= 0 || b <= 0) return null
  return a / b
}

// formatSpeedup prints the ratio. One decimal under 10, none above — "23x"
// reads as a measurement and "23.4x" reads as a claim about the last digit.
export function formatSpeedup(ratio) {
  if (ratio === null || !Number.isFinite(ratio) || ratio <= 0) return ''
  return ratio < 10 ? `${ratio.toFixed(1)}x` : `${Math.round(ratio)}x`
}

// verdict is the sentence under the two cards.
//
// The four kinds are separate because they need different words, not
// different adjectives. `void` is the one that matters: a comparison whose
// halves disagree must not be dressed up as a slow one.
export function verdict(cold, warm) {
  if (!cold || !warm) return { kind: 'partial', text: 'Run both sides to compare them.' }
  if (!agree(cold, warm)) {
    return {
      kind: 'void',
      text: 'The two sides rebuilt different states, so this comparison means nothing. The snapshot is stale or corrupt — rebuild it before reading these numbers.',
    }
  }
  const ratio = speedup(cold, warm)
  if (ratio === null) {
    return {
      kind: 'unmeasurable',
      text: 'Both sides rebuilt the same state, but one of them was too fast to time. Seed more events and run it again.',
    }
  }
  const saved = Number(cold.eventsRead) - Number(warm.eventsRead)
  return {
    kind: 'measured',
    ratio,
    text: `Both sides rebuilt the same state. The snapshot side read ${formatCount(saved)} fewer events. The gap grows with the log: the cold side gets slower every time you record a trip, and the snapshot side does not.`,
  }
}

// trailsBy is how far the snapshot was behind the log when it was read.
//
// CLAUDE.md: "the snapshot is always stale". This is that staleness as a
// number, and it is never negative — a snapshot ahead of the tail it replayed
// is impossible, and clamping keeps a transient read from printing nonsense.
export function trailsBy(warm) {
  if (!warm) return 0
  return Math.max(0, Number(warm.eventsRead) || 0)
}
