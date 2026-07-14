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

import { ref, watch } from 'vue'

const TYPE_KEY = 'ship-status'

// Built-in English fallback — used when the backend/refdata is unreachable or
// a code has no localization. Mirrors the labels seeded for `ship-status`.
const SHIP_STATUS_FALLBACK = {
  'in-transit': 'In Transit',
  docked: 'Docked',
  'at-anchor': 'At Anchor',
  'not-under-command': 'Not Under Command',
  'restricted-manoeuvrability': 'Restricted Manoeuvrability',
}

const labels = ref({}) // code → label, resolved for selectedLocale
const locales = ref([]) // locales registered in refdata (for the switcher)
const selectedLocale = ref('en') // '' would mean raw codes; default to English
const connected = ref(false)

let source = null
let started = false

async function fetchJSON(path) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' } })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

async function refreshLabels() {
  try {
    const q = selectedLocale.value ? `?locale=${encodeURIComponent(selectedLocale.value)}` : ''
    const data = await fetchJSON(`/api/refdata/types/${TYPE_KEY}${q}`)
    const map = {}
    for (const item of data.items || []) map[item.code] = item.label || item.code
    labels.value = map
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

// Re-resolve labels whenever the user switches locale.
watch(selectedLocale, () => refreshLabels())

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
