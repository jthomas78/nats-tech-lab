/*
  The health plane, browser side (BR-AS60/AS64/AS65).

  Health is decoration. It is read on its own subject, it never blocks the
  boot, and losing it changes nothing about the catalogue, the loaded plugins
  or what any of them may do — the worst it can do to a screen is say
  "unknown".

  The one thing this module is strict about is age. A reading is a fact about
  a moment that has passed, so it ages into `stale` when it is READ rather
  than on a timer: nothing has to wake up for a stale value to stop claiming
  to be current, and there is no interval here to leak.
*/

export const HEALTH_READ_SUBJECT = 'api._platform.registry.frontend-plugins.health.v1'
export const HEALTH_NOTIFY_SUBJECT = 'notify._platform.registry.frontend-plugins.health'

/* Mirrors the registry's own freshness window. Stated here rather than read
   from the reply because it is what THIS shell is willing to believe, and a
   registry that shortened its own window must not be able to make a browser
   trust a reading for longer. */
export const HEALTH_FRESHNESS_MS = 15_000

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
      if (!reply || reply.ok !== true || !reply.plugins || typeof reply.plugins !== 'object' || Array.isArray(reply.plugins)) {
        return { ok: false, code: 'health-malformed' }
      }
      const plugins = {}
      for (const [id, entry] of Object.entries(reply.plugins)) {
        plugins[id] = { frontend: readSignal(entry?.frontend), backend: readSignal(entry?.backend) }
      }
      const asOf = Number.isSafeInteger(reply.asOf) ? reply.asOf : null
      return { ok: true, asOf, plugins }
    },
  }
}

/**
 * The plane: one read on start, one on every hint, one after a reconnect gap,
 * and an answer for any plugin at any time.
 */
export function createHealthPlane({ transport, subscribe, now = () => Date.now() }) {
  let subscription = null
  let listening = false
  let inFlight = null
  /* A hint that arrives mid-read is news the in-flight read may predate, so
     it is remembered and answered with one more read — never dropped, and
     never turned into a queue of reads a burst could grow without bound. */
  let pending = false
  /* id -> {frontend, backend, receivedAt}. Kept through a failed read: losing
     the plane says nothing about any plugin, so the last real observation
     stays and ages rather than being blanked or believed forever. */
  const readings = new Map()
  let latestAsOf = null

  const refresh = async () => {
    if (inFlight) {
      pending = true
      return inFlight
    }
    inFlight = Promise.resolve()
      .then(() => transport.fetchHealth())
      .catch(() => ({ ok: false, code: 'health-unreachable' }))
      .then((result) => {
        inFlight = null
        if (pending) {
          pending = false
          void refresh()
        }
        if (!result?.ok) return result
        /* Two reads can land backwards. An older answer must not overwrite a
           newer one, or a recovered plugin flickers back to broken. */
        if (latestAsOf !== null && result.asOf !== null && result.asOf < latestAsOf) return result
        if (result.asOf !== null) latestAsOf = result.asOf
        const receivedAt = now()
        for (const [id, signals] of Object.entries(result.plugins)) {
          readings.set(id, { ...signals, receivedAt })
        }
        return result
      })
    return inFlight
  }

  const age = (signal, receivedAt) => {
    if (CONFIGURED.has(signal.state)) return signal
    if (!OBSERVED.has(signal.state)) return signal
    return now() - receivedAt >= HEALTH_FRESHNESS_MS ? { ...signal, state: HEALTH_STATE.STALE } : signal
  }

  return {
    start() {
      if (listening) return this
      listening = true
      subscription = subscribe(HEALTH_NOTIFY_SUBJECT, () => {
        /* The hint carries nothing to install. Whatever it claims, the read
           is what is authoritative — the same promise the catalogue's
           notification makes (BR-AS64). */
        if (listening) void refresh()
      })
      void refresh()
      return this
    },
    stop() {
      listening = false
      subscription?.unsubscribe()
      subscription = null
      return this
    },
    /* A gap in the subscription is a gap in the hints, and a hint that never
       arrived is indistinguishable from nothing happening. */
    onReconnect() {
      if (!listening) return Promise.resolve(null)
      return refresh()
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
