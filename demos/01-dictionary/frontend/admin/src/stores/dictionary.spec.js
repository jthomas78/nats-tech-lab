import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// Regression test for a real production incident (2026-08-05, surfaced by
// the Admin UI's Log panel): App.vue calls store.connect() unconditionally
// after loadContexts(), which leaves this.context at its initial '' when
// getRefdataContexts() fails (e.g. refdata-service not yet ready right
// after a restart). connect() used to subscribe anyway, producing a
// malformed notify..kv.{bucket}.> subject (empty {context} token) that the
// NATS server correctly rejected as a Subscription Violation.

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
  let subscribe

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    subscribe = vi.fn(() => () => {})
    useNatsConnection.mockReturnValue({ connected: { value: true }, subscribe })
  })

  it('does nothing when context is empty — no subscribe, no bootstrap fetch, stays disconnected', async () => {
    const store = useDictionaryStore()
    expect(store.context).toBe('')

    await store.connect()

    expect(subscribe).not.toHaveBeenCalled()
    expect(getPorts).not.toHaveBeenCalled()
    expect(getKvBucketEntries).not.toHaveBeenCalled()
    expect(store.connected).toBe(false)
  })

  it('subscribes on the real context once one is set', async () => {
    getPorts.mockResolvedValue({ values: [] })
    getKvBucketEntries.mockResolvedValue([])
    const store = useDictionaryStore()

    store.setContext('acme')
    await Promise.resolve() // let the fire-and-forget connect() microtask settle

    expect(subscribe).toHaveBeenCalledWith('notify.acme.kv.dict-a.>', expect.any(Function))
    expect(subscribe).toHaveBeenCalledWith('notify.acme.kv.dict-b.>', expect.any(Function))
    expect(store.connected).toBe(true)
  })
})
