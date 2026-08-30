import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createChangeSubscription } from './changeSubscription.js'
import { createRegistryTransport } from './registryTransport.js'

describe('BR-AS28 — in-flight snapshots do not swallow later hints', () => {
  it('does a trailing read if a burst moved beyond the completed snapshot', async () => {
    let deliver, finish
    const read = vi.fn().mockImplementationOnce(() => new Promise((r) => { finish = r })).mockResolvedValue({ ok: true, revision: 15 })
    const sub = createChangeSubscription({ subscribe: (_s, handler) => { deliver = handler; return { unsubscribe() {} } }, read, currentRevision: () => 12 })
    sub.start()
    deliver({ revision: 13 })
    await flushPromises()
    deliver({ revision: 15 })
    finish({ ok: true, revision: 13 })
    await flushPromises()
    expect(read).toHaveBeenCalledTimes(2)
    sub.stop()
  })

  it('does not process queued hints after stop', async () => {
    let deliver, finish
    const read = vi.fn(() => new Promise((r) => { finish = r }))
    const sub = createChangeSubscription({ subscribe: (_s, handler) => { deliver = handler; return { unsubscribe() {} } }, read, currentRevision: () => 12 })
    sub.start()
    deliver({ revision: 13 })
    await flushPromises()
    deliver({ revision: 14 })
    sub.stop()
    finish({ ok: true, revision: 13 })
    await flushPromises()
    expect(read).toHaveBeenCalledOnce()
  })
})

describe('BR-AS04 — invalid wire answers stay outside the catalog', () => {
  it('keeps the read timestamp and classifies wrapped NATS timeout errors', async () => {
    const transport = createRegistryTransport({ request: async () => ({ ok: true, revision: 1, entries: [] }), now: () => '2026-08-30T19:00:00.000Z' })
    expect((await transport.fetchRegistry()).fetchedAt).toBe('2026-08-30T19:00:00.000Z')
    const failed = createRegistryTransport({ request: async () => { throw Object.assign(new Error('request failed'), { cause: { name: 'TimeoutError' } }) } })
    expect(await failed.fetchRegistry()).toEqual({ ok: false, code: 'registry-timeout' })
  })

  it.each([
    { ok: true, revision: 2, unchanged: true },
    { ok: true, revision: -1, entries: [] },
    { ok: true, revision: 2, entries: [], schemaVersion: 999 },
    { ok: true, revision: 2, entries: [], degraded: true },
  ])('refuses an invalid answer %j without throwing', async (reply) => {
    const transport = createRegistryTransport({ request: async () => reply })
    expect(await transport.fetchRegistry()).toEqual({ ok: false, code: 'registry-malformed' })
  })
})
