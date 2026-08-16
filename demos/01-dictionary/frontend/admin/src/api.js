// Thin client over the demo backend. Most of this file is REST — relative
// paths so the vite dev proxy (dev) or nginx (docker) routes them to the
// backend. The trading-partner functions at the bottom are the exception:
// Phase 26h moved those onto NATS api.* subjects.

import { useNatsConnection } from './nats/useNatsConnection.js'

async function request(path, options = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  if (res.status === 204) return null
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    const err = new Error(body.error || `${res.status} ${res.statusText}`)
    err.status = res.status
    throw err
  }
  return body
}

// ── Ship commands ─────────────────────────────────────────────────────────────

export function arrivePort(input) {
  return request('/api/ships/arrive', { method: 'POST', body: JSON.stringify(input) })
}

export function departPort(input) {
  return request('/api/ships/depart', { method: 'POST', body: JSON.stringify(input) })
}

// ── Container commands ────────────────────────────────────────────────────────

export function registerContainer(input) {
  return request('/api/containers/register', { method: 'POST', body: JSON.stringify(input) })
}

export function loadContainer(input) {
  return request('/api/containers/load', { method: 'POST', body: JSON.stringify(input) })
}

export function unloadContainer(input) {
  return request('/api/containers/unload', { method: 'POST', body: JSON.stringify(input) })
}

// ── Terminal / meta queries ───────────────────────────────────────────────────

export function listContainers(context) {
  return request(`/api/containers/${context}`)
}

export function getTerminal(context, port) {
  return request(`/api/terminal/${context}/${encodeURIComponent(port)}`)
}

export function getManifest(context, shipID) {
  return request(`/api/manifest/${context}/${shipID}`)
}

// ── Ports (Postgres-backed reference table, BR-017/BR-018) ───────────────────

export function getPorts(context) {
  return request(`/api/ports/${context}`)
}

export function registerPort(context, name) {
  return request('/api/ports', { method: 'POST', body: JSON.stringify({ context, name }) })
}

// Raw ports table rows (name + createdAt) for the admin Postgres Tables
// panel — distinct from getPorts, which returns names only for dropdowns.
export function getPortsTable(context) {
  return request(`/api/admin/ports/${context}`)
}

export function getKnownContainers(context) {
  return request(`/api/meta/${context}/known-containers`)
}

// ── Shape B reads (KV cache → Postgres) ──────────────────────────────────────

export function getShipShapeB(context, shipID) {
  return request(`/api/shape-b/ships/${context}/${shipID}`)
}

export function evictShipCache(context, shipID) {
  return request(`/api/shape-b/cache/${context}/${shipID}`, { method: 'DELETE' })
}

// ── Shape C fleet reconstruction ─────────────────────────────────────────────

export function getFleet() {
  return request('/api/shape-c/fleet')
}

// Every event stream across every account this backend reaches (tagged with
// its account), with run-time status — backs the Streams view's stream rail.
// Deliberately NOT scoped to the topbar's active tenant, same as
// listKVBuckets below. KV_* backing streams are excluded server-side.
export function listStreams() {
  return request('/api/jetstream/streams')
}

// Every KV bucket across every account this backend reaches (tagged with
// its account), with run-time status — backs the KV inspector's bucket
// rail. Deliberately NOT scoped to the topbar's active tenant.
export function listKVBuckets() {
  return request('/api/kv/buckets')
}

// ── One-shot bootstrap reads (Phase 23) ──────────────────────────────────────
// Replace the snapshot/replay half of the SSE streams below — each returns a
// single JSON array reflecting current state at request time. Live updates
// after this snapshot arrive over the tenant/platform NATS WebSocket
// connections (src/nats/) via notify.* subscriptions instead.

export function getKvBucketEntries(account, bucket) {
  return request(`/api/kv/buckets/${encodeURIComponent(account)}/${encodeURIComponent(bucket)}/entries`)
}

// account disambiguates the stream name, which is only unique WITHIN an
// account (every tenant provisions its own SHIPPING) — same reason
// getKvBucketEntries above takes one. Omit it to fall back to whichever tenant
// the backend currently has active.
export function getJetstreamReplay(account, stream = 'SHIPPING') {
  const query = new URLSearchParams({ stream })
  if (account) query.set('account', account)
  return request(`/api/jetstream/replay?${query}`)
}

// ── Tenant switch (Phase 13b) ─────────────────────────────────────────────────
// Distinct from fleet context (getPorts above): this reconnects
// shipping-service's NATS connection under a different account entirely, so
// every ship/container endpoint's data changes, not just what a query filters.

export function getTenant() {
  return request('/api/tenant')
}

export function switchTenant(tenant) {
  return request('/api/tenant/switch', { method: 'POST', body: JSON.stringify({ tenant }) })
}

// ── Refdata contexts (Phase 16f) ───────────────────────────────────────────────
// Replaces the previously hardcoded CONTEXTS array in stores/dictionary.js —
// scoped to whichever tenant switchTenant above last selected.

export function getRefdataContexts() {
  return request('/api/refdata/contexts').then((body) => body.values ?? [])
}

// ── Accounts (Phase 14c) ───────────────────────────────────────────────────────
// Dynamic tenant provisioning via accounts-service, proxied at /api/platform/
// (nginx.conf / vite.config.js inject the shared basic-auth secret — the
// browser never handles it). Distinct from getTenant/switchTenant above:
// those talk to shipping-service about which account it's *currently*
// connected as; these talk to accounts-service about which accounts *exist*.

export function listAccounts() {
  return request('/api/platform/accounts')
}

export function createAccount(input) {
  return request('/api/platform/accounts', { method: 'POST', body: JSON.stringify(input) })
}

export function getAccount(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}`)
}

export function suspendAccount(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/suspend`, { method: 'POST' })
}

export function reactivateAccount(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/reactivate`, { method: 'POST' })
}

export function updateAccountLimits(name, limits) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/jslimits`, { method: 'POST', body: JSON.stringify(limits) })
}

export function getAccountsUsage() {
  return request('/api/platform/accounts/usage')
}

// Live export/import edges across every account, read from each account's
// current resolver JWT (not the bootstrap-time convention) — powers the
// Topology tab.
export function getAccountsTopology() {
  return request('/api/platform/accounts/topology')
}

// ── System settings (BR-AC20) ──────────────────────────────────────────────
// Platform-global config, not account-scoped — served under the same
// /api/platform/accounts proxy prefix (basic-auth injected by nginx/vite) as
// the collection-level /usage and /topology endpoints. Currently just the
// browser/admin JWT expiry TTL policy; the response also carries the read-only
// envelope bounds so the UI can constrain its editors.

export function getSystemConfig() {
  return request('/api/platform/accounts/system-config')
}

export function updateSystemConfig(input) {
  return request('/api/platform/accounts/system-config', { method: 'PUT', body: JSON.stringify(input) })
}

// ── Business Units (Phase 22) ─────────────────────────────────────────────────

export function listBusinessUnits(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/business-units`)
}

export function createBusinessUnit(name, input) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/business-units`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function updateBusinessUnit(name, buName, input) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/business-units/${encodeURIComponent(buName)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
}

// ── NATS Connections + Services (Phase 17c) ──────────────────────────────────
// Both are snapshot reads, not SSE — /connz and $SRV.STATS are inherently
// poll-based (a single request/reply round-trip), unlike the KV/JetStream
// watches above which have a native "replay then live" model.

export function getNatsConnections() {
  return request('/api/nats/connections')
}

export function getNatsServices() {
  return request('/api/nats/services')
}

export function getNatsAccountActivity() {
  return request('/api/nats/account-activity')
}

// Log panel — tails NATS's own log_file server-side (level/q filters, tail
// hard-capped at 1000 regardless of what's requested). REST-polled like the
// two above, not a push/follow transport.
export function getNatsLog({ level, q, tail } = {}) {
  const params = new URLSearchParams()
  if (level) params.set('level', level)
  if (q) params.set('q', q)
  if (tail) params.set('tail', tail)
  const qs = params.toString()
  return request(`/api/nats/log${qs ? `?${qs}` : ''}`)
}

// ── Trading Partners (Phase 26) ───────────────────────────────────────────────
// Shipper/Transporter registration — own service (trading-partner-service).
//
// Phase 26h: these are the only functions in this file that do NOT use the
// fetch-based request() above. They go over NATS on
// api.{context}.trading-partner.{entity}.{action}.v1, via the tenant
// connection's request() (nats/connectionFactory.js). trading-partner-service
// still serves the equivalent REST routes — it's a dual transport, same as
// pricing-service — so the REST path remains available for curl/debugging;
// the browser just no longer takes it.
//
// Two consequences of the transport that are visible here:
//
//   - {context} is a subject token, not a path segment, and the service reads
//     it from the subject rather than any body field. It still needs escaping,
//     but as a *subject token*: NATS subject tokens cannot contain dots or
//     spaces, and a context value is already constrained to that shape
//     (ValidateContextName, BR-D33), so tpSubject asserts rather than encodes.
//   - addFleetAsset no longer takes `tenant`. REST needed it because HTTP had
//     no tenant identity; over NATS the tenant is the account the connection
//     authenticated into, and the service reads it from there. Passing it in a
//     body would be ignored — see browserrpc/adapter.go's package doc.

// tpSubject builds one trading-partner api.* subject, failing loudly if
// context isn't a legal single subject token. Silently producing
// "api.acme.north.trading-partner..." would shift every later token by one and
// make the service resolve the wrong context.
function tpSubject(context, entity, action) {
  if (!context || /[.\s*>]/.test(context)) {
    throw new Error(`invalid context for a NATS subject token: ${JSON.stringify(context)}`)
  }
  return `api.${context}.trading-partner.${entity}.${action}.v1`
}

function tpRequest(context, entity, action, payload) {
  return useNatsConnection().request(tpSubject(context, entity, action), payload)
}

export function listTradingPartners(context) {
  return tpRequest(context, 'partner', 'list')
}

export function registerTradingPartner(context, input) {
  return tpRequest(context, 'partner', 'register', input)
}

export function activateTradingPartner(context, id) {
  return tpRequest(context, 'partner', 'activate', { id })
}

export function suspendTradingPartner(context, id, reason) {
  return tpRequest(context, 'partner', 'suspend', { id, reason })
}

export function reactivateTradingPartner(context, id) {
  return tpRequest(context, 'partner', 'reactivate', { id })
}

export function getTradingPartnerAudit(context, id) {
  return tpRequest(context, 'partner', 'audit', { id })
}

export function listComplianceDocuments(context, id) {
  return tpRequest(context, 'document', 'list', { id })
}

export function addComplianceDocument(context, id, input) {
  return tpRequest(context, 'document', 'add', { id, ...input })
}

export function approveComplianceDocument(context, id, type) {
  return tpRequest(context, 'document', 'approve', { id, type })
}

export function rejectComplianceDocument(context, id, type) {
  return tpRequest(context, 'document', 'reject', { id, type })
}

export function resubmitComplianceDocument(context, id, type) {
  return tpRequest(context, 'document', 'resubmit', { id, type })
}

export function listFleetAssets(context, id) {
  return tpRequest(context, 'fleet-asset', 'list', { id })
}

// Signature keeps `tenant` so TradingPartnersPanel.vue's call site is
// unchanged, but the value is deliberately unused: the service derives the
// tenant from the connection's own NATS account. Dropping the parameter would
// silently change the meaning of the 4th positional argument (input) at every
// call site, which is a worse trade than one documented unused parameter.
// eslint-disable-next-line no-unused-vars
export function addFleetAsset(context, id, tenant, input) {
  return tpRequest(context, 'fleet-asset', 'add', { id, ...input })
}
