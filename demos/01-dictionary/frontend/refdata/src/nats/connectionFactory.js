// Shared connect/reconnect/subscribe/request machinery behind
// useRefdataAdminConnection.js (Phase 32) — this app's single PLATFORM-
// account NATS connection.
//
// Duplicated from frontend/admin/src/nats/connectionFactory.js (itself
// modeled on seafreight-app's) rather than imported: Vite/Rollup resolves
// the bare @nats-io/nats-core specifier relative to the importing file, and
// this app has its own separate node_modules from admin's/seafreight-app's.
// Functionally identical — same short-lived-JWT, no-refresh-flow tradeoff,
// same connect/disconnect/subscribe/request shape.

import { ref } from 'vue'
import { headers, jwtAuthenticator, wsconnect } from '@nats-io/nats-core'

const encoder = new TextEncoder()
const decoder = new TextDecoder()

// Matches every other frontend in this repo (Phase 18): a per-tab identity
// the Request/Reply panel can attribute traffic to.
const REQUESTOR_HEADER = 'Nats-Requestor'
const REQUEST_TIMEOUT_MS = 10000

function errorMessage(err) {
  return err instanceof Error ? err.message : String(err)
}

// createConnectionState builds one independent connection's reactive state
// + connect/disconnect/subscribe/request, closing over its own nc/connectSeq.
//
// fetchConnectInfo() must return { wsUrl, jwt, nkeySeed } (accounts-service
// auth/handler.go's ConnectInfo shape) and is called fresh on every
// connect()/reconnect.
export function createConnectionState({ fetchConnectInfo, connectionName }) {
  const connected = ref(false)
  const lastError = ref('')

  // Stable for the tab's lifetime so a series of calls is attributable to
  // one session in the Request/Reply panel.
  const requestorID = `${connectionName}/${crypto.randomUUID().replaceAll('-', '').slice(0, 16)}`

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
  // arrived on, useful when it carries a wildcard-matched token the payload
  // itself doesn't (e.g. notify._platform.refdata.>, where {context}/
  // {typeKey} live in the subject, not the body). An empty-body message
  // yields payload=null. Fire-and-forget: a missed message during a brief
  // disconnect is covered by whichever bootstrap read the caller already
  // makes on (re)connect.
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

  // request performs one api.* call: JSON-encodes payload, sends it, decodes
  // the JSON reply. Throws if the service replied with an {error} envelope
  // (browserrpc.errorResponse — the same shape every service in this repo
  // uses).
  //
  // The envelope's `notFound`/`conflict` discriminators are copied onto the
  // thrown Error rather than dropped. Without this a caller only ever sees
  // the message string, so a 409 is indistinguishable from a 500 and a
  // conflict can only be recognized by matching on prose — which BR-TP39's
  // conflict banner would then be one backend wording change away from
  // silently losing. Both flags default to false, so callers that only read
  // `.message` are unaffected.
  async function request(subject, payload) {
    if (!nc) throw notConnectedError()
    const h = headers()
    h.set(REQUESTOR_HEADER, requestorID)
    const msg = await nc.request(subject, encoder.encode(JSON.stringify(payload ?? {})), {
      timeout: REQUEST_TIMEOUT_MS,
      headers: h,
    })
    const body = msg.data.length ? JSON.parse(decoder.decode(msg.data)) : {}
    if (body.error) {
      const err = new Error(body.error)
      err.notFound = body.notFound === true
      err.conflict = body.conflict === true
      throw err
    }
    return body
  }

  return { connected, lastError, connect, disconnect, subscribe, request }
}
