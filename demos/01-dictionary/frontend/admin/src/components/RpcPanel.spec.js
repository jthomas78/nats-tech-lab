import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RpcPanel from './RpcPanel.vue'
import SubjectPath from './SubjectPath.vue'
import { useUiStore } from '../stores/ui.js'

// BR-026 (Phase 28g retirement amendment, BUSINESS_RULES-SHIPPING.md) — the
// [messages] tab no longer subscribes obs.rpc.*/obs.api.* (both dead since
// Phase 28a-28e removed every adapter's publishObs call — see
// ARCHITECTURE-COMMUNICATIONS.md § 6's Phase 28g amendment). It now derives
// from the same obs.trace.* data [traces] (TraceWaterfall.vue) reads —
// the trace-request-reply KV bucket — flattened to one row per SPAN instead of one row
// per trace. These specs cover that flattening, family/status filtering
// carried over from the old view, and the single-pane (reply-only) detail
// view: the old two-pane Request | Reply split is gone because a natstrace
// span carries only the reply side (BR-037's one-span-per-call design).
//
// Live-update behavior (notify._platform.kv.trace-request-reply.>) isn't exercised here,
// same as TraceWaterfall.spec.js — usePlatformConnection is mocked inert,
// so these specs cover the bootstrap-derived rendering only.

vi.mock('../api', () => ({
  getKvBucketEntries: vi.fn(),
}))
vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ connected: ref(false), subscribe: vi.fn() }),
}))

import { getKvBucketEntries } from '../api'

const BASE = 1755000000000

const OK_SPAN = {
  traceId: 't1',
  spanId: 'a1',
  service: 'shipping',
  entity: 'ship',
  action: 'arrive',
  subject: 'api.acme.shipping.ship.arrive.v1',
  statusCode: 'OK',
  durationMs: 12,
  payloadBytes: 42,
  headers: { 'Nats-Responder': ['shipping-service/abc'] },
  payload: { ok: true },
  timestamp: new Date(BASE).toISOString(),
}
const ERROR_SPAN = {
  traceId: 't2',
  spanId: 'b1',
  service: 'refdata',
  entity: 'item',
  action: 'get',
  subject: 'rpc.acme.refdata.item.get.v1',
  statusCode: 'ERROR',
  error: 'not found',
  durationMs: 4,
  payloadBytes: 10,
  timestamp: new Date(BASE + 1000).toISOString(),
}

function kvEntry(traceId, spans) {
  return { key: `_platform.trace.${traceId}`, op: 'PUT', revision: 1, value: { traceId, spans } }
}

async function mountMessagesTab() {
  const pinia = createPinia()
  const wrapper = mount(RpcPanel, { global: { plugins: [PrimeVue, pinia] } })
  // Force the messages tab directly rather than clicking the toggle — the
  // default tab is 'traces' (ARCHITECTURE-ADMIN.md § 4.5), and mounting
  // TraceWaterfall too would race its own getKvBucketEntries call against
  // this spec's mock.
  useUiStore(pinia).rpcTab = 'messages'
  await flushPromises()
  return wrapper
}

describe('RpcPanel [messages] tab (Phase 28g retirement, BR-026)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getKvBucketEntries.mockResolvedValue([kvEntry('t1', [OK_SPAN]), kvEntry('t2', [ERROR_SPAN])])
  })

  it('renders one row per span, flattened out of the trace-request-reply KV bucket', async () => {
    const wrapper = await mountMessagesTab()

    const subjects = wrapper.findAllComponents(SubjectPath).map((c) => c.props('subject'))
    expect(subjects).toContain('api.acme.shipping.ship.arrive.v1')
    expect(subjects).toContain('rpc.acme.refdata.item.get.v1')
  })

  it('filters by family (rpc/api), derived from the span subject', async () => {
    const wrapper = await mountMessagesTab()

    const apiChip = wrapper.findAll('.rpc-toolbar .chip').find((b) => b.text() === 'api')
    await apiChip.trigger('click')

    const subjects = wrapper.findAllComponents(SubjectPath).map((c) => c.props('subject'))
    expect(subjects).not.toContain('api.acme.shipping.ship.arrive.v1')
    expect(subjects).toContain('rpc.acme.refdata.item.get.v1')
  })

  it('filters by status (ok/error) — no "pending" state exists for a span', async () => {
    const wrapper = await mountMessagesTab()

    expect(wrapper.findAll('.rpc-toolbar .chip').map((b) => b.text())).not.toContain('pending')

    const okChip = wrapper.findAll('.rpc-toolbar .chip').find((b) => b.text() === 'ok')
    await okChip.trigger('click')

    const subjects = wrapper.findAllComponents(SubjectPath).map((c) => c.props('subject'))
    expect(subjects).not.toContain('api.acme.shipping.ship.arrive.v1')
    expect(subjects).toContain('rpc.acme.refdata.item.get.v1')
  })

  it('opens a single-pane detail view with only the reply payload — no Request pane', async () => {
    const wrapper = await mountMessagesTab()

    const row = wrapper.findAllComponents(SubjectPath).find((c) => c.props('subject') === 'api.acme.shipping.ship.arrive.v1')
    await row.trigger('click')

    expect(wrapper.find('.detail').exists()).toBe(true)
    expect(wrapper.find('.pane-title').exists()).toBe(false) // old two-pane markup is gone
    expect(wrapper.text()).toContain('duration')
    expect(wrapper.text()).toContain('12 ms')
    expect(wrapper.html()).toContain('"ok"')
  })

  it('shows the span error inline for a failed call', async () => {
    const wrapper = await mountMessagesTab()

    const row = wrapper.findAllComponents(SubjectPath).find((c) => c.props('subject') === 'rpc.acme.refdata.item.get.v1')
    await row.trigger('click')

    expect(wrapper.find('.err-banner').text()).toBe('not found')
  })
})

// Phase 28q — toggling [traces]/[messages] used to fully unmount/remount
// TraceWaterfall on every switch (the `v-if="ui.rpcTab === 'traces'"` gate),
// so switching back to [traces] re-ran its onMounted bootstrap: a fresh
// getKvBucketEntries HTTP round-trip plus a full re-subscribe, on an
// unbounded trace buffer that only grows over a session — the reported
// sluggishness. `<KeepAlive>` around that same v-if keeps the lazy first
// mount (still gated by v-if, so a messages-only test run — see
// mountMessagesTab above — never mounts TraceWaterfall) while caching the
// instance instead of destroying it once mounted, so a later switch away
// and back reuses it with no repeat bootstrap call.
describe('RpcPanel [traces] tab toggling (Phase 28q)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getKvBucketEntries.mockResolvedValue([kvEntry('t1', [OK_SPAN]), kvEntry('t2', [ERROR_SPAN])])
  })

  it('keeps TraceWaterfall mounted across a Messages round-trip instead of re-fetching', async () => {
    const pinia = createPinia()
    const wrapper = mount(RpcPanel, { global: { plugins: [PrimeVue, pinia] } })
    await flushPromises()

    const callsAfterInitialMount = getKvBucketEntries.mock.calls.length
    expect(callsAfterInitialMount).toBeGreaterThan(0)

    useUiStore(pinia).rpcTab = 'messages'
    await flushPromises()
    useUiStore(pinia).rpcTab = 'traces'
    await flushPromises()

    expect(getKvBucketEntries.mock.calls.length).toBe(callsAfterInitialMount)
  })
})
