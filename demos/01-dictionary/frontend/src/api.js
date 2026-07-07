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

export function createEntry(input) {
  return request('/api/entries', { method: 'POST', body: JSON.stringify(input) })
}

export function updateEntry(input) {
  const { context, entityType, id, ...rest } = input
  return request(`/api/entries/${context}/${entityType}/${id}`, {
    method: 'PUT',
    body: JSON.stringify(rest),
  })
}

export function getShapeA(context, entityType, id) {
  return request(`/api/shape-a/entries/${context}/${entityType}/${id}`)
}

export function getShapeB(context, entityType, id) {
  return request(`/api/shape-b/entries/${context}/${entityType}/${id}`)
}

export function evictShapeBCache(context, entityType, id) {
  return request(`/api/shape-b/cache/${context}/${entityType}/${id}`, { method: 'DELETE' })
}

export function watchUrl(context) {
  return `/api/watch/${context}`
}
