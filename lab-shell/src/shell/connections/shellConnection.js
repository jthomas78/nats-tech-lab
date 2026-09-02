import { reactive } from 'vue'

// One shell, one connection. The injected dialer mints a fresh credential on
// every outer reconnect; the NATS client restores subscriptions on inner ones.
export function createShellConnection({
  connect,
  onReconnect,
  // Wrappers retain the browser's receiver. Storing native methods directly
  // and invoking timers.setTimeout() supplies `timers` as this, which throws.
  timers = { setTimeout: (fn, ms) => setTimeout(fn, ms), clearTimeout: (id) => clearTimeout(id) },
  retryDelayMs = 1000,
  /* How long any request on this connection may take. One number, because
     every caller here is a browser waiting on a screen: the catalogue read
     and the health read share the wait and there is no reason for them to
     disagree about it. Injected so a spec can prove the timeout without
     spending it. */
  requestTimeoutMs = 5000,
}) {
  const state = reactive({ connected: false, epoch: 0, error: null })
  const subscriptions = new Set()
  const encoder = new TextEncoder()
  const decoder = new TextDecoder()
  let conn = null
  let pending = null
  let stopped = false
  let retry = null

  const established = () => {
    if (stopped || !conn) return
    state.connected = true
    state.error = null
    state.epoch++
  }
  onReconnect?.(established)

  const scheduleRetry = () => {
    if (stopped || retry !== null) return
    retry = timers.setTimeout(() => {
      retry = null
      void api.start()
    }, retryDelayMs)
  }

  const attach = (record) => {
    record.sub = conn.subscribe(record.subject, {
      callback: (err, msg) => {
        if (!record.active || err) return
        let body = null
        try { body = JSON.parse(decoder.decode(msg.data)) } catch { /* unreadable hint still triggers a read */ }
        record.handler(body)
      },
    })
  }

  const observe = async (socket) => {
    try {
      if (!socket.status) return
      for await (const status of socket.status()) {
        if (conn !== socket || stopped) return
        if (status.type === 'disconnect') state.connected = false
        if (status.type === 'reconnect') established()
      }
    } catch { /* closed() owns terminal failure */ }
  }

  const api = {
    state,
    start() {
      if (stopped || conn) return Promise.resolve(api)
      if (pending) return pending
      pending = (async () => {
        try {
          const socket = await connect()
          if (stopped) { await socket.close(); return api }
          conn = socket
          for (const record of subscriptions) attach(record)
          established()
          void observe(socket)
          socket.closed?.().then(() => {
            if (conn !== socket || stopped) return
            conn = null
            state.connected = false
            state.error = { code: 'connection-closed' }
            scheduleRetry()
          }).catch(() => { /* nats closed() resolves with an error, never rejects */ })
        } catch {
          const socket = conn
          conn = null
          try { await socket?.close() } catch { /* already closed */ }
          state.connected = false
          state.error = { code: 'connect-refused' }
          scheduleRetry()
        }
        return api
      })().finally(() => { pending = null })
      return pending
    },
    async close() {
      stopped = true
      if (retry !== null) timers.clearTimeout(retry)
      retry = null
      for (const record of subscriptions) record.sub?.unsubscribe()
      subscriptions.clear()
      const socket = conn
      conn = null
      state.connected = false
      try { await socket?.close() } catch { /* safe to close an already closed socket */ }
    },
    async request(subject, payload) {
      if (!conn || !state.connected) throw new Error('connection-unavailable')
      const msg = await conn.request(subject, encoder.encode(JSON.stringify(payload)), { timeout: requestTimeoutMs })
      return JSON.parse(decoder.decode(msg.data))
    },
    subscribe(subject, handler) {
      const record = { subject, handler, active: true, sub: null }
      subscriptions.add(record)
      if (conn) attach(record)
      return { unsubscribe() {
        record.active = false
        record.sub?.unsubscribe()
        subscriptions.delete(record)
      } }
    },
    async flush() { await conn?.flush?.() },
  }
  return api
}
