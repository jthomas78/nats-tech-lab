import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// Regression test for the empty-context guard in connect() — App.vue calls
// store.connect() unconditionally after loadContexts(), which leaves context
// at '' when getRefdataContexts() fails (e.g. refdata-service not yet ready
// right after a restart). connect() must bail out without fetching anything
// rather than passing an empty-string account to getKvBucketEntries.

vi.mock('../api', () => ({
  getKvBucketEntries: vi.fn(),
  getRefdataContexts: vi.fn(),
}))

import { getKvBucketEntries } from '../api'
import { useDictionaryStore } from './dictionary'
import { useTenantStore } from './tenant.js'

describe('useDictionaryStore.connect (context guard)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    useTenantStore().tenant = 'acme'
  })

  it('does nothing when context is empty — no fetch', async () => {
    const store = useDictionaryStore()
    expect(store.context).toBe('')

    await store.connect()

    expect(getKvBucketEntries).not.toHaveBeenCalled()
  })

  it('fetches the REST KV snapshot for the backend active account once a context is set', async () => {
    getKvBucketEntries.mockResolvedValue([])
    const store = useDictionaryStore()

    store.setContext('acme')
    await Promise.resolve() // let the fire-and-forget connect() microtask settle

    // Account remains explicit because bucket names collide across accounts;
    // this parameter does not authenticate the browser into that account.
    expect(getKvBucketEntries).toHaveBeenCalledWith('acme', 'ships')
  })
})
