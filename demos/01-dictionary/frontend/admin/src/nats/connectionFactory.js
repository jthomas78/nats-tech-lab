// Shared connect/reconnect/subscribe/request machinery behind
// useNatsConnection.js (tenant) and usePlatformConnection.js (PLATFORM) —
// Phase 23's dual-connection model for frontend/admin, replacing its
// EventSource/SSE streams.
//
// This factory was subscribe-only until Phase 26h, on the explicit grounds
// that "adding [a request surface] before anything needs it would be exactly
// the speculative feature CLAUDE.md's 'don't design for hypothetical future
// requirements' warns against". Phase 26h is that need arriving: the Admin
// UI's Organizations screens call organizations-service over
// api.{context}.organizations.* instead of REST.
//
// request() is only usable on the *tenant* connection. The PLATFORM
// credential from GET /api/auth/adminConnectInfo is publish-denied at the JWT
// level (auth/token.go's MintAdminToken sets Pub.Deny = ">"), while the tenant
// credential from GET /api/auth/connectInfo?tenant=… already carries
// Pub.Allow = ["api.>", "_INBOX.>"] — the same MintBrowserToken seafreight
// uses. Calling request() on the platform connection will fail the publish
// authorization, by design; nothing here needs to guard it because no caller
// has a reason to try.
//
// Modeled directly on seafreight-app/src/nats/useNatsConnection.js's
// connect/disconnect/auto-reconnect-on-close shape (same short-lived-JWT,
// no-refresh-flow tradeoff — see that file's doc comment) — duplicated here
// rather than imported because Vite/Rollup resolves the bare
// @nats-io/nats-core specifier relative to the importing file, and this app
// has its own separate node_modules from seafreight-app's.

import { ref } from 'vue'
import { headers, jwtAuthenticator, wsconnect } from '@nats-io/nats-core'

const encoder = new TextEncoder()
const decoder = new TextDecoder()

// Matches seafreight-app's convention (Phase 18): a per-tab identity the
// Admin UI's own Request/Reply panel can attribute traffic to, and the value
// organizations-service records as an audit row's sourceIP (NATS has no
// client address to record instead — see browserrpc's actor()).
const REQUESTOR_HEADER = 'Nats-Requestor'
const REQUEST_TIMEOUT_MS = 10000

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

  // Per-instance so the tenant and PLATFORM connections are distinguishable in
  // the Request/Reply panel, and stable for the tab's lifetime so a series of
  // calls is attributable to one session.
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

  // request performs one api.* call: JSON-encodes payload, sends it, decodes
  // the JSON reply. Throws if the service replied with an {error} envelope
  // (browserrpc.errorResponse — the same shape every service in this repo
  // uses) so callers use ordinary try/catch instead of checking a field, which
  // also keeps the api.js functions' contract identical to the fetch-based
  // ones they replace. Mirrors seafreight-app's request() deliberately.
  async function request(subject, payload) {
    if (!nc) throw notConnectedError()
    const h = headers()
    h.set(REQUESTOR_HEADER, requestorID)
    const msg = await nc.request(subject, encoder.encode(JSON.stringify(payload ?? {})), {
      timeout: REQUEST_TIMEOUT_MS,
      headers: h,
    })
    const body = msg.data.length ? JSON.parse(decoder.decode(msg.data)) : {}
    if (body.error) throw new Error(body.error)
    return body
  }

  return { connected, lastError, connect, disconnect, subscribe, request }
}
