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

// ── Ports (Postgres-backed reference table, BR-017/BR-018) ───────────────────

export function getPorts(context) {
  return request(`/api/ports/${context}`)
}

export function registerPort(context, name) {
  return request('/api/ports', { method: 'POST', body: JSON.stringify({ context, name }) })
}

// ── SSE stream URLs ───────────────────────────────────────────────────────────

export function watchUrl(context) {
  return `/api/watch/${context}`
}

export function watchTerminalUrl(context) {
  return `/api/watch-terminal/${context}`
}

// ── Tenant switch (Phase 13b) ─────────────────────────────────────────────────
// Distinct from fleet context above: this reconnects shipping-service's NATS
// connection under a different account entirely, so every ship/container
// endpoint's data changes, not just what a query filters.

export function getTenant() {
  return request('/api/tenant')
}

export function switchTenant(tenant) {
  return request('/api/tenant/switch', { method: 'POST', body: JSON.stringify({ tenant }) })
}
