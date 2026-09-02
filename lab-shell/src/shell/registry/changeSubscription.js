export const NOTIFY_SUBJECT = 'notify._platform.registry.frontend-plugins.changed'

const revisionNumber = (value) => {
  if (typeof value !== 'number' && !(typeof value === 'string' && /^\d+$/.test(value))) return null
  const n = Number(value)
  return Number.isSafeInteger(n) && n >= 0 ? n : null
}

/*
  Every read the shell makes while it is live, and the whole of when to make
  one (BR-AS28, AS29; decisions 55-58).

  One machine, on purpose. Reads used to be serialised by the session and
  coalesced here, which meant two pieces of state answering the same question
  and each blind to the other's: the session queued reads the subscription did
  not know were outstanding, and kept a second reconnect counter because
  `onReconnect` refused to hold one before `start`. This owns both jobs — at
  most one read at a time, and what a hint or a reconnect arriving during one
  is worth — so the session is left with nothing but start and stop.

  The interface is four calls and no state to read back: `refresh` for a read
  the caller wants, `start`/`stop` for the subscription, `onReconnect` for a
  re-established link. All four are safe in any order and none of them throw.
*/
export function createChangeSubscription({ subscribe, read, currentRevision }) {
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
  let pendingRevision = null
  let reconnects = 0

  const run = (unconditional, reason) => {
    outstanding++
    /* Chained, never concurrent. Two reads in flight can install in the order
       they finish rather than the order they were asked for, which lets an
       older document overwrite a newer one. A read that rejects is swallowed
       here so one failure cannot break the chain for every read after it. */
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
      const target = pendingRevision
      pendingHint = false
      pendingRevision = null
      const received = revisionNumber(result?.revision) ?? revisionNumber(currentRevision())
      // Do not lose a hint received after the server took its read snapshot.
      // Unknown hints require a trailing read; known bursts already covered
      // by the completed document coalesce completely.
      if (target === null || received === null || target > received) run(false, 'notify')
    })
    return done
  }

  const onMessage = (message) => {
    if (!subscribed) return
    const revision = revisionNumber(message?.revision)
    const held = revisionNumber(currentRevision())
    if (revision !== null && held !== null && revision <= held) return
    if (outstanding > 0) {
      if (!pendingHint) pendingRevision = revision
      else if (pendingRevision !== null) pendingRevision = revision === null ? null : Math.max(pendingRevision, revision)
      pendingHint = true
    } else run(false, 'notify')
  }

  return {
    /* A read the caller asked for — the boot read, and the one that closes the
       snapshot/subscription gap. It goes through the same queue as a hint's
       read for the single reason above: nothing here may install out of order,
       whoever asked for it. */
    refresh({ unconditional = false, reason } = {}) {
      if (!active) return Promise.resolve(null)
      return run(unconditional, reason)
    },
    start() {
      if (active && !subscribed) {
        subscribed = true
        subscription = subscribe(NOTIFY_SUBJECT, onMessage)
      }
      return this
    },
    stop() {
      active = false
      subscribed = false
      subscription?.unsubscribe()
      subscription = null
      pendingHint = false
      reconnects = 0
      return this
    },
    /* Honoured before `start` as well as after it. A link re-established while
       the boot read is still running is exactly the case that used to need a
       counter in the session. */
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
