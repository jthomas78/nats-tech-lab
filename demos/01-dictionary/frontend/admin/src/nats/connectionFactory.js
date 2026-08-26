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

import { REQUESTOR_HEADER, requestorID as buildRequestorID } from '../requestorId.js'

const encoder = new TextEncoder()
const decoder = new TextDecoder()

// Matches seafreight-app's convention (Phase 18): a per-tab identity the
// Admin UI's own Request/Reply panel can attribute traffic to, and the value
// organizations-service records as an audit row's sourceIP (NATS has no
// client address to record instead — see browserrpc's actor()).
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
  // epoch increments on every (re)establishment of the underlying socket —
  // BOTH kinds. A consumer of a fire-and-forget subscription must resync its
  // REST snapshot on each bump, so it needs a signal that fires for both:
  //
  //   - the OUTER reconnect, where closed() resolved and connect() built a
  //     brand-new NatsConnection. `connected` flips false then true, so a
  //     watch on `connected` alone does see this one.
  //   - the INNER reconnect, where nats-core's own reconnect logic re-dialled
  //     and restored subscriptions on the SAME NatsConnection. closed() never
  //     resolves and `connected` never flips, so a watch on `connected` is
  //     blind to it — yet it drops core-NATS messages exactly like the outer
  //     one does, because a re-established subscription is not a replayed one.
  //
  // The inner case is the common one: a NATS restart or a brief network blip
  // is absorbed entirely inside the client, so watching `connected` for
  // "should I resync?" silently misses most real gaps. Verified against a
  // running stack — `docker compose restart nats` left `connected` true
  // throughout and fired no resync at all until this was added.
  const epoch = ref(0)

  // Named per connection so the tenant and PLATFORM connections are
  // distinguishable in the Request/Reply panel, over one tab-wide instance
  // half (requestorId.js) so this app's REST calls and its api.* calls read
  // as the same actor rather than two unrelated ones.
  const requestorID = buildRequestorID(connectionName)

  let nc = null
  let connectSeq = 0
  // wantConnected records intent, separately from whether a socket is up right
  // now: it stays true across a failed reconnect so the retry loop below knows
  // it should keep going, and only a caller-initiated disconnect() clears it.
  let wantConnected = false
  let retrying = false

  function notConnectedError() {
    return new Error(lastError.value || 'not connected')
  }

  function sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms))
  }

  // teardown drops the current socket without clearing intent — connect()'s own
  // "close whatever is open first" step. The exported disconnect() below is
  // this plus "and stay down".
  async function teardown() {
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

  async function disconnect() {
    wantConnected = false
    await teardown()
  }

  // retryConnect keeps trying until it succeeds or a caller disconnects.
  //
  // The single non-retrying attempt this replaces left the app permanently
  // dark: nats-core resolves closed() only after exhausting its OWN reconnect
  // budget, so by the time we get here the server has usually been unreachable
  // for a while and the one retry fails too — after which nothing ever tried
  // again and every panel stayed frozen until the user reloaded the page.
  // Verified against a running stack: `docker compose stop nats`, wait ~30s,
  // `docker compose start nats` left the UI disconnected indefinitely while a
  // manual reload connected instantly.
  async function retryConnect() {
    if (retrying) return
    retrying = true
    let delay = 500
    try {
      while (wantConnected && !nc) {
        try {
          await connect()
          return
        } catch (err) {
          lastError.value = errorMessage(err)
        }
        await sleep(delay)
        delay = Math.min(delay * 2, 10000)
      }
    } finally {
      retrying = false
    }
  }

  async function connect() {
    wantConnected = true
    await teardown()
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
    epoch.value++

    // Track nats-core's own reconnect cycle. `connected` is reported honestly
    // through it (false while re-dialling) so a panel can say it is stale, and
    // epoch bumps on recovery so it can resync. The iteration ends by itself
    // when the connection closes.
    ;(async () => {
      for await (const status of conn.status()) {
        if (nc !== conn) return
        if (status.type === 'disconnect') {
          connected.value = false
        } else if (status.type === 'reconnect') {
          connected.value = true
          epoch.value++
        }
      }
    })().catch(() => {
      // status() iteration errors are not actionable — closed() below is the
      // authoritative end-of-connection signal.
    })

    conn.closed().then((err) => {
      if (nc !== conn) return
      nc = null
      connected.value = false
      if (err) lastError.value = errorMessage(err)
      retryConnect()
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
  // disconnect is covered by the caller re-reading its REST snapshot on every
  // `epoch` bump, not by anything this function does. Watch `epoch`, not
  // `connected` — see epoch's comment above for why `connected` misses the
  // reconnect that actually happens most often.
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

  return { connected, epoch, lastError, connect, disconnect, subscribe, request }
}
