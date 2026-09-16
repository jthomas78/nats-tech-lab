// Formatting only. No decisions, no rules — see model.js's header and plan
// section 9.2 (D1). A number gets thousands separators here; whether the
// number was allowed to exist was settled in domain.go.

// A non-breaking thin space groups the thousands, so `48 210.4` never wraps
// mid-number in a narrow column.
const THIN = ' '

export function formatKm(km) {
  const n = Number(km)
  if (!Number.isFinite(n)) return '0.0'
  const [whole, frac] = n.toFixed(1).split('.')
  return `${whole.replace(/\B(?=(\d{3})+(?!\d))/g, THIN)}.${frac}`
}

export function formatCount(n) {
  const v = Number(n)
  if (!Number.isFinite(v)) return '0'
  return String(Math.trunc(v)).replace(/\B(?=(\d{3})+(?!\d))/g, THIN)
}

// Clock time only. The demo is watched live, so the date is noise; an empty or
// unparseable timestamp is drawn as an em dash rather than as "Invalid Date".
//
// Go's zero time is year 1, and it arrives in the read model as a real,
// parseable timestamp for a vehicle that has never travelled. Drawn as a clock
// it reads as a trip that happened, so anything before 1970 is "never" — the
// same word the CLI prints.
export function formatClock(value) {
  if (!value) return '—'
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return '—'
  if (d.getUTCFullYear() < 1970) return '(never)'
  return d.toTimeString().slice(0, 8)
}

// Which side has folded an event in yet. This is the lag, told row by row:
// the same sequence number that sits at the head of the log is "neither yet"
// until both projectors reach it.
export function foldedInto(seq, { writeSeq = 0, readSeq = 0 } = {}) {
  const s = Number(seq) || 0
  const sides = []
  if (s <= Number(writeSeq)) sides.push('write')
  if (s <= Number(readSeq)) sides.push('read')
  return sides
}

// Bytes, printed so a reader can price them at a glance.
//
// Plan 04.7.16 makes this a standing rule: anywhere this demo shows a stream's
// message count, it shows the bytes that count consumes as well. A length is a
// number nobody can price — 100 000 000 events sounds reasonable right up to
// the moment you learn it is 7.6 GB.
//
// Binary units, because that is what `nats stream info` prints and a reader is
// meant to be able to check this screen against a terminal.
export function formatBytes(bytes) {
  const n = Number(bytes)
  if (!Number.isFinite(n) || n < 0) return '0 B'
  if (n < 1024) return `${Math.trunc(n)} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let value = n / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(1)} ${units[unit]}`
}
