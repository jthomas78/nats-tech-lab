// Number formatting for the summary-card rows shared by ConnectionsPanel and
// ServicesPanel.
//
// Every card value in those rows uses one type size (20px — see
// `.summary-value`), which means a counter that grows without bound can't be
// allowed to set the card's width: at 20px, "1,234,567 / 8,901,234" is ~197px
// and would overflow or wrap a card that its siblings fit comfortably. The old
// answer was a smaller font on those cards alone, which made three different
// value sizes appear in one row and still overflowed once the numbers got big
// enough.
//
// So: exact figures while they're short, magnitude once they aren't, and the
// exact value always available via the card's title. Message and request
// counters are the only values here that run away — connection and instance
// counts don't — but both panels format every value through this so a row can't
// drift back into mixed treatments.

// Below this, the grouped figure is short enough to fit and more useful than a
// rounded one: "99,999" reads as a real count, "100.0K" doesn't.
const COMPACT_FROM = 100_000

/** Grouped exact figure — "8,901,234". Use for tooltips and detail panes. */
export function exactCount(value) {
  return Number(value || 0).toLocaleString()
}

/**
 * Card-safe figure: exact under 100,000, magnitude above it — "774",
 * "99,999", "123K", "8.9M". Keeps a summary card one line wide at any age of
 * the stack.
 */
export function compactCount(value) {
  const n = Number(value || 0)
  if (Math.abs(n) < COMPACT_FROM) return n.toLocaleString()
  return n.toLocaleString(undefined, {
    notation: 'compact',
    maximumFractionDigits: 1,
  })
}
