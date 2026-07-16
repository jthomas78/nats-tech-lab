import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useUiCopy } from '@refdata/useUiCopy.js'
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

describe('useUiCopy request-ordering guard', () => {
  beforeEach(() => {
    global.EventSource = FakeEventSource
  })

  afterEach(() => {
    const { disconnect } = useUiCopy()
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
    const { switching, connect } = useUiCopy()
    selectedLocale.value = 'en'

    const enFetch = deferred()
    const esFetch = deferred()
    global.fetch = vi.fn((url) => {
      const locale = new URL(url, 'http://localhost').searchParams.get('locale')
      return (locale === 'es' ? esFetch : enFetch).promise
    })

    const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {}, es: {} } })

    connect(i18n) // starts the initial 'en' fetch (slow — resolved last, below)
    expect(switching.value).toBe(true)

    selectedLocale.value = 'es' // starts a second, concurrent 'es' fetch
    await Promise.resolve() // let the watcher's refreshCatalog() call start

    // The newer 'es' request resolves first...
    esFetch.resolve({ items: [{ code: 'app.title', label: 'SeaFreight Flow' }] })
    await new Promise((r) => setTimeout(r, 0))
    expect(i18n.global.locale.value).toBe('es')
    expect(switching.value).toBe(false)

    // ...then the older, now-stale 'en' request finally resolves too.
    enFetch.resolve({ items: [{ code: 'app.title', label: 'SeaFreight Flow' }] })
    await new Promise((r) => setTimeout(r, 0))

    // The stale 'en' response must not clobber the user's 'es' selection.
    expect(i18n.global.locale.value).toBe('es')
  })
})
