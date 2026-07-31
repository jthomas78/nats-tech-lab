// Sea Freight Flow's data-access layer (Phase 15d) — NATS request/reply over
// the single browser WebSocket connection (nats/useNatsConnection.js)
// instead of REST + SSE. Every command/query below is the rpc.* dual-
// transport counterpart of what used to be a `fetch()` call; see
// BUSINESS_RULES-SHIPPING.md's BR-023 for the transport contract and
// dictionary/internal/natsrpc/adapter.go for the server side.
//
// Refdata calls (below) are the one exception — they stay on REST + SSE,
// unchanged: refdata-service runs cross-tenant on the DEFAULT NATS account
// (BR-D08), which is out of scope for this phase (see Main-POC-Plan.md
// Phase 15's Context section).

import { request } from './nats/useNatsConnection'

function rpcSubject(context, entity, action) {
  return `rpc.${context}.shipping.${entity}.${action}.v1`
}

// ── Ship commands ─────────────────────────────────────────────────────────────

export function arrivePort(input) {
  return request(rpcSubject(input.context, 'ship', 'arrive'), input)
}

export function departPort(input) {
  return request(rpcSubject(input.context, 'ship', 'depart'), input)
}

// ── Container commands ────────────────────────────────────────────────────────

export function registerContainer(input) {
  return request(rpcSubject(input.context, 'container', 'register'), input)
}

export function loadContainer(input) {
  return request(rpcSubject(input.context, 'container', 'load'), input)
}

export function unloadContainer(input) {
  return request(rpcSubject(input.context, 'container', 'unload'), input)
}

// ── Ports (Postgres-backed reference table, BR-017/BR-018) ───────────────────

export function getPorts(context) {
  return request(rpcSubject(context, 'port', 'list'), {}).then((body) => body.values)
}

export function registerPort(context, name) {
  return request(rpcSubject(context, 'port', 'register'), { name })
}

// ── Bootstrap queries (Phase 15d) — replace the SSE initial-snapshot the
// pre-Phase-15 stores relied on (KV WatchAll's replay-then-live semantics).
// Called once on connect/reconnect; notify.* (below) keeps state fresh
// after that. ─────────────────────────────────────────────────────────────

export function listShips(context) {
  return request(rpcSubject(context, 'ship', 'list'), {}).then((body) => body.ships)
}

export function listContainers(context) {
  return request(rpcSubject(context, 'container', 'list'), {}).then((body) => body.containers)
}

export function knownContainers(context) {
  return request(rpcSubject(context, 'meta', 'known-containers'), {}).then((body) => body.values)
}

// ── notify.* subject builders (Phase 15b) — for stores to pass to
// useNatsConnection's subscribe(). Not REST URLs; kept here so every
// rpc.*/notify.* subject in this app is built in one place. ─────────────

export function notifySubject(context, entity) {
  return `notify.${context}.shipping.${entity}.changed`
}
