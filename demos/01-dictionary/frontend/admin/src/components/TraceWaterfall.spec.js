import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import SubjectPath from './SubjectPath.vue'
import TraceWaterfall from './TraceWaterfall.vue'

// BR-035 (Phase 28g) — the trace waterfall panel renders one row per trace,
// not per span; the reply-ack line appears only for a trace with an async
// tail; the account gutter marks a crossing only where consecutive spans in
// the parent/child chain carry different account labels; and the header
// shows both durations, with read-model-consistent always >= reply latency.
//
// Live-update behavior (the notify._platform.kv.trace-request-reply.> subscription) isn't
// exercised here — usePlatformConnection is mocked inert (never connected),
// so these specs cover the bootstrap-derived rendering only, which is where
// BR-035's required assertions live.

vi.mock('../api', () => ({
  getKvBucketEntries: vi.fn(),
}))
vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ connected: ref(false), subscribe: vi.fn() }),
}))

import { getKvBucketEntries } from '../api'

const BASE = 1755000000000

// Trace t1: root (shipping) -> sync child (refdata, crosses TENANT->PLATFORM)
// -> async tail (shipping, finishes after the reply — the ack line).
const ROOT = {
  traceId: 't1',
  spanId: 'a1',
  service: 'shipping',
  entity: 'ship',
  action: 'arrive',
  subject: 'api.acme.shipping.ship.arrive.v1',
  statusCode: 'OK',
  durationMs: 41,
  timestamp: new Date(BASE + 41).toISOString(),
  // Phase 28h — requestPayload closes the "Request body — not captured yet"
  // gap; payload (the reply body) already existed pre-28h.
  requestPayload: { shipId: 'MV-AURELIA' },
  payload: { status: 'arrived' },
}
const SYNC_CHILD = {
  traceId: 't1',
  spanId: 'a2',
  parentSpanId: 'a1',
  service: 'refdata',
  entity: 'item',
  action: 'get',
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
  entity: 'ship',
  action: 'projected',
  subject: 'evt.acme.shipping.ship.MV-AURELIA.arrived',
  statusCode: 'OK',
  durationMs: 3,
  timestamp: new Date(BASE + 54).toISOString(),
}

// Trace t2: a single failed root span, no children — no async tail, so no
// ack line should render for it.
const FAILED_ROOT = {
  traceId: 't2',
  spanId: 'd1',
  service: 'shipping',
  entity: 'container',
  action: 'register',
  subject: 'api.acme.shipping.container.register.v1',
  statusCode: 'ERROR',
  error: 'container type not found: REEFER-45',
  durationMs: 10,
  timestamp: new Date(BASE - 5000 + 10).toISOString(),
}

// Trace t3 (Phase 28k) — a real production pair: an outbound rpc.* call
// whose parent and child finish within the same MILLISECOND but a fraction
// of a millisecond apart (caller finishes ~0.65ms after the callee, since
// it also waits out the network round trip) — reproduces a real captured
// trace where `new Date(...).getTime()`'s millisecond truncation collapsed
// both spans' computed start times to the same value, and the waterfall's
// stable sort then fell back to array order, rendering the CHILD above its
// own PARENT.
const SUBMS_PARENT = {
  traceId: 't3',
  spanId: 'p1',
  service: 'refdata',
  entity: 'locales',
  action: 'list',
  subject: 'refdata.locales.list.v1',
  statusCode: 'OK',
  durationMs: 2,
  timestamp: '2026-01-01T00:00:00.266084722Z',
}
const SUBMS_CHILD = {
  traceId: 't3',
  spanId: 'p2',
  parentSpanId: 'p1',
  service: 'refdata',
  entity: 'locales',
  action: 'list',
  subject: 'rpc.acme.refdata.locales.list.v1',
  statusCode: 'OK',
  durationMs: 1,
  timestamp: '2026-01-01T00:00:00.265438763Z',
}

// Trace t4 (Phase 28m) — a real captured HTTP-rooted trace: a browser GET
// wraps an outbound rpc.* call which itself wraps the server-side reply, all
// three finishing close enough together that `durationMs` (whole-millisecond
// -truncated server-side, Go's `time.Duration.Milliseconds()`) rounds the
// HTTP root and its direct child to the SAME 66ms even though the root's
// true duration is a fraction of a millisecond longer — `ownStart` (finish
// minus that truncated duration) then estimates the ROOT's start as later
// than its own CHILD's, and a flat sort-by-offset rendered the child above
// its parent and the parent above its own grandchild's parent. Fixed by
// walking the known parentSpanId tree instead of trusting offset alone.
const HTTP_ROOT = {
  traceId: 't4',
  spanId: 'b3af02c596a5b479',
  service: 'shipping',
  entity: 'refdata',
  action: 'get',
  subject: '/api/refdata/types/string',
  statusCode: 'OK',
  durationMs: 66,
  timestamp: '2026-08-15T19:53:32.958219254Z',
}
const HTTP_OUTBOUND_CHILD = {
  traceId: 't4',
  spanId: 'c84f8571aa041f0f',
  parentSpanId: 'b3af02c596a5b479',
  service: 'refdata',
  entity: 'type',
  action: 'list',
  subject: 'refdata.type.list.v1',
  statusCode: 'OK',
  durationMs: 66,
  timestamp: '2026-08-15T19:53:32.95761067Z',
}
const HTTP_INBOUND_GRANDCHILD = {
  traceId: 't4',
  spanId: 'cda3de94736d7b58',
  parentSpanId: 'c84f8571aa041f0f',
  service: 'refdata',
  entity: 'type',
  action: 'list',
  subject: 'rpc.acme.refdata.type.list.v1',
  statusCode: 'OK',
  durationMs: 29,
  timestamp: '2026-08-15T19:53:32.956237462Z',
}

// Trace t5 (Phase 28o) — an HTTP-rooted trace, for the rest/nats kind
// marker + filter. Its root subject is a URL path, same shape
// httpTraceMiddleware publishes (trace_middleware.go's httpEntity), which is
// exactly what traceKind reads to classify a trace as "rest".
const HTTP_ROOT_SIMPLE = {
  traceId: 't5',
  spanId: 'e1',
  service: 'shipping',
  entity: 'refdata',
  action: 'get',
  subject: '/api/refdata/types/ship-status',
  statusCode: 'OK',
  durationMs: 12,
  timestamp: new Date(BASE + 100).toISOString(),
}

function kvEntry(traceId, spans) {
  return { key: `_platform.trace.${traceId}`, op: 'PUT', revision: 1, value: { traceId, spans } }
}

function mountPanel() {
  return mount(TraceWaterfall, { global: { plugins: [PrimeVue, createPinia()] } })
}

describe('TraceWaterfall (Phase 28g, BR-035)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getKvBucketEntries.mockResolvedValue([
      kvEntry('t1', [ROOT, SYNC_CHILD, ASYNC_TAIL]),
      kvEntry('t2', [FAILED_ROOT]),
    ])
  })

  it('renders one row per traceId, not one per span', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.tw-trace')).toHaveLength(2)
  })

  it('shows the reply-ack line only for a trace with an async tail, and none for one without', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // t1 sorts first (newest — at = BASE), selected by default.
    expect(wrapper.find('.tw-wf-tid').text()).toContain('t1')
    expect(wrapper.find('.tw-ack').exists()).toBe(true)

    await wrapper.findAll('.tw-trace')[1].trigger('click')
    await flushPromises()

    expect(wrapper.find('.tw-wf-tid').text()).toContain('t2')
    expect(wrapper.find('.tw-ack').exists()).toBe(false)
  })

  it('marks a crossing only where a span and its parent carry different account labels', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('.tw-row')
    expect(rows).toHaveLength(3)

    const acctCells = rows.map((r) => r.find('.tw-acct'))
    // Root (a1, shipping/TENANT, no parent) — never a crossing.
    expect(acctCells[0].classes()).not.toContain('cross')
    // Sync child (a2, refdata/PLATFORM, parent a1/TENANT) — crosses.
    expect(acctCells[1].classes()).toContain('cross')
    expect(acctCells[1].text()).toContain('⇥')
    // Async tail (a3, shipping/TENANT, parent a1/TENANT) — same account, no crossing.
    expect(acctCells[2].classes()).not.toContain('cross')
  })

  it('renders both durations in the header, with read-model-consistent always >= reply latency', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const stats = wrapper.findAll('.tw-stat')
    const reply = stats[0]
    const consistent = stats[1]
    expect(reply.find('.v').text()).toBe('41ms')
    expect(consistent.find('.v').text()).toContain('54ms')

    // Structural invariant BR-035 requires: the read-model-consistent
    // timestamp can never be earlier than the reply itself.
    const replyMs = Number(reply.find('.v').text().replace('ms', ''))
    const consistentMs = Number(consistent.find('.v').text().match(/(\d+)ms/)[1])
    expect(consistentMs).toBeGreaterThanOrEqual(replyMs)
  })

  it('shows an empty state and no ack line for a trace with no async tail', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.findAll('.tw-trace')[1].trigger('click')
    await flushPromises()

    const consistent = wrapper.findAll('.tw-stat')[1]
    expect(consistent.text()).toContain('never — no async tail')
  })

  it('labels a missing responder by why it is missing, not as "not yet finished" (Phase 28r) — an evt.* consumer span never had one to record', async () => {
    // Every span in the trace store already finished (natstrace.go's finish()
    // is the only obs.trace.* publish point, called exclusively from End/Fail)
    // — a missing Nats-Responder header is never an in-flight state. ASYNC_TAIL
    // (a3, evt.acme.shipping.ship.MV-AURELIA.arrived) is a JetStream consumer
    // reacting to a message, not answering a request, so it carries no
    // Nats-Responder header by design.
    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('.tw-row')
    await rows[2].trigger('click')
    await flushPromises()

    const identities = wrapper.findAll('.tw-who-id')
    expect(identities[1].text()).toBe('async event — no NATS responder')
  })

  it('labels a missing responder on a failed call as "no reply received", not "not yet finished" (Phase 28r)', async () => {
    // t2 (FAILED_ROOT) finished via Fail with no reply ever received — a real,
    // permanent outcome, not an incomplete one.
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.findAll('.tw-trace')[1].trigger('click')
    await flushPromises()

    const identities = wrapper.findAll('.tw-who-id')
    expect(identities[1].text()).toBe('call failed — no reply received')
  })

  it('renders a request body and a response body in the same request|response split as identity/headers (Phase 28i)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    // selectTrace (which also seeds selectedSpanId to the root span) only
    // runs on a trace-tile click, not on the initial bootstrap selection.
    await wrapper.findAll('.tw-trace')[0].trigger('click')
    await flushPromises()

    // One continuous grid (BR-036 Phase 28i), not a separate full-width
    // section per body — "body" is the section label now that the column
    // itself (identity/headers/body all under one REQUEST/RESPONSE caption)
    // carries the direction, so it no longer needs a "Request "/"Response "
    // prefix repeated at every level.
    const cells = wrapper.findAll('.tw-rr-cell')
    const bodyCells = cells.filter((c) => c.find('.json').exists())
    expect(bodyCells).toHaveLength(2)
    expect(bodyCells[0].find('.sect-label').text()).toBe('body')
    expect(bodyCells[0].text()).toContain('MV-AURELIA')
    expect(bodyCells[1].find('.sect-label').text()).toBe('body')
    expect(bodyCells[1].text()).toContain('arrived')
  })

  it('orders a parent above its child even when their finish timestamps round to the same millisecond (Phase 28k)', async () => {
    // Overrides the shared beforeEach mock with just this one trace — t1/t2
    // sort by `at` (traceStart), and t3's 2026 timestamp would otherwise
    // jump ahead of them and break every other test's `.tw-trace[0]` == t1
    // assumption.
    getKvBucketEntries.mockResolvedValue([kvEntry('t3', [SUBMS_PARENT, SUBMS_CHILD])])
    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('.tw-row')
    expect(rows).toHaveLength(2)
    const subjects = rows.map((r) => r.findComponent(SubjectPath).props('subject'))
    expect(subjects).toEqual(['refdata.locales.list.v1', 'rpc.acme.refdata.locales.list.v1'])

    // The parent (root, no parentSpanId) renders with no indent rail; its
    // child renders with exactly one.
    expect(rows[0].findAll('.tw-rail')).toHaveLength(0)
    expect(rows[1].findAll('.tw-rail')).toHaveLength(1)
  })

  it('walks the parentSpanId tree instead of flat-sorting by offset, so a grandparent HTTP span renders above its child and grandchild even when truncated durationMs ties their estimated start times (Phase 28m)', async () => {
    getKvBucketEntries.mockResolvedValue([kvEntry('t4', [HTTP_INBOUND_GRANDCHILD, HTTP_OUTBOUND_CHILD, HTTP_ROOT])])
    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('.tw-row')
    expect(rows).toHaveLength(3)
    const subjects = rows.map((r) => r.findComponent(SubjectPath).props('subject'))
    expect(subjects).toEqual(['/api/refdata/types/string', 'refdata.type.list.v1', 'rpc.acme.refdata.type.list.v1'])

    expect(rows[0].findAll('.tw-rail')).toHaveLength(0)
    expect(rows[1].findAll('.tw-rail')).toHaveLength(1)
    expect(rows[2].findAll('.tw-rail')).toHaveLength(2)
  })

  it('resizes the trace rail by dragging the handle, clamped to [240, 640]', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const handle = wrapper.find('.tw-resize-handle')
    const split = wrapper.find('.tw-split')
    const railWidth = () => Number(split.attributes('style').match(/(\d+)px/)[1])

    expect(railWidth()).toBe(420) // default: 50% wider than the original 280px rail

    await handle.trigger('mousedown', { clientX: 420 })
    await window.dispatchEvent(new MouseEvent('mousemove', { clientX: 500 }))
    expect(railWidth()).toBe(500)

    // Clamps at the floor and ceiling rather than tracking the cursor past them.
    await window.dispatchEvent(new MouseEvent('mousemove', { clientX: -1000 }))
    expect(railWidth()).toBe(240)
    await window.dispatchEvent(new MouseEvent('mousemove', { clientX: 5000 }))
    expect(railWidth()).toBe(640)

    await window.dispatchEvent(new MouseEvent('mouseup'))
    expect(handle.classes()).not.toContain('active')
  })

  it('resizes the trace rail with arrow keys once the handle is focused', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const handle = wrapper.find('.tw-resize-handle')
    const split = wrapper.find('.tw-split')
    const railWidth = () => Number(split.attributes('style').match(/(\d+)px/)[1])

    await handle.trigger('keydown', { key: 'ArrowLeft' })
    expect(railWidth()).toBe(400)
    await handle.trigger('keydown', { key: 'ArrowRight' })
    expect(railWidth()).toBe(420)
  })

  it('tags each trace REST or NATS by its root span, and filters the list by transport (Phase 28o)', async () => {
    getKvBucketEntries.mockResolvedValue([kvEntry('t1', [ROOT, SYNC_CHILD, ASYNC_TAIL]), kvEntry('t2', [FAILED_ROOT]), kvEntry('t5', [HTTP_ROOT_SIMPLE])])
    const wrapper = mountPanel()
    await flushPromises()

    const kindOf = () => wrapper.findAll('.tw-trace').map((t) => t.find('.kind-tag').text())
    expect(wrapper.findAll('.tw-trace')).toHaveLength(3)
    // t1/t2 root on api.* (NATS); t5 roots on a URL path (REST).
    expect(kindOf().sort()).toEqual(['nats', 'nats', 'rest'])

    await wrapper.find('[data-k="rest"]').trigger('click')
    expect(wrapper.findAll('.tw-trace')).toHaveLength(1)
    expect(kindOf()).toEqual(['rest'])

    await wrapper.find('[data-k="nats"]').trigger('click')
    expect(wrapper.findAll('.tw-trace')).toHaveLength(2)
    expect(kindOf()).toEqual(['nats', 'nats'])

    await wrapper.find('[data-k="all"]').trigger('click')
    expect(wrapper.findAll('.tw-trace')).toHaveLength(3)
  })

  it('summarizes the currently displayed trace window into request/error/latency histograms, and reshapes with the toolbar filters (Phase 28p)', async () => {
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

    // Narrowing the toolbar's errors filter narrows the strip to the same
    // slice the trace list shows underneath it — one dataset, two views.
    await wrapper.find('.chip.err').trigger('click')
    expect(pulseValues()).toEqual(['1', '1', '10'])
    expect(wrapper.find('.pulse-card:nth-child(2) .pulse-window').text()).toBe('100.0% of window')

    await wrapper.find('.chip.err').trigger('click')

    // A filter combination with no matching traces at all hides the strip
    // entirely rather than rendering a zero-width, zero-everything strip.
    const slowChip = wrapper.findAll('.chip').find((c) => c.text().includes('slow'))
    await slowChip.trigger('click')
    expect(wrapper.find('.pulse-strip').exists()).toBe(false)
  })
})
