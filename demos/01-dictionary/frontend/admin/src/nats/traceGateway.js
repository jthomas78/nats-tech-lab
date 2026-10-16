// Phase 54c — the Admin UI reads traces from the trace query gateway
// (observability-service's api._platform.observability.trace.*.v1, BR-074,
// BR-075), not from the trace-request-reply KV feed (useTraceFeed.js).
//
// Two jobs, both pinned by traceGateway.spec.js (BR-076):
//
//   - toViewSpan maps one lab.trace.v1 span onto the flat span shape every
//     trace panel already reads (natstrace's traceSpan plus attributedTenant).
//     The panels keep their reading of a span; only the store under it moved.
//   - useTraceGateway polls trace.search while the page is visible and loads
//     each found trace whole through trace.get (decision 8). Tempo has no
//     push, so a new trace shows up late: measured p50 9.9 s on a local cell
//     with TEMPO_QUERY_END_CUTOFF=5s, and about 30 s with Tempo's default.
//     The panel says so instead of hiding it.
import { onMounted, onUnmounted, ref, unref, watch } from 'vue'

import { usePlatformConnection } from './usePlatformConnection.js'

export const TRACE_SEARCH_SUBJECT = 'api._platform.observability.trace.search.v1'
export const TRACE_GET_SUBJECT = 'api._platform.observability.trace.get.v1'
export const TRACE_SERVICES_SUBJECT = 'api._platform.observability.trace.services.v1'

// The poll interval comes from the measured visibility delay. A trace is
// not searchable for about 10 s after it ends, so polling faster than a few
// seconds only repeats the same answer. 5 s keeps the added wait to at most
// half the floor the store already sets.
export const POLL_INTERVAL_MS = 5000
export const SEARCH_LIMIT = 50

const REST_NAME = /^(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS) (\/.*)$/

// isoFromUnixNano keeps the sub-millisecond part, because spanFinishMs reads
// it back (BR-056). A JSON number in nanoseconds has lost a few hundred ns of
// precision already; that is far below the microseconds the panels show.
function isoFromUnixNano(ns) {
  const sec = Math.floor(ns / 1e9)
  const frac = Math.max(0, Math.round(ns - sec * 1e9))
  const whole = new Date(sec * 1000).toISOString().slice(0, 19)
  return `${whole}.${String(Math.min(frac, 999999999)).padStart(9, '0')}Z`
}

function parseBody(text) {
  if (typeof text !== 'string') return text
  try {
    return JSON.parse(text)
  } catch {
    return text // a cut (truncated) body is not JSON; keep it as text
  }
}

function parseHeaders(text) {
  if (!text) return {}
  if (typeof text === 'object') return text
  try {
    return JSON.parse(text) ?? {}
  } catch {
    return {}
  }
}

function firstHeader(headers, name) {
  const v = headers?.[name]
  return Array.isArray(v) ? v[0] : v
}

function asText(attributes) {
  const out = {}
  for (const [k, v] of Object.entries(attributes ?? {})) {
    out[k] = typeof v === 'string' ? v : Array.isArray(v) || (v && typeof v === 'object') ? JSON.stringify(v) : String(v)
  }
  return out
}

// toViewSpan maps a lab.trace.v1 span to the panels' span shape.
export function toViewSpan(span) {
  const events = span.events ?? []
  const req = events.find((e) => e.name === 'rpc.request.body')?.attributes
  const rep = events.find((e) => e.name === 'rpc.reply.body')?.attributes
  const headers = { ...parseHeaders(req?.headers), ...parseHeaders(rep?.headers) }
  const rest = REST_NAME.exec(span.name ?? '')
  const kind = span.kind ?? ''
  const attributes = span.attributes ?? {}

  const view = {
    traceId: span.traceId,
    spanId: span.spanId,
    parentSpanId: span.parentSpanId,
    service: span.service,
    subject: rest ? rest[2] : span.name,
    name: span.name,
    kind,
    direction: kind === 'CLIENT' || kind === 'PRODUCER' ? 'outbound' : 'inbound',
    timestamp: isoFromUnixNano(span.endUnixNano),
    // Whole microseconds, as natstrace published them (BR-056).
    durationUs: Math.round((span.endUnixNano - span.startUnixNano) / 1000),
    statusCode: span.statusCode,
    statusMessage: span.statusMessage,
    attributes: asText(attributes),
    attributedTenant: typeof attributes['lab.tenant.originating'] === 'string' ? attributes['lab.tenant.originating'] : '',
    headers,
    requester: firstHeader(headers, 'Nats-Requestor'),
    events,
    links: span.links ?? [],
  }
  if (rest) view.method = rest[1]
  if (span.statusCode === 'ERROR') view.error = span.statusMessage || 'error'
  if (req) {
    view.requestPayload = parseBody(req.body)
    view.requestPayloadBytes = req['body.bytes']
    view.requestRedacted = req.redacted ?? []
    view.requestTruncated = req.truncated === true
  }
  if (rep) {
    view.payload = parseBody(rep.body)
    view.payloadBytes = rep['body.bytes']
    view.redacted = rep.redacted ?? []
    view.truncated = rep.truncated === true
  }
  return view
}

// grafanaTraceUrl builds "Open in Grafana" for one trace ID: Explore on the
// tempo datasource, the trace ID as a TraceQL query. Empty when the build
// did not set a Grafana URL (VITE_GRAFANA_URL), so the panel hides the link.
export function grafanaTraceUrl(base, traceId) {
  if (!base || !traceId) return ''
  const panes = {
    trace: {
      datasource: 'tempo',
      queries: [{ refId: 'A', datasource: { type: 'tempo', uid: 'tempo' }, queryType: 'traceql', query: traceId }],
      range: { from: 'now-1h', to: 'now' },
    },
  }
  const url = new URL('/explore', base)
  url.searchParams.set('schemaVersion', '1')
  url.searchParams.set('panes', JSON.stringify(panes))
  return url.toString()
}

export function grafanaBaseUrl() {
  return import.meta.env?.VITE_GRAFANA_URL ?? ''
}

export function searchTraces(request, filters = {}) {
  return request(TRACE_SEARCH_SUBJECT, { limit: SEARCH_LIMIT, ...filters })
}

export async function getTrace(request, traceId) {
  const trace = await request(TRACE_GET_SUBJECT, { traceId })
  return (trace.spans ?? []).map(toViewSpan)
}

export function traceServices(request) {
  return request(TRACE_SERVICES_SUBJECT, {})
}

// useTraceGateway returns what useTraceFeed returned — {traces, connected,
// bootstrapFailed} — so a panel swaps one for the other, plus the polling
// controls the panel shows.
//
//   active   a ref; false pauses polling (a hidden tab of RpcPanel)
//   filters  a function returning the search body (service, name, …)
//   onUpsert called once per loaded trace with its spans
export function useTraceGateway({ active, filters, onUpsert } = {}) {
  const { connected, epoch, request } = usePlatformConnection()
  const traces = ref(new Map())
  const bootstrapFailed = ref(false)
  const error = ref('')
  const limited = ref(false)
  const lastRefresh = ref(null)
  const loading = ref(false)

  const spanCounts = new Map() // traceId -> spanCount the last load saw
  const pinned = new Set() // traces loaded by ID; a poll never drops them
  let timer = null
  let stopped = false
  let inFlight = null

  const isActive = () => (active === undefined ? true : unref(active) !== false)
  const visible = () => typeof document === 'undefined' || document.visibilityState !== 'hidden'
  const shouldPoll = () => !stopped && isActive() && visible() && unref(connected) !== false

  function setTrace(traceId, spans) {
    const next = new Map(traces.value)
    next.set(traceId, spans)
    traces.value = next
    onUpsert?.(traceId, spans)
  }

  async function poll() {
    const result = await searchTraces(request, filters?.() ?? {})
    limited.value = result.limited === true
    const keep = new Set(pinned)
    for (const s of result.traces ?? []) {
      keep.add(s.traceId)
      if (spanCounts.get(s.traceId) === s.spanCount && traces.value.has(s.traceId)) continue
      try {
        const spans = await getTrace(request, s.traceId)
        spanCounts.set(s.traceId, s.spanCount)
        setTrace(s.traceId, spans)
      } catch {
        // A trace found but not yet readable is tried again on the next poll.
      }
    }
    if ([...traces.value.keys()].some((id) => !keep.has(id))) {
      const next = new Map([...traces.value].filter(([id]) => keep.has(id)))
      for (const id of spanCounts.keys()) if (!keep.has(id)) spanCounts.delete(id)
      traces.value = next
    }
  }

  async function refresh() {
    if (inFlight) return inFlight
    loading.value = true
    inFlight = (async () => {
      try {
        await poll()
        bootstrapFailed.value = false
        error.value = ''
        lastRefresh.value = new Date()
      } catch (err) {
        // Keep what is held; say the last poll failed.
        bootstrapFailed.value = true
        error.value = err instanceof Error ? err.message : String(err)
      } finally {
        loading.value = false
        inFlight = null
      }
    })()
    return inFlight
  }

  // load reads one trace by ID, whether or not the search returned it. A
  // Messages row links here, and its trace can be older than the window.
  async function load(traceId) {
    const spans = await getTrace(request, traceId)
    pinned.add(traceId)
    spanCounts.set(traceId, spans.length)
    setTrace(traceId, spans)
    return spans
  }

  function schedule() {
    clearTimeout(timer)
    timer = null
    if (!shouldPoll()) return
    timer = setTimeout(async () => {
      await refresh()
      schedule()
    }, POLL_INTERVAL_MS)
  }

  async function wake() {
    if (!shouldPoll()) {
      clearTimeout(timer)
      timer = null
      return
    }
    await refresh()
    schedule()
  }

  function onVisibility() {
    wake()
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', onVisibility)
    wake()
  })
  onUnmounted(() => {
    stopped = true
    clearTimeout(timer)
    document.removeEventListener('visibilitychange', onVisibility)
  })
  if (active !== undefined) watch(() => unref(active), wake)
  watch(() => unref(epoch), wake)
  watch(() => unref(connected), wake)

  return { traces, connected, bootstrapFailed, error, limited, lastRefresh, loading, refresh, load }
}
