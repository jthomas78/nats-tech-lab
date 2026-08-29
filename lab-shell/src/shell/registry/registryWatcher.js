/*
  The re-read trigger (BR-AS19, decision 44).

  A registry change has to become visible to a shell that is already running,
  and decision 36 keeps the browser off NATS in this phase — so the signal
  arrives over the read path the shell already has: a conditional GET on focus
  and on a slow interval. `If-None-Match` makes the common case a 304 that
  costs a round trip and nothing else.

  Everything ambient is injected — the document, the timer functions, the
  client. That is not test decoration: a watcher that reached for
  `globalThis.document` could not be booted twice in one process, and every
  spec in this repo boots a shell per test.

  This module decides WHEN to read and never what a change means; the caller
  is handed the result and decides that (see registryDiff.js).
*/

/** Ten minutes. Slow on purpose: focus is the responsive trigger, this is the
    backstop for a tab left open on one screen all afternoon. */
export const DEFAULT_INTERVAL_MS = 10 * 60 * 1000

/**
 * @param {object} options
 * @param {{fetchRegistry(opts?: object): Promise<object>}} options.client
 * @param {(result: object) => void} options.onResult every completed read,
 *   304s included — the caller distinguishes them by `unchanged`.
 * @param {string|null} [options.etag] what boot already read, so the first
 *   re-read is conditional rather than a full fetch.
 * @param {number} [options.intervalMs]
 * @param {Document} [options.doc]
 * @param {{setInterval: Function, clearInterval: Function}} [options.timers]
 */
export function createRegistryWatcher({
  client,
  onResult,
  etag = null,
  intervalMs = DEFAULT_INTERVAL_MS,
  doc = globalThis.document,
  /* Bound, not merely referenced: in a browser these are methods on `window`
     and calling a bare reference throws "Illegal invocation" — which no spec
     with injected timers would ever see. */
  timers = {
    setInterval: globalThis.setInterval.bind(globalThis),
    clearInterval: globalThis.clearInterval.bind(globalThis),
  },
}) {
  let lastEtag = etag
  let timer = null
  let listening = false
  /* One read at a time. Focus and the interval can land together, and two
     concurrent reads would race to set the ETag — the loser writing back the
     older one, which then asks for a document the shell already has. */
  let inFlight = null

  const visible = () => !doc || doc.visibilityState !== 'hidden'

  const check = async (reason = 'manual') => {
    /* A hidden tab reads nothing. The visibility handler covers the moment it
       comes back, so an interval tick in the background would only spend a
       request on a document nobody can see. `manual` is exempt: it is the
       operator pressing the button. */
    if (reason !== 'manual' && !visible()) return null
    if (inFlight) return inFlight

    inFlight = (async () => {
      const result = await client.fetchRegistry({ etag: lastEtag })
      /* Only a successful read moves the ETag. A failed one leaves it alone,
         so the next attempt still asks the conditional question rather than
         silently falling back to a full read forever. */
      if (result?.ok && result.etag) lastEtag = result.etag
      onResult?.({ ...result, reason })
      return result
    })()

    try {
      return await inFlight
    } finally {
      inFlight = null
    }
  }

  const onVisibility = () => {
    if (visible()) void check('visible')
  }

  return {
    get etag() {
      return lastEtag
    },

    start() {
      if (listening) return this
      listening = true
      doc?.addEventListener?.('visibilitychange', onVisibility)
      timer = timers.setInterval(() => void check('interval'), intervalMs)
      return this
    },

    /* Idempotent, and safe to call without start(): a shell that never
       started a watcher still tears one down on unmount. */
    stop() {
      listening = false
      doc?.removeEventListener?.('visibilitychange', onVisibility)
      if (timer !== null) timers.clearInterval(timer)
      timer = null
      return this
    },

    check,
  }
}
