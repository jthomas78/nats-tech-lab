// Reading a subject, and reading a KV key. Nothing else.
//
// The subject carries the event TYPE — the body does not (see codec.go). So a
// screen that wants to say "travelled" reads the subject, exactly the way a
// consumer that wants to filter on `...travelled` does. This file is the one
// place in the UI that knows that.

import { SUBJECT_PREFIX } from '../config.js'

const PREFIX = SUBJECT_PREFIX.split('.')

// The three types domain.go can produce. An unknown type is refused rather
// than passed through: codec.go treats one as an error for the same reason,
// and a screen that silently drops an event it does not understand is lying
// about what the log holds.
export const EVENT_TYPES = Object.freeze(['registered', 'travelled', 'retired'])

// parseEventSubject splits evt.odometer.vehicle.{id}.{type}.
// Returns null for anything that is not one of this demo's events.
export function parseEventSubject(subject) {
  const tokens = String(subject ?? '').split('.')
  if (tokens.length !== PREFIX.length + 2) return null
  for (let i = 0; i < PREFIX.length; i++) {
    if (tokens[i] !== PREFIX[i]) return null
  }
  const vehicle = tokens[PREFIX.length]
  const type = tokens[PREFIX.length + 1]
  if (!vehicle || !EVENT_TYPES.includes(type)) return null
  return { vehicle, type }
}

// vehicleFromKey reverses snapshotKey() in names.go: `vehicle.{id}`.
// Both buckets use the same key shape, so both watches use this.
export function vehicleFromKey(key) {
  const s = String(key ?? '')
  if (!s.startsWith('vehicle.')) return null
  const id = s.slice('vehicle.'.length)
  return id === '' ? null : id
}

// workerFromKey reverses workerKey() in names.go: `worker.{02d}`.
// KV odometer-pool-workers holds one of these per worker, and nothing else.
export function workerFromKey(key) {
  const s = String(key ?? '')
  if (!s.startsWith('worker.')) return null
  const n = Number(s.slice('worker.'.length))
  return Number.isInteger(n) && n > 0 ? n : null
}
