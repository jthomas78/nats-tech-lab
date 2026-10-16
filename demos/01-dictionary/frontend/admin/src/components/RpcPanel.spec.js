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
// Phase 54c (BR-076) — the spans come from the trace query gateway. The
// connection is mocked with a request() that answers trace.search.v1 and
// trace.get.v1 the way observability-service does, so these specs run the
// real gateway adapter (../nats/traceGateway.js) end to end. The polling
// itself is pinned in traceGateway.spec.js.

const conn = vi.hoisted(() => ({ request: null, subscribe: null, connected: null, epoch: null, traces: {} }))
vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => conn,
}))
vi.mock('../api', () => ({ getKvBucketEntries: vi.fn() }))

import { TRACE_GET_SUBJECT, TRACE_SEARCH_SUBJECT } from '../nats/traceGateway.js'

const BASE_NS = 1755000000000 * 1e6

// lab.trace.v1 spans: the same two calls the view-shape fixtures used to be.
const OK_SPAN = {
  traceId: 't1',
  spanId: 'a1',
  name: 'api.acme.shipping.ship.arrive.v1',
  kind: 'SERVER',
  service: 'shipping',
  startUnixNano: BASE_NS,
  endUnixNano: BASE_NS + 12e6,
  statusCode: 'UNSET',
  attributes: { 'lab.tenant.originating': 'acme' },
  events: [
    {
      name: 'rpc.reply.body',
      timeUnixNano: BASE_NS + 12e6,
      attributes: { body: '{"ok":true}', 'body.bytes': 42, headers: '{"Nats-Responder":["shipping-service/abc"]}' },
    },
  ],
  links: [],
}
const ERROR_SPAN = {
  traceId: 't2',
  spanId: 'b1',
  name: 'rpc.acme.refdata.item.get.v1',
  kind: 'SERVER',
  service: 'refdata',
  startUnixNano: BASE_NS + 1000e6,
  endUnixNano: BASE_NS + 1004e6,
  statusCode: 'ERROR',
  statusMessage: 'not found',
  attributes: {},
  events: [],
  links: [],
}

function serve(traces) {
  conn.traces = traces
}

function summaryOf(traceId, spans) {
  return { traceId, rootService: spans[0].service, rootName: spans[0].name, startUnixNano: spans[0].startUnixNano, durationMs: 1, spanCount: spans.length, errorCount: 0, services: [] }
}

function resetConn() {
  conn.connected = ref(true)
  conn.epoch = ref(1)
  conn.subscribe = vi.fn()
  conn.request = vi.fn(async (subject, body) => {
    if (subject === TRACE_SEARCH_SUBJECT) {
      return { schema: 'lab.tracesearch.v1', limit: 50, limited: false, traces: Object.entries(conn.traces).map(([id, spans]) => summaryOf(id, spans)) }
    }
    if (subject === TRACE_GET_SUBJECT) return { schema: 'lab.trace.v1', traceId: body.traceId, spans: conn.traces[body.traceId] ?? [] }
    throw new Error('unexpected ' + subject)
  })
}

const searchCount = () => conn.request.mock.calls.filter(([s]) => s === TRACE_SEARCH_SUBJECT).length

async function mountMessagesTab() {
  const pinia = createPinia()
  const wrapper = mount(RpcPanel, { global: { plugins: [PrimeVue, pinia] } })
  // Force the messages tab directly rather than clicking the toggle — the
  // default tab is 'traces' (Phase 54c), and this keeps the spec on the
  // messages tab's own gateway.
  useUiStore(pinia).rpcTab = 'messages'
  await flushPromises()
  return wrapper
}

describe('RpcPanel [messages] tab (Phase 28g retirement, BR-026)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetConn()
    serve({ t1: [OK_SPAN], t2: [ERROR_SPAN] })
  })

  it('renders one row per span, flattened out of the gateway\'s traces', async () => {
    const wrapper = await mountMessagesTab()

    const subjects = wrapper.findAllComponents(SubjectPath).map((c) => c.props('subject'))
    expect(subjects).toContain('api.acme.shipping.ship.arrive.v1')
    expect(subjects).toContain('rpc.acme.refdata.item.get.v1')
  })

  it('orders rows newest first by span time, not by load order (Phase 54c, BR-076)', async () => {
    // A search returns the newest trace first, and one poll loads the whole
    // batch. Putting each loaded span on top reversed it; the old KV feed
    // pushed spans in time order, so arrival order was time order.
    serve({ t2: [ERROR_SPAN], t1: [OK_SPAN] })
    const wrapper = await mountMessagesTab()

    const subjects = wrapper.findAllComponents(SubjectPath).map((c) => c.props('subject'))
    expect(subjects).toEqual(['rpc.acme.refdata.item.get.v1', 'api.acme.shipping.ship.arrive.v1'])
  })

  it('says it is searching, not "waiting for traffic", until the first search answers (Phase 54c)', async () => {
    let answer
    conn.request = vi.fn(() => new Promise((resolve) => { answer = resolve }))
    const wrapper = await mountMessagesTab()

    expect(wrapper.text()).toContain('Searching the trace store')
    expect(wrapper.text()).not.toContain('Waiting for rpc.*/api.* traffic')

    answer({ schema: 'lab.tracesearch.v1', limit: 50, limited: false, traces: [] })
    await flushPromises()
    expect(wrapper.text()).toContain('Waiting for rpc.*/api.* traffic')
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

  it('links a row to its whole trace in the Traces tab (Phase 54c)', async () => {
    const pinia = createPinia()
    const wrapper = mount(RpcPanel, { global: { plugins: [PrimeVue, pinia] } })
    const ui = useUiStore(pinia)
    ui.rpcTab = 'messages'
    await flushPromises()
    const row = wrapper.findAllComponents(SubjectPath).find((c) => c.props('subject') === 'api.acme.shipping.ship.arrive.v1')
    await row.trigger('click')
    await wrapper.find('[data-testid="rpc-open-trace"]').trigger('click')
    expect(ui.rpcTab).toBe('traces')
    await flushPromises()
    // TraceWaterfall took the request and loaded the trace by ID.
    expect(conn.request).toHaveBeenCalledWith(TRACE_GET_SUBJECT, { traceId: 't1' })
    expect(wrapper.find('.tw-wf-tid').text()).toContain('t1')
  })
})

// Phase 28q — toggling [traces]/[messages] used to fully unmount/remount
// TraceWaterfall on every switch, so switching back re-ran its bootstrap on
// an unbounded trace buffer — the reported sluggishness. `<KeepAlive>` keeps
// the lazy first mount while caching the instance.
//
// Phase 54c: a kept-alive tab must also not POLL while hidden. Each tab
// searches once when it is shown and stops when it is left; Pulse never
// searches at all (BR-077).
describe('RpcPanel tab toggling (Phase 28q, Phase 44, Phase 54c)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetConn()
    serve({ t1: [OK_SPAN], t2: [ERROR_SPAN] })
  })

  it('opens on the Traces tab, not on Pulse', () => {
    const pinia = createPinia()
    mount(RpcPanel, { global: { plugins: [PrimeVue, pinia] } })
    expect(useUiStore(pinia).rpcTab).toBe('traces')
  })

  it('keeps Traces mounted across tab round-trips and searches only for the tab that is shown', async () => {
    const pinia = createPinia()
    const wrapper = mount(RpcPanel, { global: { plugins: [PrimeVue, pinia] } })
    await flushPromises()
    const ui = useUiStore(pinia)
    const traceWaterfall = () => wrapper.findComponent({ name: 'TraceWaterfall' })

    // Default [traces]: one search, by TraceWaterfall; [messages] is idle.
    expect(searchCount()).toBe(1)
    const first = traceWaterfall().element

    ui.rpcTab = 'pulse'
    await flushPromises()
    expect(searchCount()).toBe(1) // Pulse reads nothing

    ui.rpcTab = 'messages'
    await flushPromises()
    expect(searchCount()).toBe(2) // the messages tab's own first search

    ui.rpcTab = 'traces'
    await flushPromises()
    expect(searchCount()).toBe(3) // one fresh search on return …
    expect(traceWaterfall().element === first).toBe(true) // … on the same kept-alive instance
  })
})
