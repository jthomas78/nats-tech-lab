/* The readiness probe: three states, and the line between two of them
   (BR-AS79, task 16e).

   The whole rule fits in one sentence: readiness is ASSERTED, not inferred
   from reachability. A port that answers proves a process is listening. It
   does not prove the demo works — demo 04's shim starts happily before its
   NATS server has a stream or its buckets, and a probe that treated a 200 as
   "up" would send a reader into a page that fails on its first command.

   So the demo's own endpoint does the asserting (it asks NATS for the log and
   both buckets on every call) and this file only reads the verdict.

   Three states, and the third is the one people get wrong:

   - `available`   — the demo said yes.
   - `unavailable` — the demo said no, and said which piece is missing. The
                     demo is running; something it needs is not there.
   - `unknown`     — we could not ask. A timeout, a refused connection, a 404
                     on the route, a body that will not parse.

   `unknown` must NEVER be worded as "the demo is stopped". We did not learn
   that. A laptop asleep, a proxy not configured, a slow container and a demo
   that is genuinely off all land here, and only one of them is fixed by
   starting the demo. The wording for it lives in demoReadinessText.js and says
   "cannot reach demo services", which is exactly what happened.

   The request is same-origin: the URL comes from the catalogue as a path on
   this page's origin, and the shell's own proxy forwards it. No demo backend
   was given a CORS grant (F-3), so a probe sent anywhere else simply fails —
   which is the property working, not a bug.
*/

export const DEMO_STATE = Object.freeze({
  AVAILABLE: 'available',
  UNAVAILABLE: 'unavailable',
  UNKNOWN: 'unknown',
})

/* Why we could not ask, when the state is `unknown`. A small closed set the
   text layer maps to sentences; no message from a demo's backend is ever put
   on screen by the shell. */
export const PROBE_CAUSE = Object.freeze({
  TIMEOUT: 'timeout',
  UNREACHABLE: 'unreachable',
  NO_ROUTE: 'no-route',
  UNREADABLE: 'unreadable',
})

/* One failing piece, reduced to a word. The demo sends `missing` or
   `unreachable` per check; anything else becomes `unreachable`, so a demo
   cannot introduce a new code that the shell would print unrecognised. */
function checksOf(body) {
  if (!Array.isArray(body?.checks)) return []
  return body.checks
    .filter((check) => check && typeof check.name === 'string' && check.ok !== true)
    .map((check) => Object.freeze({
      name: check.name,
      code: check.code === 'missing' ? 'missing' : 'unreachable',
    }))
}

/**
 * Ask one demo whether it is ready.
 *
 * @param {object} options
 * @param {{url: string, timeoutMs: number}} options.readiness From the catalogue.
 * @param {Function} options.fetch
 * @param {Function} [options.now] Injected clock, for `checkedAt`.
 * @returns {Promise<{state: string, cause: string|null, failing: object[], checkedAt: string}>}
 */
export async function probeDemo({ readiness, fetch, now = () => new Date().toISOString() }) {
  const at = () => now()
  const unknown = (cause) => ({ state: DEMO_STATE.UNKNOWN, cause, failing: [], checkedAt: at() })

  if (!readiness?.url) return unknown(PROBE_CAUSE.NO_ROUTE)

  /* The timeout is the SHELL's, not the demo's. A demo that hangs must not
     hang the panel, and an AbortController is the only way to stop a fetch
     that has already left. */
  const controller = typeof AbortController === 'function' ? new AbortController() : null
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    controller?.abort()
  }, readiness.timeoutMs ?? 3000)

  let response
  try {
    response = await fetch(readiness.url, { cache: 'no-store', signal: controller?.signal })
  } catch {
    return unknown(timedOut ? PROBE_CAUSE.TIMEOUT : PROBE_CAUSE.UNREACHABLE)
  } finally {
    clearTimeout(timer)
  }

  /* 404 is the route, not the demo. Both environments close the readiness
     prefix with a 404 precisely so this case is distinguishable, instead of
     the SPA answering 200 with a page of HTML that would read as "ready". */
  if (response?.status === 404) return unknown(PROBE_CAUSE.NO_ROUTE)

  /* 503 is the demo answering, so it is `unavailable`, not `unknown`, and its
     body is read below. Any other refusal is something BETWEEN us and the
     demo — most often the proxy reporting that the demo's port refused the
     connection — so it is `unreachable`.

     This is checked BEFORE the body is parsed, and the order matters. A proxy
     reporting a refused upstream answers 500 with a line of plain text, and
     parsing first read that as "the answer did not make sense" — blaming the
     demo for a sentence the demo never sent. */
  if (!response.ok && response.status !== 503) return unknown(PROBE_CAUSE.UNREACHABLE)

  let body
  try {
    body = await response.json()
  } catch {
    return unknown(PROBE_CAUSE.UNREADABLE)
  }

  if (body?.ready === true) {
    return { state: DEMO_STATE.AVAILABLE, cause: null, failing: [], checkedAt: at() }
  }
  if (body?.ready === false) {
    return { state: DEMO_STATE.UNAVAILABLE, cause: null, failing: checksOf(body), checkedAt: at() }
  }
  /* A 200 with no verdict is not a yes. Silence is never consent here. */
  return unknown(PROBE_CAUSE.UNREADABLE)
}
