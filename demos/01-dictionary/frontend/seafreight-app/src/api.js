// Sea Freight Flow's data-access layer (Phase 15d) — NATS request/reply over
// the single browser WebSocket connection (nats/useNatsConnection.js)
// instead of REST + SSE. Every command/query below is the api.* frontend-
// to-service counterpart of what used to be a `fetch()` call — renamed from
// rpc.* in Phase 16b, since every caller here is the browser, never another
// backend service (rpc.* is now service-to-service only); see
// BUSINESS_RULES-SHIPPING.md's BR-023 for the transport contract and
// dictionary/internal/browserrpc/adapter.go for the server side.
//
// Refdata calls (below) are the one exception — they stay on REST + SSE,
// unchanged: refdata-service runs cross-tenant on the DEFAULT NATS account
// (BR-D08), which is out of scope for this phase (see Main-POC-Plan.md
// Phase 15's Context section).

import { request } from './nats/useNatsConnection'

function apiSubject(context, entity, action) {
  return `api.${context}.shipping.${entity}.${action}.v1`
}

// ── Ship commands ─────────────────────────────────────────────────────────────

export function arrivePort(input) {
  return request(apiSubject(input.context, 'ship', 'arrive'), input)
}

export function departPort(input) {
  return request(apiSubject(input.context, 'ship', 'depart'), input)
}

// ── Container commands ────────────────────────────────────────────────────────

export function registerContainer(input) {
  return request(apiSubject(input.context, 'container', 'register'), input)
}

export function loadContainer(input) {
  return request(apiSubject(input.context, 'container', 'load'), input)
}

export function unloadContainer(input) {
  return request(apiSubject(input.context, 'container', 'unload'), input)
}

// ── Ports (Postgres-backed reference table, BR-017/BR-018) ───────────────────

export function getPorts(context) {
  return request(apiSubject(context, 'port', 'list'), {}).then((body) => body.values)
}

export function registerPort(context, name) {
  return request(apiSubject(context, 'port', 'register'), { name })
}

// ── Bootstrap queries (Phase 15d) — replace the SSE initial-snapshot the
// pre-Phase-15 stores relied on (KV WatchAll's replay-then-live semantics).
// Called once on connect/reconnect; notify.* (below) keeps state fresh
// after that. ─────────────────────────────────────────────────────────────

export function listShips(context) {
  return request(apiSubject(context, 'ship', 'list'), {}).then((body) => body.ships)
}

export function listContainers(context) {
  return request(apiSubject(context, 'container', 'list'), {}).then((body) => body.containers)
}

export function knownContainers(context) {
  return request(apiSubject(context, 'meta', 'known-containers'), {}).then((body) => body.values)
}

// ── notify.* subject builders (Phase 15b) — for stores to pass to
// useNatsConnection's subscribe(). Not REST URLs; kept here so every
// api.*/notify.* subject in this app is built in one place. ─────────────

export function notifySubject(context, entity) {
  return `notify.${context}.shipping.${entity}.changed`
}

// ── Refdata contexts (Phase 16f) — the one REST call in this file; see the
// module comment above for why refdata reads stay on REST. Replaces the
// previously hardcoded CONTEXTS array in stores/port.js. ────────────────

export async function getRefdataContexts() {
  const res = await fetch('/api/refdata/contexts')
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body.values ?? []
}
