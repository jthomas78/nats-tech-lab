import { createPinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PulsePanel from './PulsePanel.vue'

// Phase 44 — ported from TraceWaterfall.spec.js's Phase 28p pulse-strip
// spec, with the toolbar-filter-narrowing assertions dropped: Pulse has no
// toolbar of its own and aggregates the full unfiltered trace set by design
// (see PulsePanel.vue's own doc comment for why it doesn't filter).
//
// Live-update behavior (notify._platform.kv.trace-request-reply.>) isn't
// exercised here, same as TraceWaterfall.spec.js — usePlatformConnection is
// mocked inert, so this covers the bootstrap-derived rendering only.

vi.mock('../api', () => ({
  getKvBucketEntries: vi.fn(),
}))
vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ connected: ref(false), subscribe: vi.fn() }),
}))

import { getKvBucketEntries } from '../api'

const BASE = 1755000000000

// Same trace shapes as TraceWaterfall.spec.js's t1/t2/t5 fixtures.
const ROOT = {
  traceId: 't1',
  spanId: 'a1',
  service: 'shipping',
  subject: 'api.acme.shipping.ship.arrive.v1',
  statusCode: 'OK',
  durationMs: 41,
  timestamp: new Date(BASE + 41).toISOString(),
}
const SYNC_CHILD = {
  traceId: 't1',
  spanId: 'a2',
  parentSpanId: 'a1',
  service: 'refdata',
  subject: 'rpc._platform.refdata.item.get.v1',
  statusCode: 'OK',
  durationMs: 24,
  timestamp: new Date(BASE + 30).toISOString(),
}
const ASYNC_TAIL = {
  traceId: 't1',
  spanId: 'a3',
  parentSpanId: 'a1',
  service: 'shipping',
  subject: 'evt.acme.shipping.ship.MV-AURELIA.arrived',
  statusCode: 'OK',
  durationMs: 3,
  timestamp: new Date(BASE + 54).toISOString(),
}
const FAILED_ROOT = {
  traceId: 't2',
  spanId: 'd1',
  service: 'shipping',
  subject: 'api.acme.shipping.container.register.v1',
  statusCode: 'ERROR',
  error: 'container type not found: REEFER-45',
  durationMs: 10,
  timestamp: new Date(BASE - 5000 + 10).toISOString(),
}
const HTTP_ROOT_SIMPLE = {
  traceId: 't5',
  spanId: 'e1',
  service: 'shipping',
  subject: '/api/refdata/types/ship-status',
  statusCode: 'OK',
  durationMs: 12,
  timestamp: new Date(BASE + 100).toISOString(),
}

function kvEntry(traceId, spans) {
  return { key: `_platform.trace.${traceId}`, op: 'PUT', revision: 1, value: { traceId, spans } }
}

function mountPanel() {
  return mount(PulsePanel, { global: { plugins: [createPinia()] } })
}

describe('PulsePanel (Phase 44)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('summarizes the full unfiltered trace set into request/error/latency histograms', async () => {
    getKvBucketEntries.mockResolvedValue([kvEntry('t1', [ROOT, SYNC_CHILD, ASYNC_TAIL]), kvEntry('t2', [FAILED_ROOT]), kvEntry('t5', [HTTP_ROOT_SIMPLE])])
    const wrapper = mountPanel()
    await flushPromises()

    const pulseValues = () => wrapper.findAll('.pulse-card').map((c) => c.find('.pulse-value').text())

    // t1 (ok, replyMs 41), t2 (error, replyMs 10), t5 (ok, replyMs 12) — 1 of
    // 3 traces failed, and the newest trace by its own `at` (t5) sets
    // "current" latency, not simply the last array entry.
    expect(pulseValues()).toEqual(['3', '1', '21'])
    expect(wrapper.find('.pulse-card:nth-child(2) .pulse-window').text()).toBe('33.3% of window')
    expect(wrapper.find('.pulse-card:nth-child(3) .pulse-window').text()).toBe('12ms now')
    expect(wrapper.findAll('.pulse-bar')).toHaveLength(40) // 20 request buckets + 20 error buckets
    expect(wrapper.find('.pulse-line').exists()).toBe(true)
  })

  it('renders the what-it-covers card and the animated flow diagram regardless of whether any traces exist yet', async () => {
    getKvBucketEntries.mockResolvedValue([])
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain('What request/reply covers')
    expect(wrapper.text()).toContain('parentSpanId')
    expect(wrapper.find('.flow-card svg').exists()).toBe(true)

    // No traces at all yet — the stat row hides entirely rather than
    // rendering a zero-width, zero-everything row (same rule the strip it
    // replaced followed).
    expect(wrapper.find('.pulse-row').exists()).toBe(false)
  })
})
