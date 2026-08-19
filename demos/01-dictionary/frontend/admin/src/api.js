// Thin client over the demo backend. Most of this file is REST — relative
// paths so the vite dev proxy (dev) or nginx (docker) routes them to the
// backend. The ship/container/port/meta functions below and the
// trading-partner functions further down are the exceptions: Phase 26h moved
// trading-partner calls onto NATS api.* subjects, and Phase 33.8 did the same
// for shipping-service's ship/container/port/meta business calls once its
// REST routes were retired (Phase 33, BR-039) — shipTerminal/getTerminal's
// Terminal.ListByPort query has no api.* equivalent (browserrpc/adapter.go
// only exposes container-list/container-manifest) and was unused by any
// component, so it was dropped rather than migrated; add an api.* endpoint
// first if a by-port terminal view is ever built.

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

// shippingSubject builds one shipping-service api.* subject
// (api.{context}.shipping.{entity}.{action}.v1 — browserrpc/adapter.go),
// failing loudly if context isn't a legal single subject token. Mirrors
// tpSubject below for trading-partner-service.
function shippingSubject(context, entity, action) {
  if (!context || /[.\s*>]/.test(context)) {
    throw new Error(`invalid context for a NATS subject token: ${JSON.stringify(context)}`)
  }
  return `api.${context}.shipping.${entity}.${action}.v1`
}

function shippingRequest(context, entity, action, payload) {
  return useNatsConnection().request(shippingSubject(context, entity, action), payload)
}

// ── Ship commands ─────────────────────────────────────────────────────────────

export function arrivePort(input) {
  return shippingRequest(input.context, 'ship', 'arrive', input)
}

export function departPort(input) {
  return shippingRequest(input.context, 'ship', 'depart', input)
}

// ── Container commands ────────────────────────────────────────────────────────

export function registerContainer(input) {
  return shippingRequest(input.context, 'container', 'register', input)
}

export function loadContainer(input) {
  return shippingRequest(input.context, 'container', 'load', input)
}

export function unloadContainer(input) {
  return shippingRequest(input.context, 'container', 'unload', input)
}

// ── Terminal / meta queries ───────────────────────────────────────────────────

export function listContainers(context) {
  return shippingRequest(context, 'container', 'list')
}

export function getManifest(context, shipID) {
  return shippingRequest(context, 'container', 'manifest', { shipID })
}

// ── Ports (Postgres-backed reference table, BR-017/BR-018) ───────────────────

export function getPorts(context) {
  return shippingRequest(context, 'port', 'list')
}

export function registerPort(context, name) {
  return shippingRequest(context, 'port', 'register', { name })
}

// Raw ports table rows (name + createdAt) for the admin Postgres Tables
// panel — distinct from getPorts, which returns names only for dropdowns.
// Stays on REST: an admin diagnostics route (/api/admin/ports/{context}),
// never a business one — see BR-039/Phase 33's admin vs. business split.
export function getPortsTable(context) {
  return request(`/api/admin/ports/${context}`)
}

export function getKnownContainers(context) {
  return shippingRequest(context, 'meta', 'known-containers')
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

// ── Refdata contexts (Phase 16f; moved onto api.* in Phase 32) ────────────────
// Replaces the previously hardcoded CONTEXTS array in stores/dictionary.js.
//
// Phase 32 repointed this from shipping-service's retired /api/refdata/contexts
// relay to refdata-service's own api.* subject — this call is the reason that
// relay route existed, and removing the conduit is the point of the phase.
// context.list is a business subject, not an admin one (BR-D41): reading the
// context tree for a dropdown is a plain read, so a browser credential
// reaches it without the denied api.*.refdata.admin.> prefix.
//
// The tenant filter travels in the request body (BR-D35) — refdata-service
// has no server-supplied caller identity to derive it from (BR-D34), and the
// per-tenant connection only proves which ACCOUNT the caller is in, which is
// a different axis from the `tenant` column contexts are tagged with.
// Returns bare context names, matching the shape the REST relay returned so
// stores/dictionary.js's loadContexts() is unchanged.
export function getRefdataContexts() {
  const { request: natsRequest, tenant } = useNatsConnection()
  return natsRequest('api._platform.refdata.context.list.v1', { tenant: tenant.value }).then((body) =>
    (body.contexts ?? []).map((c) => c.context),
  )
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

// Overview tab's trend charts (Phase 45, BR-043) — bucketed history from
// observability-service's 60-minute ring buffer, not the live snapshot
// above. duration must be '5m', '30m', or '1h'.
export function getNatsAccountActivityHistory(duration) {
  return request(`/api/nats/account-activity/history?duration=${encodeURIComponent(duration)}`)
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
