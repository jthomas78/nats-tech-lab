// One place to answer "how long did this span take" and "when did it start".
//
// It exists because several consumers once answered the second question
// independently and were all wrong in the same way (BR-056). In the gateway
// view `timestamp` is a span's FINISH moment, so a start is finish minus
// duration. That subtraction is subtle enough to deserve one implementation.

// spanDurationMs reads a span's own measured duration, in milliseconds —
// fractional, because the field is microseconds (BR-056). The trace gateway
// view gives `durationUs` as (end − start) / 1000 (BR-076).
//
// Milliseconds is the DISPLAY unit throughout this UI and stays so.
export function spanDurationMs(span) {
  return typeof span?.durationUs === 'number' ? span.durationUs / 1000 : 0
}

// spanFinishMs parses the span's finish timestamp WITHOUT losing its
// sub-millisecond part. Date.parse() truncates at milliseconds, which would
// re-introduce on the read side exactly the precision loss BR-056 removed
// from the write side.
export function spanFinishMs(span) {
  const iso = span?.timestamp
  if (!iso) return 0
  const match = /\.(\d+)Z$/.exec(iso)
  if (!match) return new Date(iso).getTime()
  const wholeSecondMs = new Date(iso.slice(0, match.index) + 'Z').getTime()
  const fractionMs = Number(match[1].padEnd(9, '0').slice(0, 9)) / 1e6
  return wholeSecondMs + fractionMs
}

// spanStartMs is the derived value everything above exists to protect.
//
// Both operands must be precise or the result is biased in one direction:
// a truncated duration subtracted from a precise finish puts the start too
// LATE, never too early, by up to the truncation. Within a trace the
// outermost span has run longest and carries the largest discarded
// remainder, so it is pushed furthest right — which is how a root span came
// to be drawn starting after its own grandchild.
export function spanStartMs(span) {
  return spanFinishMs(span) - spanDurationMs(span)
}
