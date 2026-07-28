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

// ── SSE stream URLs ───────────────────────────────────────────────────────────

// One bucket's WatchAll feed: current contents (replay) → INIT_DONE → live
// changes. Both the contents snapshot and the update feed come from this one
// connection.
export function kvBucketWatchUrl(bucket) {
  return `/api/kv/buckets/${encodeURIComponent(bucket)}/watch`
}


export function watchUrl(context) {
  return `/api/watch/${context}`
}

export function watchTerminalUrl(context) {
  return `/api/watch-terminal/${context}`
}

export function jetstreamWatchUrl(stream = 'SHIPPING') {
  return `/api/jetstream/watch?stream=${encodeURIComponent(stream)}`
}
export function jetstreamStreamUrl(stream = 'SHIPPING') {
  return `/api/jetstream/stream?stream=${encodeURIComponent(stream)}`
}

// obs.rpc.* dual-transport RPC traffic (Phase 12.10) — replays up to the
// last 10 minutes from RPCTRACE on connect, then continues live (BR-D29).
export function rpcWatchUrl() {
  return '/api/rpc-watch'
}

// ── Tenant switch (Phase 18b) ─────────────────────────────────────────────────
// Distinct from fleet context (getPorts/watchUrl above): this reconnects
// shipping-service's NATS connection under a different account entirely, so
// every ship/container endpoint's data changes, not just what a query filters.

export function getTenant() {
  return request('/api/tenant')
}

export function switchTenant(tenant) {
  return request('/api/tenant/switch', { method: 'POST', body: JSON.stringify({ tenant }) })
}
