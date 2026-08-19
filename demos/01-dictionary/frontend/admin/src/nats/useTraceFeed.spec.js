// useTraceFeed owns lifecycle hooks (onMounted/onUnmounted/watch), so it
// needs an active component instance to run — mount a trivial host
// component rather than calling it bare, matching how this codebase's
// component specs already exercise bootstrap/live-subscribe behavior
// (PulsePanel.spec.js, TraceWaterfall.spec.js), except this spec covers the
// composable directly: none of the three panels that now share it test the
// live-subscribe/reconnect path themselves (they mock usePlatformConnection
// inert and only exercise the bootstrap path).
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api', () => ({
  getKvBucketEntries: vi.fn(),
}))
vi.mock('./usePlatformConnection.js', () => ({
  usePlatformConnection: () => mockPlatformConnection,
}))

import { getKvBucketEntries } from '../api'
import { useTraceFeed } from './useTraceFeed.js'

let mockPlatformConnection

function mountFeed(options) {
  let feed
  const Host = defineComponent({
    setup() {
      feed = useTraceFeed(options)
      return () => null
    },
  })
  const wrapper = mount(Host)
  return { wrapper, feed: () => feed }
}

describe('useTraceFeed', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPlatformConnection = { connected: ref(false), subscribe: vi.fn() }
  })

  it('bootstraps traces from the trace-request-reply KV bucket on mount', async () => {
    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', value: { traceId: 't1', spans: [{ spanId: 'a1' }] } },
      { op: 'PUT', value: { traceId: 't2', spans: [{ spanId: 'b1' }] } },
      { op: 'DEL', value: { traceId: 't3', spans: [] } }, // ignored — not a PUT
    ])
    const { feed } = mountFeed()
    await flushPromises()

    expect(getKvBucketEntries).toHaveBeenCalledWith('platform', 'trace-request-reply')
    expect(feed().traces.value.size).toBe(2)
    expect(feed().traces.value.get('t1')).toEqual([{ spanId: 'a1' }])
  })

  it('subscribes live once connected, and upserts on a matching notify', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    expect(mockPlatformConnection.subscribe).toHaveBeenCalledWith(
      'notify._platform.kv.trace-request-reply.>',
      expect.any(Function),
    )
    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback({ traceId: 't1', spans: [{ spanId: 'a1' }] }, 'notify._platform.kv.trace-request-reply.trace.t1.changed')

    expect(feed().traces.value.get('t1')).toEqual([{ spanId: 'a1' }])
  })

  it('reconnects when connected flips from false to true after mount', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mountFeed()
    await flushPromises()
    expect(mockPlatformConnection.subscribe).not.toHaveBeenCalled()

    mockPlatformConnection.connected.value = true
    await flushPromises()
    expect(mockPlatformConnection.subscribe).toHaveBeenCalledTimes(1)
  })

  it('drops a live notify with no traceId or no spans array, without upserting', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback({ spans: [{ spanId: 'a1' }] }, 'notify._platform.kv.trace-request-reply.trace.t1.changed') // no traceId
    callback({ traceId: 't2' }, 'notify._platform.kv.trace-request-reply.trace.t2.changed') // no spans

    expect(feed().traces.value.size).toBe(0)
  })

  it('calls onUpsert for every trace upserted, from bootstrap and from live updates alike', async () => {
    getKvBucketEntries.mockResolvedValue([{ op: 'PUT', value: { traceId: 't1', spans: [{ spanId: 'a1' }] } }])
    mockPlatformConnection.connected.value = true
    const onUpsert = vi.fn()
    mountFeed({ onUpsert })
    await flushPromises()

    expect(onUpsert).toHaveBeenCalledWith('t1', [{ spanId: 'a1' }])

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback({ traceId: 't2', spans: [{ spanId: 'b1' }] }, 'notify._platform.kv.trace-request-reply.trace.t2.changed')
    expect(onUpsert).toHaveBeenCalledWith('t2', [{ spanId: 'b1' }])
  })

  it('sets bootstrapFailed when the initial KV read throws', async () => {
    getKvBucketEntries.mockRejectedValue(new Error('boom'))
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().bootstrapFailed.value).toBe(true)
    expect(feed().everDisconnected.value).toBe(false)
  })

  it('leaves bootstrapFailed false when the initial KV read succeeds', async () => {
    getKvBucketEntries.mockResolvedValue([])
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().bootstrapFailed.value).toBe(false)
  })

  it('sets everDisconnected once the live feed drops after having connected', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()
    expect(feed().everDisconnected.value).toBe(false)

    mockPlatformConnection.connected.value = false
    await flushPromises()

    expect(feed().everDisconnected.value).toBe(true)
  })
})
