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
    mockPlatformConnection = { connected: ref(false), epoch: ref(0), subscribe: vi.fn() }
  })

  it('bootstraps traces from the trace-request-reply KV bucket on mount', async () => {
    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', value: { traceId: 't1', spans: [{ tenant: 'acme', span: { spanId: 'a1' } }] } },
      { op: 'PUT', value: { traceId: 't2', spans: [{ tenant: 'acme', span: { spanId: 'b1' } }] } },
      { op: 'DEL', value: { traceId: 't3', spans: [] } }, // ignored — not a PUT
    ])
    const { feed } = mountFeed()
    await flushPromises()

    expect(getKvBucketEntries).toHaveBeenCalledWith('platform', 'trace-request-reply')
    expect(feed().traces.value.size).toBe(2)
    expect(feed().traces.value.get('t1')).toEqual([{ spanId: 'a1', attributedTenant: 'acme' }])
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
    callback({ traceId: 't1', spans: [{ tenant: 'acme', span: { spanId: 'a1' } }] }, 'notify._platform.kv.trace-request-reply.trace.t1.changed')

    expect(feed().traces.value.get('t1')).toEqual([{ spanId: 'a1', attributedTenant: 'acme' }])
  })

  it('reconnects when connected flips from false to true after mount', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mountFeed()
    await flushPromises()
    expect(mockPlatformConnection.subscribe).not.toHaveBeenCalled()

    mockPlatformConnection.connected.value = true
    mockPlatformConnection.epoch.value += 1
    await flushPromises()
    expect(mockPlatformConnection.subscribe).toHaveBeenCalledTimes(1)
  })

  it('drops a live notify with no traceId or no spans array, without upserting', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback({ spans: [{ tenant: 'acme', span: { spanId: 'a1' } }] }, 'notify._platform.kv.trace-request-reply.trace.t1.changed') // no traceId
    callback({ traceId: 't2' }, 'notify._platform.kv.trace-request-reply.trace.t2.changed') // no spans

    expect(feed().traces.value.size).toBe(0)
  })

  it('calls onUpsert for every trace upserted, from bootstrap and from live updates alike', async () => {
    getKvBucketEntries.mockResolvedValue([{ op: 'PUT', value: { traceId: 't1', spans: [{ tenant: 'acme', span: { spanId: 'a1' } }] } }])
    mockPlatformConnection.connected.value = true
    const onUpsert = vi.fn()
    mountFeed({ onUpsert })
    await flushPromises()

    expect(onUpsert).toHaveBeenCalledWith('t1', [{ spanId: 'a1', attributedTenant: 'acme' }])

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback({ traceId: 't2', spans: [{ tenant: 'acme', span: { spanId: 'b1' } }] }, 'notify._platform.kv.trace-request-reply.trace.t2.changed')
    expect(onUpsert).toHaveBeenCalledWith('t2', [{ spanId: 'b1', attributedTenant: 'acme' }])
  })

  it('sets bootstrapFailed when the snapshot read throws', async () => {
    getKvBucketEntries.mockRejectedValue(new Error('boom'))
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().bootstrapFailed.value).toBe(true)
  })

  // A snapshot read that fails on the freshly-reconnected socket is usually a
  // race with NATS still coming up, so it must not wait for the *next*
  // reconnect to try again.
  it('retries a failed snapshot read on its own, without waiting for another reconnect', async () => {
    vi.useFakeTimers()
    try {
      getKvBucketEntries.mockRejectedValueOnce(new Error('boom')).mockResolvedValue([])
      const { feed } = mountFeed()
      await flushPromises()
      expect(feed().bootstrapFailed.value).toBe(true)

      await vi.advanceTimersByTimeAsync(600)
      await flushPromises()

      expect(getKvBucketEntries).toHaveBeenCalledTimes(2)
      expect(feed().bootstrapFailed.value).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('leaves bootstrapFailed false when the initial KV read succeeds', async () => {
    getKvBucketEntries.mockResolvedValue([])
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().bootstrapFailed.value).toBe(false)
  })

  it('clears bootstrapFailed once a later snapshot read succeeds', async () => {
    getKvBucketEntries.mockRejectedValueOnce(new Error('boom')).mockResolvedValue([])
    mockPlatformConnection.connected.value = false
    const { feed } = mountFeed()
    await flushPromises()
    expect(feed().bootstrapFailed.value).toBe(true)

    mockPlatformConnection.connected.value = true
    mockPlatformConnection.epoch.value += 1
    await flushPromises()

    expect(feed().bootstrapFailed.value).toBe(false)
  })

  // The whole point of the fix this spec guards: notify.* is core NATS with no
  // replay, so a span published while the socket was down is never redelivered
  // — but it IS in the durable KV bucket, so re-reading the snapshot on
  // reconnect recovers it. Without the re-read the trace is missing until the
  // page is reloaded, which is what the old "some spans may be missing" banner
  // was reporting.
  it('re-reads the KV snapshot on reconnect, recovering a trace missed while offline', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()
    expect(feed().traces.value.size).toBe(0)

    // socket drops; a trace lands in KV that this client never sees on the wire
    mockPlatformConnection.connected.value = false
    await flushPromises()
    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', value: { traceId: 'offline1', spans: [{ tenant: 'acme', span: { spanId: 'x1' } }] } },
    ])

    mockPlatformConnection.connected.value = true
    mockPlatformConnection.epoch.value += 1
    await flushPromises()

    expect(getKvBucketEntries).toHaveBeenCalledTimes(2)
    expect(feed().traces.value.get('offline1')).toEqual([{ spanId: 'x1', attributedTenant: 'acme' }])
  })

  // The regression this whole change exists for: nats-core absorbs a NATS
  // restart internally, so `connected` never flips — only epoch bumps. A feed
  // watching `connected` sleeps through it and keeps a permanent hole.
  it('resyncs on an epoch bump alone, when connected never flips', async () => {
    getKvBucketEntries.mockResolvedValue([])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()
    expect(getKvBucketEntries).toHaveBeenCalledTimes(1)

    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', value: { traceId: 'inner1', spans: [{ tenant: '_platform', span: { spanId: 'i1' } }] } },
    ])
    mockPlatformConnection.epoch.value += 1 // connected stays true throughout
    await flushPromises()

    expect(getKvBucketEntries).toHaveBeenCalledTimes(2)
    expect(feed().traces.value.get('inner1')).toEqual([{ spanId: 'i1', attributedTenant: '_platform' }])
  })

  it('re-subscribes exactly once per reconnect, without leaking the old subscription', async () => {
    getKvBucketEntries.mockResolvedValue([])
    const unsubscribe = vi.fn()
    mockPlatformConnection.subscribe.mockReturnValue(unsubscribe)
    mockPlatformConnection.connected.value = true
    mountFeed()
    await flushPromises()

    mockPlatformConnection.connected.value = false
    await flushPromises()
    mockPlatformConnection.connected.value = true
    mockPlatformConnection.epoch.value += 1
    await flushPromises()

    expect(mockPlatformConnection.subscribe).toHaveBeenCalledTimes(2)
    expect(unsubscribe).toHaveBeenCalledTimes(1)
  })

  // A snapshot read issued before a live notify but resolving after it must not
  // overwrite the newer, longer spans array with its own stale one — otherwise
  // the resync that closes gaps becomes a source of them.
  it('does not let a stale snapshot clobber a longer spans array from the live feed', async () => {
    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', value: { traceId: 't1', spans: [{ tenant: 'acme', span: { spanId: 'a1' } }] } },
    ])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback(
      { traceId: 't1', spans: [{ tenant: 'acme', span: { spanId: 'a1' } }, { tenant: 'acme', span: { spanId: 'a2' } }] },
      'notify._platform.kv.trace-request-reply.trace.t1.changed',
    )
    expect(feed().traces.value.get('t1')).toHaveLength(2)

    // a reconnect re-reads the same one-span snapshot
    mockPlatformConnection.connected.value = false
    await flushPromises()
    mockPlatformConnection.connected.value = true
    mockPlatformConnection.epoch.value += 1
    await flushPromises()

    expect(feed().traces.value.get('t1')).toHaveLength(2)
  })
})

// Phase 48c — the composable is where the KV record's per-span {tenant, span}
// wrapper is flattened, so this is where the flattening contract is pinned.
describe('useTraceFeed span attribution (Phase 48c, BR-051)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('flattens the per-span wrapper onto each span, keeping two accounts in one trace distinct', async () => {
    getKvBucketEntries.mockResolvedValue([
      {
        op: 'PUT',
        value: {
          traceId: 't1',
          spans: [
            { tenant: 'acme', span: { spanId: 'a1' } },
            { tenant: '_platform', span: { spanId: 'a2' } },
          ],
        },
      },
    ])
    const { feed } = mountFeed()
    await flushPromises()

    // The cross-account trace, which is the ordinary shape here: a tenant's
    // service calling a PLATFORM one. Collapsing this to one value per trace
    // is what Phase 48c had to undo.
    expect(feed().traces.value.get('t1')).toEqual([
      { spanId: 'a1', attributedTenant: 'acme' },
      { spanId: 'a2', attributedTenant: '_platform' },
    ])
  })

  it('leaves a pre-48c bare span unattributed instead of inferring one', async () => {
    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', value: { traceId: 't1', spans: [{ spanId: 'a1' }] } },
    ])
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().traces.value.get('t1')).toEqual([{ spanId: 'a1', attributedTenant: '' }])
  })
})

// ── Phase 48g (BR-053) ───────────────────────────────────────────────────
// One KV entry is one span now, keyed trace.{traceId}.{spanId}, and the join
// moved here. Every spec ABOVE still uses the pre-48g merged {traceId, spans}
// shape on purpose — that is the back-compat window, since a record written
// before the projector was deployed sits in the bucket for one BucketMaxAge
// and the panel must not go blank for 15 minutes. These cover the shape the
// projector actually writes today.
describe('useTraceFeed per-span records (Phase 48g, BR-053)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPlatformConnection = { connected: ref(false), epoch: ref(0), subscribe: vi.fn() }
  })

  const perSpan = (traceId, spanId, tenant) => ({
    op: 'PUT',
    key: `_platform.trace.${traceId}.${spanId}`,
    value: { tenant, span: { traceId, spanId } },
  })
  const notifySubject = (traceId, spanId) =>
    `notify._platform.kv.trace-request-reply.trace.${traceId}.${spanId}.changed`

  it('joins separate per-span entries of one trace into one spans array', async () => {
    getKvBucketEntries.mockResolvedValue([
      perSpan('t1', 'a1', 'acme'),
      perSpan('t1', 'a2', '_platform'),
      perSpan('t2', 'b1', 'acme'),
    ])
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().traces.value.size).toBe(2)
    expect(feed().traces.value.get('t1')).toEqual([
      { traceId: 't1', spanId: 'a1', attributedTenant: 'acme' },
      { traceId: 't1', spanId: 'a2', attributedTenant: '_platform' },
    ])
    expect(feed().traces.value.get('t2')).toHaveLength(1)
  })

  it('keeps each span its own attribution across the join', async () => {
    // The ordinary cross-account trace: an acme root and a PLATFORM handler.
    // The join must not let either span's tenant win for the whole trace.
    getKvBucketEntries.mockResolvedValue([
      perSpan('t1', 'root', 'acme'),
      perSpan('t1', 'handler', '_platform'),
    ])
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().traces.value.get('t1').map((s) => s.attributedTenant)).toEqual([
      'acme',
      '_platform',
    ])
  })

  it('fires onUpsert once per trace with its whole span list, not once per span', async () => {
    getKvBucketEntries.mockResolvedValue([perSpan('t1', 'a1', 'acme'), perSpan('t1', 'a2', 'acme')])
    const onUpsert = vi.fn()
    mountFeed({ onUpsert })
    await flushPromises()

    expect(onUpsert).toHaveBeenCalledTimes(1)
    expect(onUpsert.mock.calls[0][1]).toHaveLength(2)
  })

  it('merges a live per-span notify into the spans already held', async () => {
    getKvBucketEntries.mockResolvedValue([perSpan('t1', 'a1', 'acme')])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback({ tenant: '_platform', span: { traceId: 't1', spanId: 'a2' } }, notifySubject('t1', 'a2'))

    // The arriving span is ADDED, not substituted for the trace — the same
    // lost-span failure this rewrite removed from the write side, which the
    // read side would otherwise reintroduce.
    expect(feed().traces.value.get('t1')).toHaveLength(2)
  })

  it('is idempotent: the same span seen twice does not grow the trace', async () => {
    getKvBucketEntries.mockResolvedValue([perSpan('t1', 'a1', 'acme')])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    const notify = () =>
      callback(
        { tenant: 'acme', span: { traceId: 't1', spanId: 'a1', statusCode: 'OK' } },
        notifySubject('t1', 'a1'),
      )
    notify()
    notify()

    const spans = feed().traces.value.get('t1')
    expect(spans).toHaveLength(1)
    // Overwritten in place, so a redelivery carrying a fuller span wins rather
    // than being ignored.
    expect(spans[0].statusCode).toBe('OK')
  })

  it('lets a re-read after a live update add nothing rather than roll it back', async () => {
    // The race the old span-count guard existed for: a snapshot issued before
    // a live notify but resolving after it. With per-span merging the stale
    // read is a no-op, so no guard is needed to make it safe.
    getKvBucketEntries.mockResolvedValue([perSpan('t1', 'a1', 'acme')])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    const [, callback] = mockPlatformConnection.subscribe.mock.calls[0]
    callback({ tenant: 'acme', span: { traceId: 't1', spanId: 'a2' } }, notifySubject('t1', 'a2'))
    expect(feed().traces.value.get('t1')).toHaveLength(2)

    mockPlatformConnection.epoch.value += 1
    await flushPromises()
    expect(feed().traces.value.get('t1')).toHaveLength(2)
  })

  // A span with no spanId is refused at BOTH gates, which are reached by
  // different shapes: a per-span record without one is not recognisable as a
  // per-span record at all, while a legacy merged record is recognised by its
  // envelope and carries its bad span through to the merge.
  it('does not recognise a per-span record with no spanId', async () => {
    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', key: '_platform.trace.t1.a1', value: { tenant: 'acme', span: { traceId: 't1' } } },
    ])
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().traces.value.size).toBe(0)
  })

  it('drops a spanId-less span out of a legacy record rather than re-adding it each read', async () => {
    getKvBucketEntries.mockResolvedValue([
      {
        op: 'PUT',
        value: {
          traceId: 't1',
          spans: [{ tenant: 'acme', span: {} }, { tenant: 'acme', span: { spanId: 'a1' } }],
        },
      },
    ])
    mockPlatformConnection.connected.value = true
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().traces.value.get('t1')).toHaveLength(1)

    // Re-read: an undroppable span would grow the trace on every resync.
    mockPlatformConnection.epoch.value += 1
    await flushPromises()
    expect(feed().traces.value.get('t1')).toHaveLength(1)
  })

  it('reads a pre-48g merged record and a post-48g per-span one into the same trace', async () => {
    // Exactly what the bucket holds for the first BucketMaxAge after deploy.
    getKvBucketEntries.mockResolvedValue([
      { op: 'PUT', value: { traceId: 't1', spans: [{ tenant: 'acme', span: { spanId: 'old' } }] } },
      perSpan('t1', 'new', '_platform'),
    ])
    const { feed } = mountFeed()
    await flushPromises()

    expect(feed().traces.value.get('t1').map((s) => s.spanId)).toEqual(['old', 'new'])
  })
})
