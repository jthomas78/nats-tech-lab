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

export function getKnownPorts(context) {
  return request(`/api/meta/${context}/known-ports`)
}

// ── SSE stream URLs ───────────────────────────────────────────────────────────

export function watchUrl(context) {
  return `/api/watch/${context}`
}

export function watchTerminalUrl(context) {
  return `/api/watch-terminal/${context}`
}
