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

// ── NKey elision (BR-061) ────────────────────────────────────────────────────
//
// A NATS NKey is never rendered in full in the Admin UI. It renders as
// `[FIRST5...LAST5]` — brackets and glyph included — beside whatever friendly
// value identifies it.
//
// This is one helper rather than a per-panel `slice()` because it replaced four
// different idioms for the same fact: `slice(0, 10)…` in ConnectionsPanel,
// `slice(0, 12)…` in AccountsPanel's pubkey cell, a copy of the first in
// AccountsOverviewPanel, and the full 56 characters in two detail panes. A rule
// that binds every panel needs one place it can actually be enforced from.
//
// Ten characters is well past the point of collision for a stack with tens of
// keys, and that is the whole job: an NKey here is for RECOGNISING a row, not
// for verifying a key. Anything that needs the real value copies it (see
// `NKEY_ELISION` consumers' copy affordance) rather than reading it off screen.

/** The literal glyph, three periods — not `…`. Exported so specs can't drift. */
export const NKEY_GLYPH = '...'

// Below this there is nothing to elide: the two halves plus the glyph would be
// longer than the key itself, so the "shortened" form would be the lie.
const NKEY_MIN_LENGTH = 15

/**
 * `[ADD65 ... 2RTQM]` for a real NKey; the value unchanged for anything too
 * short to elide, and an empty string for nothing at all.
 *
 * Callers pass raw keys. This does not validate that the input IS an NKey —
 * `SharingPanel`'s edge labels, for instance, are a name OR a key and are
 * deliberately NOT routed through here (BR-061), because bracketing a friendly
 * name would say "this is a key" about something that isn't one.
 */
export function elideNKey(key) {
  const s = String(key ?? '').trim()
  if (!s) return ''
  if (s.length < NKEY_MIN_LENGTH) return s
  return `[${s.slice(0, 5)}${NKEY_GLYPH}${s.slice(-5)}]`
}
