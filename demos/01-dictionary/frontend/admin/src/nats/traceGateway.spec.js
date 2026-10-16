// Phase 54c — the Admin UI reads traces from the trace query gateway
// (BR-074, BR-075, BR-076), not from the trace-request-reply KV feed.
//
// Two things are pinned here:
//   - toViewSpan: the lab.trace.v1 span becomes the flat span shape every
//     trace panel already reads (subject, timestamp, durationUs, payload,
//     headers, attributedTenant …), so the panels do not change their reading
//     of a span when the store changes.
//   - useTraceGateway: decision 8's polling. Poll while the page is visible,
//     pause while hidden, keep the last refresh time, offer a manual refresh,
//     and let an open trace fill in as late spans arrive.
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const conn = vi.hoisted(() => ({ request: null, connected: null, epoch: null }))
vi.mock('./usePlatformConnection.js', () => ({
  usePlatformConnection: () => conn,
}))

import {
  POLL_INTERVAL_MS,
  TRACE_GET_SUBJECT,
  TRACE_SEARCH_SUBJECT,
  grafanaTraceUrl,
  toViewSpan,
  useTraceGateway,
} from './traceGateway.js'

const T0 = 1792170166109209968 // ns, a real ship arrive (au-1, 2026-10-16)

function labSpan(over = {}) {
  return {
    traceId: '9ce968fc29b9e77140d2404a6a8834b8',
    spanId: '4c360094f558efc8',
    name: 'api.acme.shipping.ship.arrive.v1',
    kind: 'SERVER',
    service: 'shipping-service',
    startUnixNano: T0,
    endUnixNano: T0 + 20794750,
    statusCode: 'UNSET',
    resource: { 'service.name': 'shipping-service' },
    attributes: { 'lab.tenant.receiving_account': 'ACME', 'lab.tenant.originating': 'acme' },
    events: [],
    links: [],
    ...over,
  }
}

describe('toViewSpan (BR-076): a lab.trace.v1 span in the panels\' shape', () => {
  it('keeps the IDs, service and status, and names the subject from the span name', () => {
    const v = toViewSpan(labSpan({ parentSpanId: 'ff14f018eaf04ab2' }))
    expect(v).toMatchObject({
      traceId: '9ce968fc29b9e77140d2404a6a8834b8',
      spanId: '4c360094f558efc8',
      parentSpanId: 'ff14f018eaf04ab2',
      service: 'shipping-service',
      subject: 'api.acme.shipping.ship.arrive.v1',
      kind: 'SERVER',
      direction: 'inbound',
    })
  })

  it('gives a duration in whole microseconds and a finish timestamp with its sub-millisecond part (BR-056)', () => {
    const v = toViewSpan(labSpan())
    // Whole microseconds, as natstrace published them: a fraction here
    // showed as "30.670848000000003 ms" in the [messages] tab.
    expect(v.durationUs).toBe(20795)
    expect(Number.isInteger(v.durationUs)).toBe(true)
    expect(v.timestamp).toMatch(/^2026-10-16T\d\d:\d\d:\d\d\.\d{9}Z$/)
    // The fraction is read back with the same precision the panels use.
    const ms = Number(v.timestamp.slice(20, 29)) / 1e6
    expect(ms).toBeCloseTo(((T0 + 20794750) % 1e9) / 1e6, 2)
  })

  it('reads the originating tenant as the account, and empty when it is not set (BR-054)', () => {
    expect(toViewSpan(labSpan()).attributedTenant).toBe('acme')
    expect(toViewSpan(labSpan({ attributes: { 'lab.tenant.receiving_account': 'PLATFORM' } })).attributedTenant).toBe('')
  })

  it('maps ERROR to the error text from the status message', () => {
    const v = toViewSpan(labSpan({ statusCode: 'ERROR', statusMessage: 'NO_SUCH_VEHICLE_TYPE' }))
    expect(v.statusCode).toBe('ERROR')
    expect(v.error).toBe('NO_SUCH_VEHICLE_TYPE')
    expect(toViewSpan(labSpan()).error).toBeUndefined()
  })

  it('reads the request and reply bodies from their span events, with headers, size, redaction and truncation (BR-036)', () => {
    const v = toViewSpan(labSpan({
      events: [
        {
          name: 'rpc.request.body',
          timeUnixNano: T0,
          attributes: {
            body: '{"shipID":"STK-SHIP-01","port":"DURBAN"}',
            'body.bytes': 41,
            headers: '{"Nats-Requestor":["admin-app"],"traceparent":["00-x-y-01"]}',
            redacted: ['authorization'],
          },
        },
        {
          name: 'rpc.reply.body',
          timeUnixNano: T0 + 20000000,
          attributes: { body: '{"status":"arri', 'body.bytes': 9000, truncated: true, headers: '{"Nats-Responder":["shipping-service"]}' },
        },
      ],
    }))
    expect(v.requestPayload).toEqual({ shipID: 'STK-SHIP-01', port: 'DURBAN' })
    expect(v.requestPayloadBytes).toBe(41)
    expect(v.requestRedacted).toEqual(['authorization'])
    expect(v.requestTruncated).toBe(false)
    // A cut body is not JSON any more; it is kept as text, never dropped.
    expect(v.payload).toBe('{"status":"arri')
    expect(v.payloadBytes).toBe(9000)
    expect(v.truncated).toBe(true)
    expect(v.headers).toEqual({
      'Nats-Requestor': ['admin-app'],
      traceparent: ['00-x-y-01'],
      'Nats-Responder': ['shipping-service'],
    })
    expect(v.requester).toBe('admin-app')
  })

  it('names a REST span by its path, so the panels still tag it REST', () => {
    const v = toViewSpan(labSpan({ name: 'GET /api/accounts/tenants', kind: 'SERVER' }))
    expect(v.subject).toBe('/api/accounts/tenants')
    expect(v.method).toBe('GET')
  })

  it('marks CLIENT and PRODUCER spans outbound', () => {
    expect(toViewSpan(labSpan({ kind: 'CLIENT' })).direction).toBe('outbound')
    expect(toViewSpan(labSpan({ kind: 'PRODUCER' })).direction).toBe('outbound')
    expect(toViewSpan(labSpan({ kind: 'CONSUMER' })).direction).toBe('inbound')
  })

  it('keeps every attribute, as text', () => {
    const v = toViewSpan(labSpan({ attributes: { 'rpc.retry_count': 2, flag: true } }))
    expect(v.attributes).toEqual({ 'rpc.retry_count': '2', flag: 'true' })
  })
})

describe('grafanaTraceUrl (BR-076): "Open in Grafana" for the same trace ID', () => {
  it('opens Grafana Explore on the tempo datasource with the trace ID as the query', () => {
    const url = new URL(grafanaTraceUrl('http://localhost:3050', '9ce968fc29b9e77140d2404a6a8834b8'))
    expect(url.origin).toBe('http://localhost:3050')
    expect(url.pathname).toBe('/explore')
    const panes = JSON.parse(url.searchParams.get('panes'))
    const pane = Object.values(panes)[0]
    expect(pane.datasource).toBe('tempo')
    expect(pane.queries[0]).toMatchObject({ queryType: 'traceql', query: '9ce968fc29b9e77140d2404a6a8834b8', datasource: { uid: 'tempo' } })
  })

  it('is empty when no Grafana URL is configured', () => {
    expect(grafanaTraceUrl('', 'abc')).toBe('')
  })
})

// ── polling ───────────────────────────────────────────────────────────

function summary(traceId, spanCount, start = T0) {
  return { traceId, rootService: 'shipping-service', rootName: 'api.acme.shipping.ship.arrive.v1', startUnixNano: start, durationMs: 20, spanCount, errorCount: 0, services: ['shipping-service'] }
}

function fullTrace(traceId, n) {
  return {
    schema: 'lab.trace.v1',
    traceId,
    spans: Array.from({ length: n }, (_, i) => labSpan({ traceId, spanId: `${traceId.slice(0, 15)}${i}`, parentSpanId: i ? `${traceId.slice(0, 15)}0` : undefined })),
  }
}

function setVisibility(state) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  document.dispatchEvent(new Event('visibilitychange'))
}

function mountGateway(opts) {
  let api
  const Host = defineComponent({
    setup() {
      api = useTraceGateway(opts)
      return () => h('div')
    },
  })
  const wrapper = mount(Host)
  return { wrapper, api: () => api }
}

describe('useTraceGateway (BR-076, decision 8): the Traces panel polls the gateway', () => {
  let search
  let traces

  beforeEach(() => {
    vi.useFakeTimers()
    setVisibility('visible')
    search = { schema: 'lab.tracesearch.v1', limit: 50, limited: false, traces: [summary('a'.repeat(32), 2)] }
    traces = { ['a'.repeat(32)]: fullTrace('a'.repeat(32), 2) }
    conn.connected = ref(true)
    conn.epoch = ref(1)
    conn.request = vi.fn(async (subject, body) => {
      if (subject === TRACE_SEARCH_SUBJECT) return search
      if (subject === TRACE_GET_SUBJECT) {
        const t = traces[body.traceId]
        if (!t) throw Object.assign(new Error('trace not found'), { code: '404', serviceError: true })
        return t
      }
      throw new Error('unexpected subject ' + subject)
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  const searches = () => conn.request.mock.calls.filter(([s]) => s === TRACE_SEARCH_SUBJECT).length
  const gets = () => conn.request.mock.calls.filter(([s]) => s === TRACE_GET_SUBJECT).map(([, b]) => b.traceId)

  it('searches on mount and loads each found trace whole, in the panels\' span shape', async () => {
    const { api, wrapper } = mountGateway()
    await flushPromises()
    expect(searches()).toBe(1)
    expect(gets()).toEqual(['a'.repeat(32)])
    const spans = api().traces.value.get('a'.repeat(32))
    expect(spans).toHaveLength(2)
    expect(spans[0].subject).toBe('api.acme.shipping.ship.arrive.v1')
    expect(api().lastRefresh.value).toBeInstanceOf(Date)
    wrapper.unmount()
  })

  it('polls again every POLL_INTERVAL_MS while the page is visible', async () => {
    const { wrapper } = mountGateway()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(searches()).toBe(3)
    wrapper.unmount()
  })

  it('does not load a trace again while its span count is unchanged', async () => {
    const { wrapper } = mountGateway()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(gets()).toHaveLength(1)
    wrapper.unmount()
  })

  it('fills in a trace when late spans arrive (its span count grows)', async () => {
    const { api, wrapper } = mountGateway()
    await flushPromises()
    search = { ...search, traces: [summary('a'.repeat(32), 3)] }
    traces['a'.repeat(32)] = fullTrace('a'.repeat(32), 3)
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(gets()).toHaveLength(2)
    expect(api().traces.value.get('a'.repeat(32))).toHaveLength(3)
    wrapper.unmount()
  })

  it('pauses while the page is hidden and polls at once when it is shown again', async () => {
    const { wrapper } = mountGateway()
    await flushPromises()
    setVisibility('hidden')
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3)
    expect(searches()).toBe(1)
    setVisibility('visible')
    await flushPromises()
    expect(searches()).toBe(2)
    wrapper.unmount()
  })

  it('pauses while the `active` ref is false (the panel is not shown)', async () => {
    const active = ref(false)
    const { wrapper } = mountGateway({ active })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2)
    expect(searches()).toBe(0)
    active.value = true
    await flushPromises()
    expect(searches()).toBe(1)
    wrapper.unmount()
  })

  it('refreshes at once on a manual refresh', async () => {
    const { api, wrapper } = mountGateway()
    await flushPromises()
    await api().refresh()
    expect(searches()).toBe(2)
    wrapper.unmount()
  })

  it('stops polling when unmounted', async () => {
    const { wrapper } = mountGateway()
    await flushPromises()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3)
    expect(searches()).toBe(1)
  })

  it('says when a search failed, keeps what it holds, and clears the failure on the next good poll', async () => {
    const { api, wrapper } = mountGateway()
    await flushPromises()
    const good = conn.request.getMockImplementation()
    conn.request.mockImplementation(async (subject, body) => {
      if (subject === TRACE_SEARCH_SUBJECT) throw Object.assign(new Error('trace store unavailable'), { code: '503', serviceError: true })
      return good(subject, body)
    })
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(api().bootstrapFailed.value).toBe(true)
    expect(api().error.value).toBe('trace store unavailable')
    expect(api().traces.value.size).toBe(1)
    conn.request.mockImplementation(good)
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(api().bootstrapFailed.value).toBe(false)
    expect(api().error.value).toBe('')
    wrapper.unmount()
  })

  it('passes the search on, and says when the result is capped, never as a total', async () => {
    search = { ...search, limited: true }
    const { api, wrapper } = mountGateway({ filters: () => ({ errorsOnly: true }) })
    await flushPromises()
    expect(conn.request.mock.calls[0][1]).toMatchObject({ errorsOnly: true, limit: 50 })
    expect(api().limited.value).toBe(true)
    wrapper.unmount()
  })

  it('drops a trace the search no longer returns, so the list is the search', async () => {
    const { api, wrapper } = mountGateway()
    await flushPromises()
    search = { ...search, traces: [] }
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(api().traces.value.size).toBe(0)
    wrapper.unmount()
  })

  it('loads one trace by ID on request, even when the search did not return it (the Messages link)', async () => {
    traces['b'.repeat(32)] = fullTrace('b'.repeat(32), 1)
    const { api, wrapper } = mountGateway()
    await flushPromises()
    await api().load('b'.repeat(32))
    expect(api().traces.value.get('b'.repeat(32))).toHaveLength(1)
    // A pinned trace stays after the next poll.
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(api().traces.value.has('b'.repeat(32))).toBe(true)
    wrapper.unmount()
  })

  it('fires onUpsert once per loaded trace with its spans', async () => {
    const onUpsert = vi.fn()
    const { wrapper } = mountGateway({ onUpsert })
    await flushPromises()
    expect(onUpsert).toHaveBeenCalledWith('a'.repeat(32), expect.any(Array))
    wrapper.unmount()
  })
})
