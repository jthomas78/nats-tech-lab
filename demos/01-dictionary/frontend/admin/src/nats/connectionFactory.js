// Shared connect/reconnect/subscribe machinery behind useNatsConnection.js
// (tenant) and usePlatformConnection.js (PLATFORM) — Phase 23's dual-
// connection model for frontend/admin, replacing its EventSource/SSE
// streams. Both composables are subscribe-only (the Admin UI inspects, it
// never issues api.* commands the way seafreight-app's useNatsConnection.js
// does), so this factory has no request()/publish surface at all — adding
// one before anything needs it would be exactly the speculative feature
// CLAUDE.md's "don't design for hypothetical future requirements" warns
// against.
//
// Modeled directly on seafreight-app/src/nats/useNatsConnection.js's
// connect/disconnect/auto-reconnect-on-close shape (same short-lived-JWT,
// no-refresh-flow tradeoff — see that file's doc comment) — duplicated here
// rather than imported because Vite/Rollup resolves the bare
// @nats-io/nats-core specifier relative to the importing file, and this app
// has its own separate node_modules from seafreight-app's.

import { ref } from 'vue'
import { jwtAuthenticator, wsconnect } from '@nats-io/nats-core'

const encoder = new TextEncoder()
const decoder = new TextDecoder()

function errorMessage(err) {
  return err instanceof Error ? err.message : String(err)
}

// createConnectionState builds one independent connection's reactive state
// + connect/disconnect/subscribe, closing over its own nc/connectSeq so two
// instances (tenant + platform) never share connection state.
//
// fetchConnectInfo() must return { wsUrl, jwt, nkeySeed } (accounts-service
// auth/handler.go's ConnectInfo shape) and is called fresh on every
// connect()/reconnect — it's a function, not a fixed value, so a tenant
// connection's fetchConnectInfo can read whichever tenant is current at
// reconnect time.
export function createConnectionState({ fetchConnectInfo, connectionName }) {
  const connected = ref(false)
  const lastError = ref('')

  let nc = null
  let connectSeq = 0

  function notConnectedError() {
    return new Error(lastError.value || 'not connected')
  }

  async function disconnect() {
    connectSeq++
    connected.value = false
    if (!nc) return
    const closing = nc
    nc = null
    try {
      await closing.close()
    } catch {
      // already closed/closing — nothing to do
    }
  }

  async function connect() {
    await disconnect()
    const mySeq = ++connectSeq

    const info = await fetchConnectInfo()
    const authenticator = jwtAuthenticator(info.jwt, encoder.encode(info.nkeySeed))

    const conn = await wsconnect({ servers: info.wsUrl, authenticator, name: connectionName })
    if (mySeq !== connectSeq) {
      conn.close()
      return
    }

    nc = conn
    connected.value = true
    lastError.value = ''

    conn.closed().then((err) => {
      if (nc !== conn) return
      connected.value = false
      if (err) lastError.value = errorMessage(err)
      connect().catch((reconnectErr) => {
        lastError.value = errorMessage(reconnectErr)
      })
    })
  }

  // subscribe listens on subject, JSON-decoding each message and invoking
  // callback(payload, subject) — subject is the exact subject the message
  // arrived on, useful when subject carries a wildcard-matched token the
  // payload itself doesn't (e.g. notify.*.kv.{bucket}.> — the KV inspector
  // recovers {context, key} from the subject, not the payload). An
  // empty-body message (kvstore.Store.publishNotify's Delete marker) yields
  // payload=null rather than being treated as a JSON parse failure — only a
  // non-empty, malformed body is silently skipped. Fire-and-forget (matches
  // seafreight-app's own subscribe): a missed message during a brief
  // disconnect is covered by whichever REST bootstrap call the panel already
  // makes on (re)connect, not by anything this function does.
  function subscribe(subject, callback) {
    if (!nc) throw notConnectedError()
    const sub = nc.subscribe(subject)
    ;(async () => {
      for await (const msg of sub) {
        if (msg.data.length === 0) {
          callback(null, msg.subject)
          continue
        }
        try {
          callback(JSON.parse(decoder.decode(msg.data)), msg.subject)
        } catch {
          // malformed non-empty payload — skip rather than throw out of the iterator
        }
      }
    })()
    return () => sub.unsubscribe()
  }

  return { connected, lastError, connect, disconnect, subscribe }
}
