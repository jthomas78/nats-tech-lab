// Thin client over the refdata-service REST API. All paths are relative so
// the vite dev proxy (dev) or nginx (docker) can route them to the service.

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

// ── Types ──────────────────────────────────────────────────────────────────────

export function listTypes(context) {
  return request(`/api/refdata/${context}/types`)
}

export function registerType(input) {
  return request('/api/refdata/admin/types', { method: 'POST', body: JSON.stringify(input) })
}

// ── Items ──────────────────────────────────────────────────────────────────────

export function listItems(context, typeKey, { all = false, locale = '' } = {}) {
  const params = new URLSearchParams()
  if (all) params.set('all', 'true')
  if (locale) params.set('locale', locale)
  const qs = params.toString()
  return request(`/api/refdata/${context}/${typeKey}${qs ? `?${qs}` : ''}`)
}

export function getItem(context, typeKey, code, { locale = '', expand = '' } = {}) {
  const params = new URLSearchParams()
  if (locale) params.set('locale', locale)
  if (expand) params.set('expand', expand)
  const qs = params.toString()
  return request(`/api/refdata/${context}/${typeKey}/${code}${qs ? `?${qs}` : ''}`)
}

export function registerItem(input) {
  return request('/api/refdata/admin/items', { method: 'POST', body: JSON.stringify(input) })
}

export function deprecateItem(typeKey, context, code) {
  return request(`/api/refdata/admin/items/${typeKey}/${context}/${code}/deprecate`, { method: 'POST' })
}

export function reactivateItem(typeKey, context, code) {
  return request(`/api/refdata/admin/items/${typeKey}/${context}/${code}/reactivate`, { method: 'POST' })
}

export function deleteItem(typeKey, context, code) {
  return request(`/api/refdata/admin/items/${typeKey}/${context}/${code}`, { method: 'DELETE' })
}

// ── References ─────────────────────────────────────────────────────────────────

export function createReference(input) {
  return request('/api/refdata/admin/references', { method: 'POST', body: JSON.stringify(input) })
}

export function listItemReferences(context, typeKey, code) {
  return request(`/api/refdata/${context}/${typeKey}/${code}/references`)
}

// ── Localization ───────────────────────────────────────────────────────────────

export function setLocalization(input) {
  return request('/api/refdata/admin/localizations', { method: 'POST', body: JSON.stringify(input) })
}

export function listItemLocalizations(context, typeKey, code) {
  return request(`/api/refdata/${context}/${typeKey}/${code}/localizations`)
}

export function listLocales(context) {
  return request(`/api/refdata/${context}/locales`)
}

export function addLocale(context, locale, isDefault) {
  return request('/api/refdata/admin/locales', {
    method: 'POST',
    body: JSON.stringify({ context, locale, isDefault }),
  })
}

export function getCompleteness(context, typeKey, locale) {
  return request(`/api/refdata/${context}/${typeKey}/completeness?locale=${encodeURIComponent(locale)}`)
}

export function getCacheStatus(context, typeKey) {
  return request(`/api/refdata/${context}/${typeKey}/cache-status`)
}

// ── SSE stream URL ─────────────────────────────────────────────────────────────

export function watchUrl(context) {
  return `/api/refdata-watch/${context}`
}
