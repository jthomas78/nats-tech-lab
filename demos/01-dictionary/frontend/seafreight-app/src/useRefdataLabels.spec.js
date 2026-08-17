import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// Regression test for locale-selection persistence: selectedLocale is
// module-level state, initialized once from localStorage at import time —
// vi.resetModules() + a fresh dynamic import is what simulates "the page
// reloaded" here, since a plain re-call of useRefdataLabels() within the
// same module instance would just return the already-initialized ref.
const STORAGE_KEY = 'refdata.locale'

describe('useRefdataLabels locale persistence', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('defaults to en when nothing is stored', async () => {
    const { useRefdataLabels } = await import('@refdata/useRefdataLabels.js')
    const { selectedLocale } = useRefdataLabels()
    expect(selectedLocale.value).toBe('en')
  })

  it('initializes from a previously persisted locale', async () => {
    localStorage.setItem(STORAGE_KEY, 'es')
    const { useRefdataLabels } = await import('@refdata/useRefdataLabels.js')
    const { selectedLocale } = useRefdataLabels()
    expect(selectedLocale.value).toBe('es')
  })

  it('persists a new selection so it survives a reload', async () => {
    const { useRefdataLabels } = await import('@refdata/useRefdataLabels.js')
    const { selectedLocale } = useRefdataLabels()

    selectedLocale.value = 'af-za'
    await new Promise((r) => setTimeout(r, 0)) // let the persistence watcher run

    expect(localStorage.getItem(STORAGE_KEY)).toBe('af-za')

    // Simulate a reload: a fresh module instance re-reads localStorage.
    vi.resetModules()
    const { useRefdataLabels: useRefdataLabelsAfterReload } = await import('@refdata/useRefdataLabels.js')
    const { selectedLocale: selectedLocaleAfterReload } = useRefdataLabelsAfterReload()

    expect(selectedLocaleAfterReload.value).toBe('af-za')
  })
})

// fakeTransport stands in for an app's NATS connection (Phase 32) — the
// { request, subscribe } pair setRefdataTransport expects. reply is keyed by
// subject so one transport can serve both the type.list and locales.list
// calls connect() makes.
function fakeTransport(reply = {}) {
  return {
    request: vi.fn((subject) => Promise.resolve(reply[subject] ?? {})),
    subscribe: vi.fn(() => () => {}),
  }
}

const TYPE_LIST = 'api._platform.refdata.type.list.v1'
const LOCALES_LIST = 'api._platform.refdata.locales.list.v1'

// BR-D19 regression: cold paint must render the persisted locale's
// last-known-good ship-status labels immediately, not the hardcoded English
// SHIP_STATUS_FALLBACK, while the live refetch is still in flight.
describe('useRefdataLabels ship-status label cache', () => {
  const LABELS_CACHE_KEY = 'refdata.shipStatusLabelsCache'

  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('seeds labels synchronously at module load from a cached map for the persisted locale', async () => {
    localStorage.setItem(STORAGE_KEY, 'af-za')
    localStorage.setItem(LABELS_CACHE_KEY, JSON.stringify({ 'af-za': { docked: 'Vasgemeer (cached)' } }))

    const { useRefdataLabels } = await import('@refdata/useRefdataLabels.js')
    const { labels } = useRefdataLabels()

    // No connect() call, no await — this must already be correct synchronously.
    expect(labels.value.docked).toBe('Vasgemeer (cached)')
  })

  it('caches a successfully fetched label map so the next load can prime from it', async () => {
    localStorage.setItem(STORAGE_KEY, 'af-za')

    const { setRefdataTransport, useRefdataLabels } = await import('@refdata/useRefdataLabels.js')
    // api.* nests the item, unlike the flat shape the retired REST relay
    // returned — flattenTypeList is what normalizes it back.
    setRefdataTransport(
      fakeTransport({
        [TYPE_LIST]: { items: [{ item: { code: 'docked' }, label: 'Vasgemeer' }] },
        [LOCALES_LIST]: { locales: [], defaultLocale: '' },
      }),
    )
    const { connect } = useRefdataLabels()
    connect()
    await new Promise((r) => setTimeout(r, 0))

    const cache = JSON.parse(localStorage.getItem(LABELS_CACHE_KEY))
    expect(cache['af-za'].docked).toBe('Vasgemeer')

    // Simulate a reload: a fresh module instance seeds `labels` from that cache.
    vi.resetModules()
    const { useRefdataLabels: useRefdataLabelsAfterReload } = await import('@refdata/useRefdataLabels.js')
    const { labels: labelsAfterReload } = useRefdataLabelsAfterReload()
    expect(labelsAfterReload.value.docked).toBe('Vasgemeer')
  })
})
