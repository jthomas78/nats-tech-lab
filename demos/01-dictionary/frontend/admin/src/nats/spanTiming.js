// One place to answer "how long did this span take" and "when did it start".
//
// It exists because three consumers answered the second question
// independently — TraceWaterfall.vue, PulsePanel.vue, and otlp-bridge's
// ToSpan on the Go side — and all three were wrong in the same way at the
// same time (BR-056). A span carries no start time on the wire: `timestamp`
// is its FINISH moment, and a start is only ever recoverable as finish minus
// duration. That subtraction is subtle enough to deserve one implementation
// rather than three.
//
// Why there is no start timestamp to read instead: a duration is measured
// inside one process against a monotonic clock, while two timestamps stamped
// on two different hosts are not comparable. See natstrace.go's DurationUs
// comment for the full argument.

// spanDurationMs reads a span's own measured duration, in milliseconds —
// fractional, because the wire field is microseconds (BR-056).
//
// Milliseconds is the DISPLAY unit throughout this UI and stays so; what
// changed is that the conversion now happens here, at the point of reading,
// instead of having happened on the publisher's side where it truncated.
//
// LEGACY (delete after one BucketMaxAge, 15 min, past the deploy that ships
// BR-056): a record written before it carries `durationMs` and no
// `durationUs`. Reading it keeps the deploy invisible rather than a panel of
// zero-width bars. Nothing else depends on this branch — same migration
// split as BR-053's 48g, where the frontend tolerates the old shape for one
// bucket lifetime and the Go consumers, which read a live stream rather than
// a backlog, carry no fallback at all.
export function spanDurationMs(span) {
  // Not `span.durationUs || …` — 0µs is a measured duration (a fast span),
  // and falling through on it would silently prefer a stale legacy field.
  if (typeof span?.durationUs === 'number') return span.durationUs / 1000
  return span?.durationMs || 0
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
