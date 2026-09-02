/*
  When to read, for anything the shell reads on a hint.

  The shell has two planes that are told "something changed" on a subject and
  must then go and read the truth for themselves: the catalogue
  (changeSubscription.js) and health (healthPlane.js). Both need the same four
  behaviours, and neither of them is about catalogues or health:

    - at most one read in flight, chained rather than concurrent, because two
      reads can install in the order they FINISH and an older answer must not
      overwrite a newer one;
    - a hint that lands during a read is held and answered with exactly one
      further read — never dropped, never grown into a queue a burst could
      run away with;
    - a re-established link forces an unconditional read, because a hint that
      never arrived is indistinguishable from nothing having happened;
    - a hint before the subscription exists, or after it is stopped, is not a
      read.

  Both planes had their own copy of this, and the copies had already drifted:
  the catalogue's was consolidated into one state machine and the health
  plane's kept the earlier shape (an in-flight promise plus a boolean, with
  the follow-up read fired from inside the previous read's `.then`). One
  adapter is a hypothetical seam; two is a real one, and this is the second.

  What is NOT here is what a hint MEANS. The catalogue's hints carry a
  revision, so it can drop a hint it has already overtaken and skip a trailing
  read the finished document already covers. A health reading has no revision
  and no such shortcut. That difference is the `hints` policy below — supplied
  by the caller, defaulted to "a hint is just a hint".
*/

/* A hint with no detail: every notification is news, nothing is ever already
   covered, and two held hints are still one hint. This is the whole policy
   for a plane whose readings carry no version. */
export const BARE_HINTS = Object.freeze({
  of: () => null,
  stale: () => false,
  covered: () => false,
  merge: () => null,
})

/**
 * @param {object} options
 * @param {string} options.subject the notify subject to listen on
 * @param {(subject: string, handler: (message: any) => void) => {unsubscribe(): void}} options.subscribe
 * @param {(request: {unconditional: boolean, reason: string}) => Promise<any>} options.read
 *   what a read IS — the one thing this module cannot know.
 * @param {object} [options.hints] how much a hint says about itself:
 *   `of(message)` extracts its token, `stale(token)` says the shell has
 *   already overtaken it, `covered(token, result)` says a finished read
 *   already answered it, `merge(a, b)` combines two held hints.
 */
export function createHintedReader({ subject, subscribe, read, hints = BARE_HINTS }) {
  const policy = { ...BARE_HINTS, ...hints }

  let subscription = null
  /* Live until stopped, which is NOT the same as subscribed: a boot read
     happens before the subscription exists, and a reconnect can land in that
     window and must still be honoured. */
  let active = true
  let subscribed = false
  /* Reads queued or running. Coalescing keys off this rather than off a single
     promise, so a hint arriving behind two chained reads is still held. */
  let outstanding = 0
  let chain = Promise.resolve()
  let pendingHint = false
  let pendingToken = null
  let reconnects = 0

  const run = (unconditional, reason) => {
    outstanding++
    /* Chained, never concurrent. A read that rejects is swallowed here so one
       failure cannot break the chain for every read after it. */
    const done = chain.then(() => read({ unconditional, reason })).catch(() => null)
    chain = done
    void done.then((result) => {
      outstanding--
      // Only the last read of a burst decides what still needs doing.
      if (!active || outstanding > 0) return
      if (reconnects > 0) {
        reconnects--
        run(true, 'reconnect')
        return
      }
      if (!pendingHint) return
      const token = pendingToken
      pendingHint = false
      pendingToken = null
      /* Do not lose a hint received after the server took its read snapshot.
         A plane whose hints say nothing about themselves always reads again;
         one whose hints carry a version skips the read its own document
         already covers. */
      if (!policy.covered(token, result)) run(false, 'notify')
    })
    return done
  }

  const onMessage = (message) => {
    if (!subscribed) return
    const token = policy.of(message)
    if (policy.stale(token)) return
    if (outstanding > 0) {
      pendingToken = pendingHint ? policy.merge(pendingToken, token) : token
      pendingHint = true
    } else run(false, 'notify')
  }

  return {
    /* A read the caller asked for — a boot read, or one that closes the gap
       between a snapshot and the subscription. It goes through the same queue
       as a hint's read for the single reason above: nothing may install out of
       order, whoever asked for it. */
    refresh({ unconditional = false, reason } = {}) {
      if (!active) return Promise.resolve(null)
      return run(unconditional, reason)
    },
    start() {
      if (active && !subscribed) {
        subscribed = true
        subscription = subscribe(subject, onMessage)
      }
      return this
    },
    stop() {
      active = false
      subscribed = false
      subscription?.unsubscribe()
      subscription = null
      pendingHint = false
      pendingToken = null
      reconnects = 0
      return this
    },
    /* Honoured before `start` as well as after it. A link re-established while
       the boot read is still running is exactly the case that used to need a
       second counter in the caller. */
    onReconnect() {
      if (!active) return Promise.resolve(null)
      if (outstanding > 0) {
        reconnects++
        return chain
      }
      return run(true, 'reconnect')
    },
  }
}
