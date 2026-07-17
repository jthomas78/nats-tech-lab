// Shared UI-copy composable (Phase 11.7) — loads vue-i18n's message catalog
// for the `ui-copy` dictionary type from refdata, live via the same
// KV-cached pipeline as domain labels (/api/refdata/types/ui-copy, BR-D08),
// sharing `selectedLocale` with useRefdataLabels so one switcher drives both
// domain labels and UI copy.
//
// connect(i18n) takes the app's own vue-i18n instance rather than importing
// one — this file has no 'vue-i18n' import so it stays resolvable from
// shared/refdata/ (see each app's src/i18n.js for why that boundary matters).
//
// Falls back to the bundled English catalog (BR-D11) in two cases:
//  - the fetch fails outright (refdata-service unreachable) — usingFallback
//  - a key resolves to its own code (BR-D03's fallback chain fell all the
//    way through — no real translation for this locale yet) — partialFallback
// Either flag means the UI should show a visible "using bundled text" badge;
// never silently serve the gap.
//
// BR-D19: the last-successfully-fetched catalog for each locale is cached
// (localStorage) and applied synchronously in connect(), *before* the live
// refetch — otherwise, with the selected locale itself now persisted across
// reloads, every reload into a non-en locale would cold-paint in bundled en
// (the only ever-bundled locale) for the length of the refetch, visibly
// mismatching the locale shown as selected. BR-D11 doesn't cover this: that
// rule is about refdata being *unreachable*; here it's reachable, it just
// hasn't answered *yet*. A locale visited for the very first time still
// cold-paints in en until its first successful fetch — there's no cache to
// prime from yet.
//
// refreshCatalog() is re-entrant: switching locale while a previous switch's
// fetch is still in flight (e.g. right after mount, before the initial fetch
// settles) starts a second overlapping request. Network responses aren't
// guaranteed to resolve in request order, so a request-token guard discards
// any result that's no longer the most recently started one — otherwise a
// slow, stale request can win the race and silently revert a newer locale
// switch. `switching` reflects whether the latest request is still pending,
// so a caller can disable the locale control while a switch is in flight.

import { ref, watch } from 'vue'

import { uiCopyFallbackEn } from './uiCopyFallback.en.js'
import { useRefdataLabels } from './useRefdataLabels.js'

const TYPE_KEY = 'ui-copy'
const CATALOG_CACHE_KEY = 'refdata.uiCopyCache'
const { selectedLocale } = useRefdataLabels()

const usingFallback = ref(false)
const partialFallback = ref(false)
const switching = ref(false)

function readCatalogCache() {
  try {
    return JSON.parse(localStorage.getItem(CATALOG_CACHE_KEY) || '{}')
  } catch {
    return {}
  }
}

function writeCatalogCacheEntry(locale, entry) {
  try {
    const cache = readCatalogCache()
    cache[locale] = entry
    localStorage.setItem(CATALOG_CACHE_KEY, JSON.stringify(cache))
  } catch {
    // storage disabled/unavailable — just won't prime cold paint next time
  }
}

let i18n = null
let source = null
let started = false
// Bumped on every refreshCatalog() call; a call only applies its result if
// it's still the most recently *started* one when its fetch resolves —
// guards against an earlier, slower request (e.g. the initial mount fetch)
// clobbering a locale the user has since switched to.
let requestToken = 0

async function fetchJSON(path) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' } })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

async function refreshCatalog() {
  if (!i18n) return
  const locale = selectedLocale.value || 'en'
  const myToken = ++requestToken
  switching.value = true
  try {
    const data = await fetchJSON(`/api/refdata/types/${TYPE_KEY}?locale=${encodeURIComponent(locale)}`)
    if (myToken !== requestToken) return // a newer request has since started — discard this stale result
    const messages = { ...uiCopyFallbackEn }
    let fellThrough = false
    for (const item of data.items || []) {
      if (item.label && item.label !== item.code) {
        messages[item.code] = item.label
      } else {
        fellThrough = true
      }
    }
    partialFallback.value = fellThrough
    usingFallback.value = false
    i18n.global.setLocaleMessage(locale, messages)
    writeCatalogCacheEntry(locale, { messages, partialFallback: fellThrough })
  } catch {
    if (myToken !== requestToken) return
    usingFallback.value = true
    partialFallback.value = false
    i18n.global.setLocaleMessage(locale, { ...uiCopyFallbackEn })
  }
  i18n.global.locale.value = locale
  switching.value = false
}

// Applies a previously cached catalog for the current locale synchronously,
// ahead of the live refetch (BR-D19) — a no-op if nothing's cached for it yet.
function primeFromCache() {
  const locale = selectedLocale.value || 'en'
  const cached = readCatalogCache()[locale]
  if (!cached) return
  i18n.global.setLocaleMessage(locale, cached.messages)
  i18n.global.locale.value = locale
  partialFallback.value = cached.partialFallback
  usingFallback.value = false
}

function connect(i18nInstance) {
  i18n = i18nInstance
  primeFromCache()
  if (started) {
    refreshCatalog()
    return
  }
  started = true
  refreshCatalog()
  source = new EventSource('/api/refdata-watch')
  source.onmessage = () => refreshCatalog()
}

function disconnect() {
  if (source) {
    source.close()
    source = null
  }
  started = false
}

// Re-resolve the catalog whenever the shared locale switcher changes.
watch(selectedLocale, () => refreshCatalog())

export function useUiCopy() {
  return { usingFallback, partialFallback, switching, connect, disconnect }
}
