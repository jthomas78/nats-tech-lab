// Thin client over the demo backend REST API. All paths are relative so the
// vite dev proxy (dev) or nginx (docker) can route them to the backend.

async function request(path, options = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  if (res.status === 204) return null
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(body.error || `${res.status} ${res.statusText}`)
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

// Names of every stream registered on the NATS server — backs the Streams
// view's "+" picker so it reflects what's actually provisioned rather than a
// hardcoded list.
export function listStreams() {
  return request('/api/jetstream/streams')
}

// Every KV bucket registered on the NATS server, with run-time status —
// backs the KV inspector's bucket rail.
export function listKVBuckets() {
  return request('/api/kv/buckets')
}

// ── One-shot bootstrap reads (Phase 23) ──────────────────────────────────────
// Replace the snapshot/replay half of the SSE streams below — each returns a
// single JSON array reflecting current state at request time. Live updates
// after this snapshot arrive over the tenant/platform NATS WebSocket
// connections (src/nats/) via notify.* subscriptions instead.

export function getKvBucketEntries(bucket) {
  return request(`/api/kv/buckets/${encodeURIComponent(bucket)}/entries`)
}

export function getJetstreamReplay(stream = 'SHIPPING') {
  return request(`/api/jetstream/replay?stream=${encodeURIComponent(stream)}`)
}

export function getRpcTraceReplay() {
  return request('/api/rpctrace/replay')
}

// ── Tenant switch (Phase 13b) ─────────────────────────────────────────────────
// Distinct from fleet context (getPorts/watchUrl above): this reconnects
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
