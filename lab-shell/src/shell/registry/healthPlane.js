/*
  The health plane, browser side (BR-AS60/AS64/AS65).

  Health is decoration. It is read on its own subject, it never blocks the
  boot, and losing it changes nothing about the catalogue, the loaded plugins
  or what any of them may do — the worst it can do to a screen is say
  "unknown".

  The central registry pushes one timestamped snapshot after every completed
  probe pass. The browser reads once at startup/reconnect and only occasionally
  reconciles, so opening another shell does not add another five-second poll.
  Ageing stays local: a reading becomes `stale` when it is READ.
*/

import { createHintedReader } from './hintedReader.js'

export const HEALTH_READ_SUBJECT = 'api._platform.mfe-registry.frontend-plugins.health.v1'
export const HEALTH_NOTIFY_SUBJECT = 'notify._platform.mfe-registry.frontend-plugins.health'

/* Mirrors the registry's own freshness window. Stated here rather than read
   from the reply because it is what THIS shell is willing to believe, and a
   registry that shortened its own window must not be able to make a browser
   trust a reading for longer. */
export const HEALTH_FRESHNESS_MS = 15_000
export const HEALTH_RECONCILE_BASE_MS = 60_000
export const HEALTH_RECONCILE_JITTER_MS = 15_000

/* Spread reconciliation reads from shells opened together across a 45–75s
   window. Push is the normal path; this is only a bounded repair mechanism. */
export const healthReconcileInterval = (random = Math.random) => HEALTH_RECONCILE_BASE_MS
  + Math.round((random() * 2 - 1) * HEALTH_RECONCILE_JITTER_MS)

export const HEALTH_STATE = Object.freeze({
  UNKNOWN: 'unknown',
  HEALTHY: 'healthy',
  UNAVAILABLE: 'unavailable',
  STALE: 'stale',
  NOT_CONFIGURED: 'not configured',
  NOT_APPLICABLE: 'not applicable',
})

const OBSERVED = new Set([HEALTH_STATE.HEALTHY, HEALTH_STATE.UNAVAILABLE, HEALTH_STATE.STALE])

/* A configuration answer is not an observation, so it does not age: there is
   nothing in "no target is mapped" that could have changed while nobody was
   looking. */
const CONFIGURED = new Set([HEALTH_STATE.NOT_CONFIGURED, HEALTH_STATE.NOT_APPLICABLE])

const KNOWN = new Set(Object.values(HEALTH_STATE))

/* A cause travels from a service that reached a host, so it is accepted only
   in the shape the closed vocabulary takes: one short lowercase word, hyphens
   allowed. A URL, a port, a sentence or a stack is dropped rather than
   rendered (BR-AS60). */
const SAFE_CAUSE = /^[a-z][a-z0-9-]{0,31}$/

const unknown = () => ({ state: HEALTH_STATE.UNKNOWN, cause: '' })

const readSignal = (raw) => {
  if (!raw || typeof raw !== 'object') return unknown()
  const state = KNOWN.has(raw.state) ? raw.state : HEALTH_STATE.UNKNOWN
  const cause = typeof raw.cause === 'string' && SAFE_CAUSE.test(raw.cause) ? raw.cause : ''
  const lastCheckAt = typeof raw.lastCheckAt === 'string' ? raw.lastCheckAt : ''
  return { state, cause, lastCheckAt }
}

/* One decoder for both wire paths. A pushed observation and an initial or
   reconnect response are the same health snapshot; letting those parsers
   drift would make transport choice change what the UI believes. */
export function decodeHealthSnapshot(reply) {
  if (!reply || reply.ok !== true || !reply.plugins || typeof reply.plugins !== 'object' || Array.isArray(reply.plugins)) {
    return { ok: false, code: 'health-malformed' }
  }
  const plugins = {}
  for (const [id, entry] of Object.entries(reply.plugins)) {
    plugins[id] = { frontend: readSignal(entry?.frontend), backend: readSignal(entry?.backend) }
  }
  const asOf = Number.isSafeInteger(reply.asOf) ? reply.asOf : null
  return { ok: true, asOf, plugins }
}

/**
 * The read. Never throws: a health plane that cannot be reached is a code the
 * caller records, exactly as the catalogue's transport does (BR-AS22).
 */
export function createHealthTransport({ request }) {
  return {
    async fetchHealth() {
      let reply
      try {
        /* No arguments. A held revision would imply an observation has one —
           every answer here is simply the current reading. */
        reply = await request(HEALTH_READ_SUBJECT, {})
      } catch (error) {
        const timeout = ['503', 'TIMEOUT'].includes(error?.code)
          || [error?.name, error?.cause?.name].some((name) => ['TimeoutError', 'NoRespondersError'].includes(name))
        return { ok: false, code: timeout ? 'health-timeout' : 'health-unreachable' }
      }
      return decodeHealthSnapshot(reply)
    },
  }
}

/**
 * The plane: one read on start, pushed observations during the connected
 * session, one read after a reconnect gap, and an answer for any plugin at
 * any time. A payload-free legacy hint still triggers a read, which makes the
 * wire change safe across a rolling backend/frontend restart.
 *
 * `onChange` fires once per request or push that installed newer readings.
 * The caller then reads `snapshot()` because ageing is local and a value
 * handed out by the transport would not age.
 */
export function createHealthPlane({ transport, subscribe, onChange = () => {}, now = () => Date.now() }) {
  /* id -> {frontend, backend, receivedAt}. Kept through a failed read: losing
     the plane says nothing about any plugin, so the last real observation
     stays and ages rather than being blanked or believed forever. */
  const readings = new Map()
  let latestAsOf = null
  let started = false

  const install = (result) => {
    if (!result?.ok) return result
    /* Two reads can land backwards. An older answer must not overwrite a
       newer one, or a recovered plugin flickers back to broken. Equality is
       also a duplicate: accepting it would incorrectly renew the local
       freshness lease without a newer central observation. */
    if (latestAsOf !== null && result.asOf !== null && result.asOf <= latestAsOf) return result
    if (result.asOf !== null) latestAsOf = result.asOf
    const receivedAt = now()
    for (const [id, signals] of Object.entries(result.plugins)) {
      readings.set(id, { ...signals, receivedAt })
    }
    /* After the readings are installed, so a caller that reads `snapshot()`
       from here sees this read and not the one before it. Never allowed to
       break the read: a throwing subscriber is the screen's problem, and
       health is decoration either way. */
    try { onChange() } catch { /* a subscriber must not fail a read */ }
    return result
  }

  /* What a reconciliation read IS. `hintedReader` still owns coalescing for
     startup, reconnect and a payload-free notification from an older server. */
  const read = async () => install(await Promise.resolve()
    .then(() => transport.fetchHealth())
    .catch(() => ({ ok: false, code: 'health-unreachable' })))

  const pushedSubscribe = (subject, legacyHint) => subscribe(subject, (message) => {
    const pushed = decodeHealthSnapshot(message)
    if (pushed.ok && pushed.asOf !== null) {
      install(pushed)
      return
    }
    /* An empty/malformed notification may be from the payload-free version
       during a rolling restart. Treat it as its old meaning: look again. */
    legacyHint(message)
  })

  const reader = createHintedReader({ subject: HEALTH_NOTIFY_SUBJECT, subscribe: pushedSubscribe, read })

  const refresh = () => reader.refresh({ reason: 'refresh' })

  const age = (signal, receivedAt) => {
    if (CONFIGURED.has(signal.state)) return signal
    if (!OBSERVED.has(signal.state)) return signal
    return now() - receivedAt >= HEALTH_FRESHNESS_MS ? { ...signal, state: HEALTH_STATE.STALE } : signal
  }

  return {
    start() {
      if (started) return this
      started = true
      reader.start()
      void reader.refresh({ reason: 'start' })
      return this
    },
    stop() {
      reader.stop()
      return this
    },
    /* A gap in the subscription is a gap in pushed observations, and one
       that never arrived is indistinguishable from nothing happening. */
    onReconnect() {
      return reader.onReconnect()
    },
    refresh,
    signalsFor(id) {
      const reading = readings.get(id)
      if (!reading) return { frontend: unknown(), backend: unknown() }
      return {
        frontend: age(reading.frontend, reading.receivedAt),
        backend: age(reading.backend, reading.receivedAt),
      }
    },
    snapshot() {
      const out = {}
      for (const id of readings.keys()) out[id] = this.signalsFor(id)
      return out
    },
  }
}
