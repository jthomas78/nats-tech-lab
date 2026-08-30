export const NOTIFY_SUBJECT = 'notify._platform.registry.frontend-plugins.changed'

const revisionNumber = (value) => {
  if (typeof value !== 'number' && !(typeof value === 'string' && /^\d+$/.test(value))) return null
  const n = Number(value)
  return Number.isSafeInteger(n) && n >= 0 ? n : null
}

export function createChangeSubscription({ subscribe, read, currentRevision }) {
  let subscription = null
  let listening = false
  let inFlight = null
  let pendingHint = false
  let pendingRevision = null
  let reconnects = 0

  const run = (unconditional, reason) => {
    // Start immediately so a synchronous burst sees one in-flight request.
    inFlight = Promise.resolve().then(() => read({ unconditional, reason })).catch(() => null)
    void inFlight.then((result) => {
      inFlight = null
      if (!listening) return
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
    return inFlight
  }

  const onMessage = (message) => {
    if (!listening) return
    const revision = revisionNumber(message?.revision)
    const held = revisionNumber(currentRevision())
    if (revision !== null && held !== null && revision <= held) return
    if (inFlight) {
      if (!pendingHint) pendingRevision = revision
      else if (pendingRevision !== null) pendingRevision = revision === null ? null : Math.max(pendingRevision, revision)
      pendingHint = true
    } else run(false, 'notify')
  }

  return {
    start() {
      if (!listening) {
        listening = true
        subscription = subscribe(NOTIFY_SUBJECT, onMessage)
      }
      return this
    },
    stop() {
      listening = false
      subscription?.unsubscribe()
      subscription = null
      pendingHint = false
      reconnects = 0
      return this
    },
    onReconnect() {
      if (!listening) return Promise.resolve(null)
      if (inFlight) { reconnects++; return inFlight }
      return run(true, 'reconnect')
    },
  }
}
