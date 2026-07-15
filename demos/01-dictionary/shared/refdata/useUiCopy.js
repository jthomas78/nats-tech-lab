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

import { ref, watch } from 'vue'

import { uiCopyFallbackEn } from './uiCopyFallback.en.js'
import { useRefdataLabels } from './useRefdataLabels.js'

const TYPE_KEY = 'ui-copy'
const { selectedLocale } = useRefdataLabels()

const usingFallback = ref(false)
const partialFallback = ref(false)

let i18n = null
let source = null
let started = false

async function fetchJSON(path) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' } })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

async function refreshCatalog() {
  if (!i18n) return
  const locale = selectedLocale.value || 'en'
  try {
    const data = await fetchJSON(`/api/refdata/types/${TYPE_KEY}?locale=${encodeURIComponent(locale)}`)
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
  } catch {
    usingFallback.value = true
    partialFallback.value = false
    i18n.global.setLocaleMessage(locale, { ...uiCopyFallbackEn })
  }
  i18n.global.locale.value = locale
}

function connect(i18nInstance) {
  i18n = i18nInstance
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
  return { usingFallback, partialFallback, connect, disconnect }
}
