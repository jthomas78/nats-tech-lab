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

export function loadCargo(input) {
  return request('/api/ships/cargo/load', { method: 'POST', body: JSON.stringify(input) })
}

export function unloadCargo(input) {
  return request('/api/ships/cargo/unload', { method: 'POST', body: JSON.stringify(input) })
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

// ── SSE stream URLs ───────────────────────────────────────────────────────────

export function watchUrl(context) {
  return `/api/watch/${context}`
}

export const jetstreamWatchUrl = '/api/jetstream/watch'
export const jetstreamStreamUrl = '/api/jetstream/stream'
