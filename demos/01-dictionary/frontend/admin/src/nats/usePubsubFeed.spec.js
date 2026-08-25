import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { usePubsubFeed } from './usePubsubFeed.js'

// Phase 43c (BR-048/BR-047). The pub/sub sibling of useTraceFeed: one KV
// entry is ONE envelope keyed by spanId, not a whole trace's spans array, and
// each entry carries the tenant pubsubstore derived from the import remap.
// Specs are derived from those two rules rather than from the implementation.

vi.mock('../api', () => ({ getKvBucketEntries: vi.fn() }))

const subscribeMock = vi.fn()
const connected = ref(false)
vi.mock('./usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ connected, subscribe: subscribeMock }),
}))

import { getKvBucketEntries } from '../api'

function envelope(spanId, subject, extra = {}) {
  return { spanId, traceId: `t-${spanId}`, direction: 'publish', subject, timestamp: '2026-08-25T10:00:00Z', ...extra }
}
function kvEntry(tenant, span) {
  return { key: `_platform.msg.${span.spanId}`, op: 'PUT', revision: 1, value: { tenant, span } }
}

// Mount a throwaway component so onMounted/onUnmounted actually run. Every
// wrapper is unmounted between specs: the mocked `connected` ref is shared
// module state, so a component left mounted from an earlier spec re-subscribes
// on the next false->true flip and its handler lands in subscribeMock ahead of
// the one under test.
const mounted = []
afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

async function mountFeed(options) {
  let api
  const wrapper = mount({
    setup() {
      api = usePubsubFeed(options)
      return () => null
    },
  })
  mounted.push(wrapper)
  await flushPromises()
  return { api: () => api, wrapper }
}

describe('usePubsubFeed (Phase 43c, BR-048)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    connected.value = false
    getKvBucketEntries.mockResolvedValue([
      kvEntry('acme', envelope('s1', 'evt.acme.shipping.ship.S1.arrived')),
      kvEntry('globex', envelope('s2', 'notify.globex.shipping.ship.changed')),
    ])
  })

  it('bootstraps one message per KV entry, keyed by spanId', async () => {
    const { api } = await mountFeed()

    expect(getKvBucketEntries).toHaveBeenCalledWith('platform', 'pubsub-messages')
    expect([...api().messages.value.keys()]).toEqual(['s1', 's2'])
  })

  it('carries the tenant from the KV record, not from the envelope payload', async () => {
    // The tenant is derived server-side from the monitor.{tenant}.pubsub.>
    // import remap (BR-047) precisely because it is NOT in the envelope — a
    // tenant could otherwise write whatever it liked into its own payload.
    const { api } = await mountFeed()

    expect(api().messages.value.get('s1').tenant).toBe('acme')
    expect(api().messages.value.get('s2').tenant).toBe('globex')
    expect(api().messages.value.get('s1').span.subject).toBe('evt.acme.shipping.ship.S1.arrived')
  })

  it('skips entries that are deletes or carry no span', async () => {
    getKvBucketEntries.mockResolvedValue([
      { key: '_platform.msg.gone', op: 'DEL', value: null },
      { key: '_platform.msg.empty', op: 'PUT', value: { tenant: 'acme' } },
      kvEntry('acme', envelope('s1', 'evt.acme.shipping.ship.S1.arrived')),
    ])

    const { api } = await mountFeed()

    expect([...api().messages.value.keys()]).toEqual(['s1'])
  })

  it('reports a failed bootstrap without throwing, since the live feed still works', async () => {
    getKvBucketEntries.mockRejectedValue(new Error('nope'))

    const { api } = await mountFeed()

    expect(api().bootstrapFailed.value).toBe(true)
    expect(api().messages.value.size).toBe(0)
  })

  it('subscribes to the pubsub-messages KV notify subject once connected', async () => {
    connected.value = true

    await mountFeed()

    expect(subscribeMock).toHaveBeenCalledWith('notify._platform.kv.pubsub-messages.>', expect.any(Function))
  })

  it('upserts a live envelope, taking the spanId from the notify key', async () => {
    connected.value = true
    const onUpsert = vi.fn()
    const { api } = await mountFeed({ onUpsert })

    const handler = subscribeMock.mock.calls[0][1]
    handler({ tenant: 'acme', span: envelope('live1', 'evt.acme.shipping.ship.S9.departed') },
      'notify._platform.kv.pubsub-messages.msg.live1.changed')

    expect(api().messages.value.get('live1').span.subject).toBe('evt.acme.shipping.ship.S9.departed')
    expect(api().messages.value.get('live1').tenant).toBe('acme')
    expect(onUpsert).toHaveBeenCalledWith('live1', expect.objectContaining({ tenant: 'acme' }))
  })

  it('marks the feed as having dropped once the connection goes down', async () => {
    connected.value = true
    const { api } = await mountFeed()
    expect(api().everDisconnected.value).toBe(false)

    connected.value = false
    await flushPromises()

    expect(api().everDisconnected.value).toBe(true)
  })
})
