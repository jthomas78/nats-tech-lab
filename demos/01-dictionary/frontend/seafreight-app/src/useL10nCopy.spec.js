import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useL10nCopy } from '@refdata/useL10nCopy.js'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'

// Regression test for the out-of-order-response race: switching locale while
// an earlier fetch (e.g. the initial connect() call) is still in flight must
// not let that slower, now-stale request win and revert the newer switch.
class FakeEventSource {
  constructor() {}
  close() {}
}

function deferred() {
  let resolve
  const promise = new Promise((r) => {
    resolve = r
  })
  return { promise, resolve }
}

// Resolves the deferred fetch as a genuine (ok, successful) Response —
// resolving with a raw data object instead makes fetchJSON's `res.json()`
// throw (no such method), silently routing through the catch/fallback
// branch instead of the success path the test means to exercise.
function mockFetchReturning(deferredEntry) {
  return deferredEntry.promise.then((items) => ({ ok: true, json: () => Promise.resolve({ items }) }))
}

describe('useL10nCopy request-ordering guard', () => {
  beforeEach(() => {
    global.EventSource = FakeEventSource
  })

  afterEach(() => {
    const { disconnect } = useL10nCopy()
    disconnect()
    useRefdataLabels().selectedLocale.value = 'en'
    // Inert, never-resolving-with-real-data stub — a stray leftover async
    // call (e.g. a watcher reacting to the selectedLocale reset above) must
    // not attempt a real network connection using whatever fetch happy-dom
    // provides by default.
    global.fetch = vi.fn(() => Promise.reject(new Error('fetch called after test teardown')))
  })

  it('does not let a slow, stale fetch revert a locale the user has since switched to', async () => {
    const { selectedLocale } = useRefdataLabels()
    const { switching, connect } = useL10nCopy()
    selectedLocale.value = 'en'

    const enFetch = deferred()
    const esFetch = deferred()
    global.fetch = vi.fn((url) => {
      const locale = new URL(url, 'http://localhost').searchParams.get('locale')
      return mockFetchReturning(locale === 'es' ? esFetch : enFetch)
    })

    const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {}, es: {} } })

    connect(i18n) // starts the initial 'en' fetch (slow — resolved last, below)
    expect(switching.value).toBe(true)

    selectedLocale.value = 'es' // starts a second, concurrent 'es' fetch
    await Promise.resolve() // let the watcher's refreshCatalog() call start

    // The newer 'es' request resolves first...
    esFetch.resolve([{ code: 'app.title', label: 'SeaFreight Flow' }])
    await new Promise((r) => setTimeout(r, 0))
    expect(i18n.global.locale.value).toBe('es')
    expect(switching.value).toBe(false)

    // ...then the older, now-stale 'en' request finally resolves too.
    enFetch.resolve([{ code: 'app.title', label: 'SeaFreight Flow' }])
    await new Promise((r) => setTimeout(r, 0))

    // The stale 'en' response must not clobber the user's 'es' selection.
    expect(i18n.global.locale.value).toBe('es')
  })
})

// BR-D19 regression: cold paint must render the persisted locale's
// last-known-good catalog immediately, not the bundled `en` default, while
// the live refetch is still in flight.
describe('useL10nCopy BR-D19 catalog cache', () => {
  beforeEach(() => {
    global.EventSource = FakeEventSource
    localStorage.clear()
  })

  afterEach(() => {
    const { disconnect } = useL10nCopy()
    disconnect()
    useRefdataLabels().selectedLocale.value = 'en'
    localStorage.clear()
    global.fetch = vi.fn(() => Promise.reject(new Error('fetch called after test teardown')))
  })

  it('applies a cached catalog synchronously in connect(), ahead of the live fetch', async () => {
    localStorage.setItem(
      'refdata.stringCache',
      JSON.stringify({ 'af-za': { messages: { 'app.title': 'SeaFreight Vloei (cached)' }, partialFallback: false } }),
    )
    const { selectedLocale } = useRefdataLabels()
    selectedLocale.value = 'af-za'

    const liveFetch = deferred()
    global.fetch = vi.fn(() => mockFetchReturning(liveFetch))

    const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {} } })
    const { connect } = useL10nCopy()
    connect(i18n)

    // Synchronous, before the live fetch has resolved (or even been awaited).
    expect(i18n.global.locale.value).toBe('af-za')
    expect(i18n.global.getLocaleMessage('af-za')['app.title']).toBe('SeaFreight Vloei (cached)')

    // The live fetch still lands on top once it resolves.
    liveFetch.resolve([{ code: 'app.title', label: 'SeaFreight Vloei' }])
    await new Promise((r) => setTimeout(r, 0))
    expect(i18n.global.getLocaleMessage('af-za')['app.title']).toBe('SeaFreight Vloei')
  })

  it('caches a successfully fetched catalog so the next connect() can prime from it', async () => {
    const { selectedLocale } = useRefdataLabels()
    selectedLocale.value = 'af-za'
    global.fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ items: [{ code: 'app.title', label: 'SeaFreight Vloei' }] }),
      }),
    )

    const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {} } })
    const { connect } = useL10nCopy()
    connect(i18n)
    await new Promise((r) => setTimeout(r, 0))

    const cache = JSON.parse(localStorage.getItem('refdata.stringCache'))
    expect(cache['af-za'].messages['app.title']).toBe('SeaFreight Vloei')
  })
})
