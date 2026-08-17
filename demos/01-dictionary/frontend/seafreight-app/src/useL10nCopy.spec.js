import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useL10nCopy } from '@refdata/useL10nCopy.js'
import { setRefdataTransport, useRefdataLabels } from '@refdata/useRefdataLabels.js'

// Regression test for the out-of-order-response race: switching locale while
// an earlier request (e.g. the initial connect() call) is still in flight
// must not let that slower, now-stale one win and revert the newer switch.

function deferred() {
  let resolve
  const promise = new Promise((r) => {
    resolve = r
  })
  return { promise, resolve }
}

// codesToItems builds the api.* type.list reply shape — the item is nested
// (Phase 32), unlike the flat shape the retired REST relay returned.
function codesToItems(entries) {
  return { items: entries.map(({ code, label }) => ({ item: { code }, label })) }
}

// installTransport wires a fake NATS transport whose type.list reply is
// chosen per requested locale, so a test can resolve two concurrent
// locale requests out of order.
function installTransport(replyForLocale) {
  setRefdataTransport({
    request: vi.fn((subject, payload) => {
      if (subject.endsWith('.locales.list.v1')) return Promise.resolve({ locales: [], defaultLocale: '' })
      return replyForLocale(payload.locale)
    }),
    subscribe: vi.fn(() => () => {}),
  })
}

describe('useL10nCopy request-ordering guard', () => {
  afterEach(() => {
    const { disconnect } = useL10nCopy()
    disconnect()
    useRefdataLabels().selectedLocale.value = 'en'
    // Inert stub — a stray leftover async call (e.g. a watcher reacting to
    // the selectedLocale reset above) must not resolve with real data after
    // the test has finished asserting.
    setRefdataTransport(null)
  })

  it('does not let a slow, stale request revert a locale the user has since switched to', async () => {
    const { selectedLocale } = useRefdataLabels()
    const { switching, connect } = useL10nCopy()
    selectedLocale.value = 'en'

    const enFetch = deferred()
    const esFetch = deferred()
    installTransport((locale) => (locale === 'es' ? esFetch.promise : enFetch.promise))

    const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {}, es: {} } })

    connect(i18n) // starts the initial 'en' fetch (slow — resolved last, below)
    expect(switching.value).toBe(true)

    selectedLocale.value = 'es' // starts a second, concurrent 'es' fetch
    await Promise.resolve() // let the watcher's refreshCatalog() call start

    // The newer 'es' request resolves first...
    esFetch.resolve(codesToItems([{ code: 'app.title', label: 'SeaFreight Flow' }]))
    await new Promise((r) => setTimeout(r, 0))
    expect(i18n.global.locale.value).toBe('es')
    expect(switching.value).toBe(false)

    // ...then the older, now-stale 'en' request finally resolves too.
    enFetch.resolve(codesToItems([{ code: 'app.title', label: 'SeaFreight Flow' }]))
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
    localStorage.clear()
  })

  afterEach(() => {
    const { disconnect } = useL10nCopy()
    disconnect()
    useRefdataLabels().selectedLocale.value = 'en'
    localStorage.clear()
    setRefdataTransport(null)
  })

  it('applies a cached catalog synchronously in connect(), ahead of the live request', async () => {
    localStorage.setItem(
      'refdata.stringCache',
      JSON.stringify({ 'af-za': { messages: { 'app.title': 'SeaFreight Vloei (cached)' }, partialFallback: false } }),
    )
    const { selectedLocale } = useRefdataLabels()
    selectedLocale.value = 'af-za'

    const liveFetch = deferred()
    installTransport(() => liveFetch.promise)

    const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {} } })
    const { connect } = useL10nCopy()
    connect(i18n)

    // Synchronous, before the live request has resolved (or even been awaited).
    expect(i18n.global.locale.value).toBe('af-za')
    expect(i18n.global.getLocaleMessage('af-za')['app.title']).toBe('SeaFreight Vloei (cached)')

    // The live request still lands on top once it resolves.
    liveFetch.resolve(codesToItems([{ code: 'app.title', label: 'SeaFreight Vloei' }]))
    await new Promise((r) => setTimeout(r, 0))
    expect(i18n.global.getLocaleMessage('af-za')['app.title']).toBe('SeaFreight Vloei')
  })

  it('caches a successfully fetched catalog so the next connect() can prime from it', async () => {
    const { selectedLocale } = useRefdataLabels()
    selectedLocale.value = 'af-za'
    installTransport(() => Promise.resolve(codesToItems([{ code: 'app.title', label: 'SeaFreight Vloei' }])))

    const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {} } })
    const { connect } = useL10nCopy()
    connect(i18n)
    await new Promise((r) => setTimeout(r, 0))

    const cache = JSON.parse(localStorage.getItem('refdata.stringCache'))
    expect(cache['af-za'].messages['app.title']).toBe('SeaFreight Vloei')
  })
})
