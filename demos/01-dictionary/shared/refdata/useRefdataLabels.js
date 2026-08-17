// Shared refdata-label composable for the shipping frontends (Phase 11.6,
// moved onto NATS in Phase 32).
//
// Reads refdata-service directly over api._platform.refdata.type.list.v1 /
// .locales.list.v1 (BR-D41's business subjects) and keeps the map fresh via
// a notify._platform.refdata.*.changed subscription (BR-D42). Before Phase
// 32 this went browser → REST → shipping-service → rpc.* → refdata-service,
// with shipping-service acting as an API conduit for another service's data;
// those five relay routes are gone. Hands components a statusLabel(code)
// helper that degrades gracefully to a built-in English map when refdata is
// unreachable.
//
// The NATS transport is INJECTED via setRefdataTransport rather than imported:
// this file lives in shared/, outside every app's node_modules, and
// Vite/Rollup resolves the bare @nats-io/nats-core specifier relative to the
// importing file — the same constraint that made each app duplicate
// connectionFactory.js instead of sharing one. Each app calls
// setRefdataTransport() with its own connection's { request, subscribe }
// before connect(). With no transport set, every read simply fails soft and
// statusLabel() serves the built-in fallback (BR-D11's existing degradation
// path, unchanged).
//
// State is module-level so the topbar locale <Select> and every panel in an
// app share one instance and one subscription.
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
// useL10nCopy.js's matching cache for the full rationale (same bug, same fix).

import { computed, ref, watch } from 'vue'

import { localeSelectOptions } from './locales.js'

const TYPE_KEY = 'ship-status'
const LOCALE_STORAGE_KEY = 'refdata.locale'
const LABELS_CACHE_KEY = 'refdata.shipStatusLabelsCache'

// The refdata context these UI-chrome reads resolve against — always
// _platform (refdata-service's reserved root), never a tenant- or
// fleet-derived value: ship-status is a fixed AIS vocabulary and `string` is
// fixed frontend chrome text, shared by every business unit of every tenant.
// This is the same value shipping-service's now-deleted refdataCompanyContext
// helper always returned.
const REFDATA_CONTEXT = '_platform'
const TYPE_LIST_SUBJECT = `api.${REFDATA_CONTEXT}.refdata.type.list.v1`
const LOCALES_LIST_SUBJECT = `api.${REFDATA_CONTEXT}.refdata.locales.list.v1`
const CHANGE_SUBJECT = `notify.${REFDATA_CONTEXT}.refdata.*.changed`

// transport is { request(subject, payload), subscribe(subject, cb) } — see
// this file's doc comment for why it's injected rather than imported.
let transport = null

// setRefdataTransport wires this module to an app's own NATS connection.
// Call it before connect(). Passing null detaches (used by tests and by an
// app tearing its connection down).
export function setRefdataTransport(next) {
  transport = next
}

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
const defaultLocale = ref('') // the context's default locale (BR-D32: shown first, marked)
const selectedLocale = ref(initialLocale) // '' would mean raw codes; defaults to the persisted choice, then English
const connected = ref(false)

let unsubscribeChange = null
let started = false
// Other composables (useL10nCopy) need the same "something in refdata
// changed" signal but should not open a second notify.* subscription —
// subscribeToChange() lets them react to this module's one shared
// subscription instead. (Pre-Phase-32 this mattered more acutely: the feed
// was an EventSource, and every persistent HTTP connection permanently
// occupied one of the browser's ~6-per-origin slots. A NATS subscription
// multiplexes over the single WebSocket, so the constraint is now tidiness
// rather than a hard limit — but one shared refetch trigger is still the
// right shape.)
const changeListeners = new Set()

export function subscribeToChange(fn) {
  changeListeners.add(fn)
  return () => changeListeners.delete(fn)
}
// Bumped on every refreshLabels() call — same out-of-order-response guard as
// useL10nCopy.js's refreshCatalog(): a switch triggers a new fetch while an
// earlier one (e.g. the initial connect() fetch) may still be in flight, and
// responses aren't guaranteed to resolve in request order.
let requestToken = 0

// requestRefdata is the single NATS entry point — throws (rather than
// returning a sentinel) when no transport is wired, so every caller's
// existing catch already handles "refdata unreachable" identically to a
// genuine request failure.
async function requestRefdata(subject, payload) {
  if (!transport) throw new Error('refdata transport not configured')
  return transport.request(subject, payload)
}

// requestTypeList is exported for useL10nCopy, which needs the same
// api.*.type.list.v1 call for its own `string` type key.
//
// The api.* reply nests the item — { item: { typeKey, code, context, status },
// label, locale, description, isFallback } per entry — where the retired REST
// shape flattened code/label into one object. flattenTypeList normalizes that
// back to { code, label } so both callers keep the shape they already parse.
export async function requestTypeList(typeKey, locale) {
  return requestRefdata(TYPE_LIST_SUBJECT, { context: REFDATA_CONTEXT, typeKey, locale })
}

export function flattenTypeList(data) {
  return (data.items || []).map((entry) => ({
    code: entry.item?.code ?? '',
    label: entry.label || entry.item?.code || '',
  }))
}

async function refreshLabels() {
  const myToken = ++requestToken
  try {
    const data = await requestTypeList(TYPE_KEY, selectedLocale.value)
    if (myToken !== requestToken) return // a newer request has since started — discard this stale result
    const map = {}
    for (const { code, label } of flattenTypeList(data)) map[code] = label
    labels.value = map
    writeLabelsCacheEntry(selectedLocale.value, map)
  } catch {
    // Keep the last known map; statusLabel() falls back to the built-in map.
  }
}

async function loadLocales() {
  try {
    const data = await requestRefdata(LOCALES_LIST_SUBJECT, { context: REFDATA_CONTEXT })
    locales.value = data.locales || []
    defaultLocale.value = data.defaultLocale || ''
  } catch {
    locales.value = []
    defaultLocale.value = ''
  }
}

function connect() {
  if (started) return
  started = true
  loadLocales()
  refreshLabels()
  if (!transport) {
    // No NATS connection wired — labels stay on the built-in fallback map
    // (BR-D11). Deliberately not an error: an app can mount before its
    // connection is up, then call connect() again once setRefdataTransport
    // has run.
    started = false
    connected.value = false
    return
  }
  try {
    // The notify.* feed is a "something changed" signal (no payload we rely
    // on) — any refdata change re-fetches the label map for the current
    // locale. Wildcard across typeKey: this module cares about ship-status
    // and useL10nCopy about `string`, and one subscription serves both.
    unsubscribeChange = transport.subscribe(CHANGE_SUBJECT, () => {
      refreshLabels()
      for (const fn of changeListeners) fn()
    })
    connected.value = true
  } catch {
    started = false
    connected.value = false
  }
}

function disconnect() {
  unsubscribeChange?.()
  unsubscribeChange = null
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
  // localeOptions is the BR-D32-ordered/labelled option list every locale
  // switcher binds to, so no app re-derives "default first, marked" itself.
  const localeOptions = computed(() => localeSelectOptions(locales.value, defaultLocale.value))
  return {
    labels,
    locales,
    defaultLocale,
    localeOptions,
    selectedLocale,
    connected,
    connect,
    disconnect,
    statusLabel,
  }
}
