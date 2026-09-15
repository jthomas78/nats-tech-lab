// Shaping what NATS sends into what the screen draws.
//
// Pure functions, no NATS types, no Vue. That is what makes them testable
// without a server, and it keeps useOdometer.js down to plumbing.
//
// NOTHING HERE DECIDES ANYTHING. There is no `if km <= 0` in this file and
// there never will be — every accept and every refusal comes from domain.go
// (plan section 9.2, D1). Formatting a number is not a rule.

import { parseEventSubject, vehicleFromKey } from './subjects.js'

// writeSnapshot shapes one value from KV odometer-write: {state, lastSeq}.
//
// lastSeq is the whole point of this bucket on this screen: it says how far
// behind the stream head the write side is, and that gap is real, not a bug
// (plan section 4.1).
export function writeSnapshot(key, doc) {
  const id = vehicleFromKey(key)
  if (id === null) return null
  const state = doc?.state ?? {}
  return {
    id,
    status: state.status ?? '',
    plate: state.plate ?? '',
    lastSeq: Number(doc?.lastSeq ?? 0),
  }
}

// readModel shapes one value from KV odometer-read — flat, denormalised, and
// built for one KV get with no replay.
export function readModel(key, doc) {
  const id = vehicleFromKey(key)
  if (id === null) return null
  return {
    id,
    status: doc?.status ?? '',
    plate: doc?.plate ?? '',
    totalKm: Number(doc?.totalKm ?? 0),
    trips: Number(doc?.trips ?? 0),
    lastTripAt: doc?.lastTripAt ?? '',
    lastSeq: Number(doc?.lastSeq ?? 0),
  }
}

// logEvent shapes one message off the stream into a row.
// Returns null for a subject this demo did not publish.
export function logEvent({ subject, seq, time, body }) {
  const parsed = parseEventSubject(subject)
  if (parsed === null) return null
  return {
    seq: Number(seq ?? 0),
    at: time instanceof Date ? time.toISOString() : String(time ?? ''),
    vehicle: parsed.vehicle,
    type: parsed.type,
    detail: detailOf(parsed.type, body ?? {}),
  }
}

// detailOf is the one-line summary shown beside the event type. Presentation
// only — the body is kept on the row for anyone who wants the raw JSON.
function detailOf(type, body) {
  switch (type) {
    case 'registered':
      return body.plate ? `plate ${body.plate}` : 'plate (none)'
    case 'travelled':
      return `${Number(body.km ?? 0)} km`
    case 'retired':
      return body.reason ? String(body.reason) : '(no reason given)'
    default:
      return ''
  }
}

// lag is the number the whole screen exists for: how far each side trails the
// head of the log. Two sides, two distances, one axis.
//
// A negative distance is clamped to 0. It means a projector folded an event
// that this browser's stream tail has not caught up with yet — the browser is
// behind, not the projector ahead, and drawing a bar backwards would say the
// opposite of what happened.
export function lag({ head = 0, writeSeq = 0, readSeq = 0 } = {}) {
  const h = Number(head) || 0
  const w = Number(writeSeq) || 0
  const r = Number(readSeq) || 0
  return {
    head: h,
    writeSeq: w,
    readSeq: r,
    writeLag: Math.max(0, h - w),
    readLag: Math.max(0, h - r),
  }
}

// maxSeq is how the screen finds one position for a whole bucket: the furthest
// any vehicle in it has been folded to.
export function maxSeq(entries) {
  let max = 0
  for (const e of entries) {
    if (e && Number(e.lastSeq) > max) max = Number(e.lastSeq)
  }
  return max
}
