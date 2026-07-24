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

// Full replace of an item's attrs map (BR-D18) — not a per-key merge, so
// callers must send the complete desired map (spread the current attrs and
// override just the keys changing).
export function updateItem(typeKey, context, code, attrs) {
  return request(`/api/refdata/admin/items/${typeKey}/${context}/${code}/attrs`, {
    method: 'PATCH',
    body: JSON.stringify({ attrs }),
  })
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

// Drafts candidate label/description text per target locale (BR-D07) —
// nothing is persisted by this call; save an accepted draft via
// setLocalization({ ..., source: 'ai' }). Returns { drafts: [{ locale, label,
// description, error }] } — a per-locale `error` means that locale's draft
// failed without aborting the others in the same request.
export function draftTranslation(typeKey, code, context, targetLocales) {
  return request(`/api/refdata/admin/${typeKey}/${code}/translate`, {
    method: 'POST',
    body: JSON.stringify({ context, targetLocales }),
  })
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

// ── Contexts (Phase 12.1 — context hierarchy) ───────────────────────────────────

export function listContexts() {
  return request('/api/refdata/admin/contexts')
}

export function getContext(context) {
  return request(`/api/refdata/admin/contexts/${context}/detail`)
}

export function registerContext(input) {
  return request('/api/refdata/admin/contexts', { method: 'POST', body: JSON.stringify(input) })
}

// ── Corpus versioning (Phase 12.2–12.5) ─────────────────────────────────────────

export function listCorpusVersions(context) {
  return request(`/api/refdata/admin/corpus/${context}/versions`)
}

export function getCorpusVersion(context, version) {
  return request(`/api/refdata/admin/corpus/${context}/versions/${version}`)
}

export function getDraft(context) {
  return request(`/api/refdata/admin/corpus/${context}/draft`)
}

export function createDraft(context, notes = '') {
  return request(`/api/refdata/admin/corpus/${context}/draft`, {
    method: 'POST',
    body: JSON.stringify({ notes }),
  })
}

export function putDraftItem(context, item) {
  return request(`/api/refdata/admin/corpus/${context}/draft/items`, {
    method: 'PUT',
    body: JSON.stringify(item),
  })
}

export function putDraftLocalization(context, localization) {
  return request(`/api/refdata/admin/corpus/${context}/draft/localizations`, {
    method: 'PUT',
    body: JSON.stringify(localization),
  })
}

export function publishCorpus(context) {
  return request(`/api/refdata/admin/corpus/${context}/publish`, { method: 'POST' })
}

export function rollbackCorpus(context, version, notes = '') {
  return request(`/api/refdata/admin/corpus/${context}/rollback/${version}`, {
    method: 'POST',
    body: JSON.stringify({ notes }),
  })
}

export function diffCorpusVersions(context, fromVersion, toVersion) {
  return request(`/api/refdata/admin/corpus/${context}/diff/${fromVersion}/${toVersion}`)
}
