// Browser-NATS-WebSocket composable (Phase 15d) — Sea Freight Flow's
// replacement for REST + SSE. Module-level singleton state, same convention
// as shared/refdata/useRefdataLabels.js: one NATS WebSocket connection per
// browser tab, shared by every component/store in the app, instead of the
// ~5 SSE streams the pre-Phase-15 architecture opened (the original
// per-tab-hangs-on-a-2nd-tab problem this phase exists to fix — see
// .claude/memory/admin_ui_realtime_transport_options.md).
//
// Lives in this app's own src/ tree, not shared/ (unlike useRefdataLabels.js)
// — same reason src/i18n.js gives: Vite/Rollup resolves a bare npm
// specifier (here, @nats-io/nats-core) relative to the importing file, and
// a file outside the app root can't resolve a package installed only in
// this app's own node_modules. Only Sea Freight Flow uses NATS WebSocket in
// Phase 15 (Admin/Dictionary are explicitly out of scope), so there's no
// present need to solve that constraint for this module.
//
// Credentials come from GET /api/auth/connectInfo (Phase 15c — originally
// a standalone auth-service, folded into accounts-service as its `auth`
// sub-package in Phase 19, see BUSINESS_RULES-ACCOUNTS.md) — a short-lived
// (5 min), permission-restricted NATS user JWT scoped to exactly
// api.>/notify.> (rpc.> as of Phase 15c/15d, narrowed to api.> in Phase 16b
// once shipping-service's browser-facing subjects moved off rpc.*, which is
// now service-to-service only) within whichever tenant account the caller
// asked for (accounts-service/auth's MintBrowserToken doc comment has the
// full reasoning on why the subject pattern is NOT parameterized by tenant
// — tenant isolation is the NATS account boundary itself).
//
// request()/subscribe() below are the browser's only two verbs, matching
// Main-POC-Plan.md Phase 15's interaction model:
//   - request(subject, payload)  -> api.{ctx}.shipping.{entity}.{action}.v1  (commands/queries)
//   - subscribe(subject, cb)     -> notify.{ctx}.shipping.{entity}.changed  (reactive updates)

import { ref } from 'vue'
import { headers, jwtAuthenticator, wsconnect } from '@nats-io/nats-core'

import { REQUESTOR_HEADER, REST_REQUESTOR_ID } from '../requestorId.js'

const REQUEST_TIMEOUT_MS = 5000

// Identifies this frontend as the caller on every api.* request — NATS auth
// identity (the tenant JWT) lives at the connection level and never reaches
// browserrpc's handler Msg, so without this header a handler (and the Admin
// UI's Request/Reply panel) has no way to tell which app sent the request.
// Tenant isn't part of this value: that's already the NATS account boundary
// itself, visible server-side, and repeating it here would just duplicate
// what the account already encodes.
//
// The value is "<app>/<instance ID>", symmetric with Nats-Responder's
// format (the same service.name/service.instance.id split OpenTelemetry's
// resource conventions use). The instance half is persisted by requestorId.js
// in localStorage, so it survives refreshes and identifies one browser profile
// across its tabs.
// Both halves now come from requestorId.js so this app's one REST call
// (api.js's getBusinessUnits) declares the same identity as its api.* calls.
const REQUESTOR_ID = REST_REQUESTOR_ID

const connected = ref(false)
const tenant = ref('')
const lastError = ref('')

let nc = null
let connectSeq = 0 // guards against a stale in-flight connect() resolving after a newer one started

const encoder = new TextEncoder()
const decoder = new TextDecoder()

// errorMessage unwraps an Error to its bare message — String(err) would
// prefix it with "Error: ", which is noise once this reaches an operator (it
// lands in the topbar's connection-error tooltip and, via notConnectedError
// below, in command-failure toasts).
function errorMessage(err) {
  return err instanceof Error ? err.message : String(err)
}

// notConnectedError is what request()/subscribe() throw when there is no live
// connection. It prefers lastError over the bare "not connected" whenever one
// is known: a suspended tenant (ARCHITECTURE-ACCOUNTS.md § 2t-a) leaves nc
// null with lastError holding the connectInfo endpoint's actual refusal
// ("tenant is not active"), and surfacing *that* in the resulting "Depart
// failed" toast tells the operator what happened, where "not connected" only
// names the transport symptom. Centralised here rather than in each panel's
// catch block so every command — arrive, depart, register, load, unload —
// gets it at once.
function notConnectedError() {
  return new Error(lastError.value || 'not connected')
}

async function fetchConnectInfo(forTenant) {
  const res = await fetch(`/api/auth/connectInfo?tenant=${encodeURIComponent(forTenant)}`, {
    headers: { [REQUESTOR_HEADER]: REST_REQUESTOR_ID },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body // { wsUrl, jwt, nkeySeed, tenant }
}

// connect authenticates into forTenant's NATS account and opens the single
// WebSocket connection every request()/subscribe() call below shares. Safe
// to call again with a different tenant — it disconnects the previous
// connection first (see disconnect()).
export async function connect(forTenant) {
  // disconnect() itself bumps connectSeq (so a stale in-flight connect()
  // it interrupts sees the mismatch below) — mySeq must be captured AFTER
  // that call, not before, or this connect's own disconnect() call makes
  // its own race-guard check fail unconditionally, closing the connection
  // the instant it opens.
  await disconnect()
  const mySeq = ++connectSeq

  const info = await fetchConnectInfo(forTenant)
  const authenticator = jwtAuthenticator(info.jwt, encoder.encode(info.nkeySeed))

  const conn = await wsconnect({ servers: info.wsUrl, authenticator, name: REQUESTOR_ID })
  if (mySeq !== connectSeq) {
    // A newer connect() call started (e.g. a rapid tenant switch) while this
    // one was still authenticating/dialing — this connection lost the race,
    // close it rather than leaving two live connections.
    conn.close()
    return
  }

  nc = conn
  tenant.value = forTenant
  connected.value = true
  lastError.value = ''

  // Best-effort auto-recovery: the JWT is short-lived (accounts-service/
  // auth's MintBrowserToken doc comment, 5 min TTL, no refresh flow yet), so a tab
  // left open past expiry eventually exhausts the client's own internal
  // reconnect attempts (same stale credentials every retry) and nc.closed()
  // resolves. Re-authenticating from scratch (a fresh connectInfo call gets
  // a fresh JWT) is simpler than building a refresh-token flow for this POC.
  conn.closed().then((err) => {
    if (nc !== conn) return // already superseded by a later connect()/disconnect()
    connected.value = false
    if (err) lastError.value = errorMessage(err)
    connect(forTenant).catch((reconnectErr) => {
      lastError.value = errorMessage(reconnectErr)
    })
  })
}

export async function disconnect() {
  connectSeq++ // invalidate any in-flight connect() for the previous tenant
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

export async function switchTenant(newTenant) {
  await connect(newTenant)
}

// request performs one api.* call: JSON-encodes payload, sends it, decodes
// the JSON response. Throws if the server replied with an {error} envelope
// (browserrpc.errorResponse, shipping-service's
// dictionary/internal/browserrpc/adapter.go) so callers can use ordinary
// try/catch instead of checking a field.
export async function request(subject, payload) {
  if (!nc) throw notConnectedError()
  const h = headers()
  h.set(REQUESTOR_HEADER, REQUESTOR_ID)
  const msg = await nc.request(subject, encoder.encode(JSON.stringify(payload ?? {})), {
    timeout: REQUEST_TIMEOUT_MS,
    headers: h,
  })
  const body = msg.data.length ? JSON.parse(decoder.decode(msg.data)) : {}
  if (body.error) throw new Error(body.error)
  return body
}

// subscribe listens on a notify.* subject, JSON-decoding each message and
// invoking callback(payload). Returns an unsubscribe function. Fire-and-
// forget by design (Phase 15b/BR-024) — a missed message during a brief
// disconnect is covered by the caller re-running its api.*.list.v1
// bootstrap query on reconnect, not by anything this function does.
export function subscribe(subject, callback) {
  if (!nc) throw notConnectedError()
  const sub = nc.subscribe(subject)
  ;(async () => {
    for await (const msg of sub) {
      try {
        callback(JSON.parse(decoder.decode(msg.data)))
      } catch {
        // malformed payload — skip rather than throw out of the iterator
      }
    }
  })()
  return () => sub.unsubscribe()
}

export function useNatsConnection() {
  return { connected, tenant, lastError, connect, disconnect, switchTenant, request, subscribe }
}
