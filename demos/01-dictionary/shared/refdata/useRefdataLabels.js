// Shared refdata-label composable for the shipping frontends (Phase 11.6).
//
// The shipping backend resolves ship-status labels from the refdata-service
// KV cache (KV-first, REST fallback — BR-D08) and exposes them at
// /api/refdata/types/ship-status?locale=. This composable fetches that
// code→label map for a chosen locale, keeps it fresh via the
// /api/refdata-watch SSE stream (the same KV-watch→SSE→refetch pattern the
// dictionary UI uses), and hands components a statusLabel(code) helper that
// degrades gracefully to a built-in English map if refdata is unreachable.
//
// State is module-level so the topbar locale <Select> and every panel in an
// app share one instance and one SSE connection. All paths are relative, so
// the vite dev proxy / nginx routes them to the backend.
//
// selectedLocale persists to localStorage (per-origin, so frontend-port and
// frontend on their different dev ports don't share a choice) — no backend
// user/session concept exists to store a preference server-side instead, and
// a URL query param would need hand-rolled history sync since neither app
// uses a router. No validation against the fetched `locales` list on read:
// BR-D03's fallback chain already degrades a stale/invalid persisted locale
// to the default rather than breaking anything.
//
// BR-D19: the last-successfully-fetched label map for each locale is also
// cached and seeded into `labels` synchronously at module load (not just the
// locale choice) — otherwise, on a reload into a persisted non-en locale,
// statusLabel() would show the hardcoded English SHIP_STATUS_FALLBACK for
// the length of the refetch, mismatching the locale shown as selected. See
// useUiCopy.js's matching cache for the full rationale (same bug, same fix).

import { ref, watch } from 'vue'

const TYPE_KEY = 'ship-status'
const LOCALE_STORAGE_KEY = 'refdata.locale'
const LABELS_CACHE_KEY = 'refdata.shipStatusLabelsCache'

function readStoredLocale() {
  try {
    return localStorage.getItem(LOCALE_STORAGE_KEY) || 'en'
  } catch {
    return 'en' // storage disabled/unavailable (e.g. some private-browsing modes)
  }
}

function readLabelsCache() {
  try {
    return JSON.parse(localStorage.getItem(LABELS_CACHE_KEY) || '{}')
  } catch {
    return {}
  }
}

function writeLabelsCacheEntry(locale, map) {
  try {
    const cache = readLabelsCache()
    cache[locale] = map
    localStorage.setItem(LABELS_CACHE_KEY, JSON.stringify(cache))
  } catch {
    // storage disabled/unavailable — just won't prime cold paint next time
  }
}

// Built-in English fallback — used when the backend/refdata is unreachable or
// a code has no localization. Mirrors the labels seeded for `ship-status`.
const SHIP_STATUS_FALLBACK = {
  'in-transit': 'In Transit',
  docked: 'Docked',
  'at-anchor': 'At Anchor',
  'not-under-command': 'Not Under Command',
  'restricted-manoeuvrability': 'Restricted Manoeuvrability',
}

const initialLocale = readStoredLocale()
const labels = ref(readLabelsCache()[initialLocale] || {}) // code → label, resolved for selectedLocale
const locales = ref([]) // locales registered in refdata (for the switcher)
const selectedLocale = ref(initialLocale) // '' would mean raw codes; defaults to the persisted choice, then English
const connected = ref(false)

let source = null
let started = false
// Other composables (useUiCopy) need the same "something in refdata
// changed" signal but must not open a second EventSource to
// /api/refdata-watch — every persistent connection permanently occupies one
// of the browser's ~6-per-origin slots, and this app already needs several
// (KV watch, JetStream tabs). subscribeToChange() lets them react to this
// module's one shared connection instead.
const changeListeners = new Set()

export function subscribeToChange(fn) {
  changeListeners.add(fn)
  return () => changeListeners.delete(fn)
}
// Bumped on every refreshLabels() call — same out-of-order-response guard as
// useUiCopy.js's refreshCatalog(): a switch triggers a new fetch while an
// earlier one (e.g. the initial connect() fetch) may still be in flight, and
// responses aren't guaranteed to resolve in request order.
let requestToken = 0

async function fetchJSON(path) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' } })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

async function refreshLabels() {
  const myToken = ++requestToken
  try {
    const q = selectedLocale.value ? `?locale=${encodeURIComponent(selectedLocale.value)}` : ''
    const data = await fetchJSON(`/api/refdata/types/${TYPE_KEY}${q}`)
    if (myToken !== requestToken) return // a newer request has since started — discard this stale result
    const map = {}
    for (const item of data.items || []) map[item.code] = item.label || item.code
    labels.value = map
    writeLabelsCacheEntry(selectedLocale.value, map)
  } catch {
    // Keep the last known map; statusLabel() falls back to the built-in map.
  }
}

async function loadLocales() {
  try {
    const data = await fetchJSON('/api/refdata/locales')
    locales.value = data.locales || []
  } catch {
    locales.value = []
  }
}

function connect() {
  if (started) return
  started = true
  loadLocales()
  refreshLabels()
  source = new EventSource('/api/refdata-watch')
  source.onopen = () => {
    connected.value = true
  }
  source.onerror = () => {
    connected.value = false
  }
  // The SSE feed is a "something changed" signal (no payload we rely on) —
  // any refdata KV change re-fetches the label map for the current locale.
  source.onmessage = () => {
    refreshLabels()
    for (const fn of changeListeners) fn()
  }
}

function disconnect() {
  if (source) {
    source.close()
    source = null
  }
  started = false
  connected.value = false
}

// Primes `labels` synchronously from a previously cached map for `locale`
// (BR-D19's cold-paint fix, applied on switch too, not just module load) — a
// no-op if this locale hasn't been fetched yet this session/browser.
function primeLabelsFromCache(locale) {
  const cached = readLabelsCache()[locale]
  if (cached) labels.value = cached
}

// Re-resolve labels whenever the user switches locale, and persist the choice.
watch(selectedLocale, (locale) => {
  primeLabelsFromCache(locale)
  refreshLabels()
  try {
    localStorage.setItem(LOCALE_STORAGE_KEY, locale)
  } catch {
    // storage disabled/unavailable — the choice just won't survive a reload
  }
})

// statusLabel resolves a ship-status code to a display label:
// backend-resolved label → built-in English fallback → caller's fallback
// (e.g. a currentPort-derived string) → the code itself.
function statusLabel(code, fallback) {
  if (!code) return fallback ?? ''
  return labels.value[code] || SHIP_STATUS_FALLBACK[code] || fallback || code
}

export function useRefdataLabels() {
  return { labels, locales, selectedLocale, connected, connect, disconnect, statusLabel }
}
