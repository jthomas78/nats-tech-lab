import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// Regression test for the empty-context guard in connect() — App.vue calls
// store.connect() unconditionally after loadContexts(), which leaves context
// at '' when getRefdataContexts() fails (e.g. refdata-service not yet ready
// right after a restart). connect() must bail out without fetching anything
// rather than passing an empty-string account to getKvBucketEntries.

vi.mock('../api', () => ({
  getKvBucketEntries: vi.fn(),
  getPorts: vi.fn(),
  getRefdataContexts: vi.fn(),
}))
vi.mock('../nats/useNatsConnection.js', () => ({
  useNatsConnection: vi.fn(),
}))

import { getKvBucketEntries, getPorts } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'
import { useDictionaryStore } from './dictionary'

describe('useDictionaryStore.connect (context guard)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    useNatsConnection.mockReturnValue({ tenant: { value: 'acme' } })
  })

  it('does nothing when context is empty — no fetch', async () => {
    const store = useDictionaryStore()
    expect(store.context).toBe('')

    await store.connect()

    expect(getPorts).not.toHaveBeenCalled()
    expect(getKvBucketEntries).not.toHaveBeenCalled()
  })

  it('fetches ports and KV snapshot once a context is set', async () => {
    getPorts.mockResolvedValue({ values: [] })
    getKvBucketEntries.mockResolvedValue([])
    const store = useDictionaryStore()

    store.setContext('acme')
    await Promise.resolve() // let the fire-and-forget connect() microtask settle

    // Bootstrap fetch must pass the connected NATS account (from useNatsConnection),
    // not just the business-unit context, because bucket names collide across accounts.
    expect(getKvBucketEntries).toHaveBeenCalledWith('acme', 'ships')
    expect(getPorts).toHaveBeenCalledWith('acme')
  })
})
