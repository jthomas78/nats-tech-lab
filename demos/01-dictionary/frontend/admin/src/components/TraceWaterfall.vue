<script setup>
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'

import { getKvBucketEntries } from '../api'
import { highlightJson } from '../jsonHighlight.js'
import { parseKvNotifySubject } from '../nats/kvNotifySubject.js'
import { usePlatformConnection } from '../nats/usePlatformConnection.js'
import { useUiStore } from '../stores/ui.js'
import SubjectPath from './SubjectPath.vue'

// Phase 28g / BR-035 — one row per TRACE, not per message: every hop one
// originating request caused, assembled server-side by shipping-service's
// TRACES stream consumer (dictionary/internal/eventhandler/trace_store.go)
// into the trace-request-reply KV bucket, keyed _platform.trace.{traceId}. This panel
// reads that bucket exactly the way every other KV panel in this app does —
// GET /api/kv/buckets/platform/trace-request-reply/entries for bootstrap, then
// notify._platform.kv.trace-request-reply.> on the PLATFORM connection for live updates
// (internal/kvstore.Store.EnableNotify, reused unchanged rather than a
// bespoke trace-notify bridge — see BUSINESS_RULES-SHIPPING.md's BR-036
// Phase 28g amendment).
//
// Two known simplifications versus the approved mockup
// (diagrams/admin-traces-panel.html), both because the wire span
// (natstrace.go's traceSpan) doesn't carry the fields the mockup's fixture
// data assumed:
//  - No per-span "context"/tenant field is ever serialized (only
//    StartOutbound's caller-supplied contextValue, used solely to build the
//    obs.trace.{context}... publish subject, never stored in the payload
//    itself) — so the account gutter can only show a coarse PLATFORM/TENANT
//    split (accountOf below, keyed off `service`), not which specific
//    tenant. This still surfaces the one crossing BR-035's crux scenario
//    cares about (a tenant service calling a PLATFORM service).
//  - No OTel spanKind (client/server/producer/consumer/internal) exists on
//    the wire either — direction is always "reply" (BR-037's one-span-per-
//    call design), so the mockup's detail-pane "kind" tag is omitted rather
//    than shown with a fabricated value.

const traces = ref(new Map()) // traceId -> raw span objects (from the KV record's `spans` array)

const PLATFORM_SERVICES = new Set(['refdata', 'accounts'])
function accountOf(span) {
  return PLATFORM_SERVICES.has(span?.service) ? 'PLATFORM' : 'TENANT'
}

function isRoot(span) {
  return !span.parentSpanId
}

function upsertTrace(traceId, spans) {
  const next = new Map(traces.value)
  next.set(traceId, spans)
  traces.value = next
}

async function bootstrap() {
  let entries
  try {
    entries = await getKvBucketEntries('platform', 'trace-request-reply')
  } catch {
    return // best-effort bootstrap — live subscribe below still works even if this fails
  }
  for (const entry of entries ?? []) {
    const record = entry?.value
    if (entry?.op !== 'PUT' || !record?.traceId || !Array.isArray(record.spans)) continue
    upsertTrace(record.traceId, record.spans)
  }
}

const { connected: platformConnected, subscribe: subscribePlatform } = usePlatformConnection()
let unsubscribe = null

function connectLive() {
  if (!platformConnected.value) return
  unsubscribe = subscribePlatform('notify._platform.kv.trace-request-reply.>', (payload, subject) => {
    const parsed = parseKvNotifySubject(subject)
    if (!parsed || !payload?.traceId || !Array.isArray(payload.spans)) return
    // internal/kvstore's internal key is {kvContext}.{key} — here
    // "_platform.trace.{traceId}" — so the notify's key segment (everything
    // after the bucket token) already carries the "trace." prefix baked in.
    const traceId = parsed.key.startsWith('trace.') ? parsed.key.slice('trace.'.length) : payload.traceId
    upsertTrace(traceId, payload.spans)
  })
}
function disconnectLive() {
  unsubscribe?.()
  unsubscribe = null
}

onMounted(() => {
  bootstrap()
  connectLive()
})
onUnmounted(() => {
  disconnectLive()
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', stopResize)
  window.removeEventListener('mousemove', onSpanListResizeMove)
  window.removeEventListener('mouseup', stopSpanListResize)
})
watch(platformConnected, (isConnected) => {
  if (isConnected) {
    disconnectLive()
    connectLive()
  }
})

// Draggable trace-list rail — width lives in the ui store (not a local ref)
// for the same reason rpcTab/accountsTab do: App.vue's v-else-if nav tears
// this component down on every section switch, and the width should survive
// that the way a real split-pane would.
const ui = useUiStore()
const RAIL_WIDTH_MIN = 240
const RAIL_WIDTH_MAX = 640
const RAIL_WIDTH_STEP = 20
const resizingRail = ref(false)
let resizeStartX = 0
let resizeStartWidth = 0

function clampRailWidth(px) {
  return Math.min(RAIL_WIDTH_MAX, Math.max(RAIL_WIDTH_MIN, px))
}
function onResizeMove(evt) {
  ui.traceRailWidth = clampRailWidth(resizeStartWidth + (evt.clientX - resizeStartX))
}
function stopResize() {
  resizingRail.value = false
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', stopResize)
}
function startResize(evt) {
  resizingRail.value = true
  resizeStartX = evt.clientX
  resizeStartWidth = ui.traceRailWidth
  window.addEventListener('mousemove', onResizeMove)
  window.addEventListener('mouseup', stopResize)
  evt.preventDefault()
}
function adjustRailWidth(delta) {
  ui.traceRailWidth = clampRailWidth(ui.traceRailWidth + delta)
}

// Draggable Span list / Span details divider (Phase 28j) — same
// px-in-the-ui-store pattern as the trace rail above, vertical instead of
// horizontal.
const SPAN_LIST_HEIGHT_MIN = 120
const SPAN_LIST_HEIGHT_MAX = 640
const SPAN_LIST_HEIGHT_STEP = 20
const resizingSpanList = ref(false)
let resizeStartY = 0
let resizeStartHeight = 0

function clampSpanListHeight(px) {
  return Math.min(SPAN_LIST_HEIGHT_MAX, Math.max(SPAN_LIST_HEIGHT_MIN, px))
}
function onSpanListResizeMove(evt) {
  ui.spanListHeight = clampSpanListHeight(resizeStartHeight + (evt.clientY - resizeStartY))
}
function stopSpanListResize() {
  resizingSpanList.value = false
  window.removeEventListener('mousemove', onSpanListResizeMove)
  window.removeEventListener('mouseup', stopSpanListResize)
}
function startSpanListResize(evt) {
  resizingSpanList.value = true
  resizeStartY = evt.clientY
  resizeStartHeight = ui.spanListHeight
  window.addEventListener('mousemove', onSpanListResizeMove)
  window.addEventListener('mouseup', stopSpanListResize)
  evt.preventDefault()
}
function adjustSpanListHeight(delta) {
  ui.spanListHeight = clampSpanListHeight(ui.spanListHeight + delta)
}

// ── Per-trace summary, built once per traces.value change ────────────────
// `span.timestamp` carries sub-millisecond (usually nanosecond) precision
// from the backend, e.g. "...T18:15:51.265438763Z" — but `new Date(...).getTime()`
// truncates to whole milliseconds. Two spans whose finish times differ by a
// fraction of a millisecond (routine for calls that only take 1-2ms) then
// collapse to the SAME millisecond, tying `ownStart`/`ownFinish` for both —
// `waterfallRows`' sort then falls back to array order (`t.spans`'
// publish/insertion order), which can and does put a child span above its
// own parent. Parsing the fractional-second digits directly (not through
// Date) keeps that sub-millisecond delta, so causally-ordered spans (parent
// always finishes at or after the child that caused it) sort correctly even
// when they're only a fraction of a millisecond apart.
function preciseFinishMs(iso) {
  const match = /\.(\d+)Z$/.exec(iso)
  if (!match) return new Date(iso).getTime()
  const wholeSecondMs = new Date(iso.slice(0, match.index) + 'Z').getTime()
  const fractionMs = Number(match[1].padEnd(9, '0').slice(0, 9)) / 1e6
  return wholeSecondMs + fractionMs
}
function ownStart(span) {
  return ownFinish(span) - (span.durationMs || 0)
}
function ownFinish(span) {
  return preciseFinishMs(span.timestamp)
}

// A trace's transport kind is read off its ROOT span's subject: an HTTP
// entry point's subject is always a URL path ("/api/..." — httpEntity in
// trace_middleware.go derives the span's entity from exactly this leading
// slash), every other subject shape (api.*/rpc.*/bare aliases like
// refdata.type.list.v1) is NATS. Classifying by the root rather than any
// span in the trace matches how the trace list already reads root subject
// for everything else (search, "n spans" summary) — a trace that starts as
// a browser HTTP call and fans out into NATS hops underneath is still a
// REST trace at a glance, same as Jaeger/Tempo call it by the root span's
// transport, not by counting hop types.
function traceKind(rootSubject) {
  return rootSubject?.startsWith('/') ? 'rest' : 'nats'
}

function summarize(traceId, spans) {
  const root = spans.find(isRoot) ?? spans.reduce((a, b) => (ownStart(a) <= ownStart(b) ? a : b), spans[0])
  const traceStart = Math.min(...spans.map(ownStart))
  const traceEnd = Math.max(...spans.map(ownFinish))
  const total = Math.max(traceEnd - traceStart, 0)
  const replyMs = root?.durationMs ?? total
  const rootFinish = root ? ownFinish(root) : traceEnd
  const hasAsyncTail = spans.some((s) => s !== root && ownFinish(s) > rootFinish)
  const consistentMs = hasAsyncTail ? total : null
  const ok = !spans.some((s) => s.statusCode === 'ERROR')
  const accounts = new Set(spans.map(accountOf))
  return {
    traceId,
    spans,
    root,
    ok,
    total,
    replyMs,
    consistentMs,
    accountCount: accounts.size,
    spanCount: spans.length,
    at: traceStart,
    kind: traceKind(root?.subject),
  }
}

const traceSummaries = computed(() =>
  Array.from(traces.value.entries())
    .map(([traceId, spans]) => summarize(traceId, spans))
    .sort((a, b) => b.at - a.at),
)

// ── Toolbar — pause, search, errors-only, slow-only (mirrors RpcPanel's own
// filter/pause conventions) ───────────────────────────────────────────────
const searchText = ref('')
const filters = reactive({ errorsOnly: false, slowOnly: false })
function toggleFilter(name) {
  filters[name] = !filters[name]
}
// kind is exclusive (all/rest/nats), unlike errorsOnly/slowOnly above — it's
// one axis with three states, not an independently AND-combinable toggle;
// two simultaneously-"on" boolean chips for rest/nats would just mean "all"
// again, so a segmented control models the actual choice instead of two
// booleans that could contradict what's rendered.
const kindFilter = ref('all')
function setKindFilter(kind) {
  kindFilter.value = kind
}

const paused = ref(false)
const frozenOrder = ref([])
function togglePause() {
  if (!paused.value) frozenOrder.value = traceSummaries.value.map((t) => t.traceId)
  paused.value = !paused.value
}
const displayedSummaries = computed(() => {
  const base = paused.value
    ? frozenOrder.value.map((id) => traceSummaries.value.find((t) => t.traceId === id)).filter(Boolean)
    : traceSummaries.value
  return base.filter((t) => {
    if (filters.errorsOnly && t.ok) return false
    if (filters.slowOnly && t.total <= 100) return false
    if (kindFilter.value !== 'all' && t.kind !== kindFilter.value) return false
    if (searchText.value && !(t.root?.subject || '').toLowerCase().includes(searchText.value.toLowerCase())) return false
    return true
  })
})

// ── Pulse strip — request/error/latency histograms over the currently
// displayed (i.e. filtered) trace window, so toggling errors/slow/rest-nats
// in the toolbar reshapes this strip and the trace list together (Phase
// 28p). Buckets are fixed-count, not fixed-duration: this panel has no
// historical metrics backend, just the live-buffered trace set, so the
// window is "however far back the buffer reaches" rather than a calendar
// interval.
const PULSE_BUCKETS = 20
const pulse = computed(() => {
  const list = displayedSummaries.value
  const empty = { total: 0, errCount: 0, errPct: '0%', avgLatency: 0, currentLatency: 0, reqBars: [], errBars: [], latPoints: '', latArea: '', hasLat: false }
  if (list.length === 0) return empty

  const ats = list.map((t) => t.at)
  const minAt = Math.min(...ats)
  const maxAt = Math.max(...ats)
  const span = maxAt > minAt ? maxAt - minAt : 30000 // degenerate single-instant window: widen so bucketing doesn't divide by zero

  const reqCounts = new Array(PULSE_BUCKETS).fill(0)
  const errCounts = new Array(PULSE_BUCKETS).fill(0)
  const latSums = new Array(PULSE_BUCKETS).fill(0)
  const latCounts = new Array(PULSE_BUCKETS).fill(0)
  for (const t of list) {
    const idx = Math.min(PULSE_BUCKETS - 1, Math.max(0, Math.floor(((t.at - minAt) / span) * PULSE_BUCKETS)))
    reqCounts[idx] += 1
    if (!t.ok) errCounts[idx] += 1
    latSums[idx] += t.replyMs
    latCounts[idx] += 1
  }

  const reqMax = Math.max(1, ...reqCounts)
  const errMax = Math.max(1, ...errCounts)
  const reqBars = reqCounts.map((v, i) => ({ h: v > 0 ? Math.max(3, (v / reqMax) * 34) : 1, now: i === PULSE_BUCKETS - 1 }))
  const errBars = errCounts.map((v, i) => ({ h: v > 0 ? Math.max(3, (v / errMax) * 34) : 1, now: i === PULSE_BUCKETS - 1, empty: v === 0 }))

  const latPointsRaw = []
  for (let i = 0; i < PULSE_BUCKETS; i += 1) {
    if (latCounts[i] > 0) latPointsRaw.push([i, latSums[i] / latCounts[i]])
  }
  let latPoints = ''
  let latArea = ''
  if (latPointsRaw.length > 0) {
    const vals = latPointsRaw.map(([, v]) => v)
    const latMin = Math.min(...vals)
    const latMax = Math.max(...vals)
    const xy = latPointsRaw.map(([i, v]) => {
      const x = (i / (PULSE_BUCKETS - 1)) * 200
      const y = latMax > latMin ? 32 - ((v - latMin) / (latMax - latMin)) * 30 : 17
      return `${x},${y}`
    })
    latPoints = xy.join(' ')
    latArea = `0,34 ${xy.join(' ')} 200,34`
  }

  const total = list.length
  const errCount = list.filter((t) => !t.ok).length
  return {
    total,
    errCount,
    errPct: `${((errCount / total) * 100).toFixed(1)}%`,
    avgLatency: Math.round(list.reduce((sum, t) => sum + t.replyMs, 0) / total),
    currentLatency: Math.round(list[0].replyMs), // displayedSummaries is sorted newest-first
    reqBars,
    errBars,
    latPoints,
    latArea,
    hasLat: latPointsRaw.length > 0,
  }
})

// ── Selection ──────────────────────────────────────────────────────────────
const selectedTraceId = ref(null)
const selectedSpanId = ref(null)
watch(
  displayedSummaries,
  (list) => {
    if (selectedTraceId.value && list.some((t) => t.traceId === selectedTraceId.value)) return
    selectedTraceId.value = list[0]?.traceId ?? null
  },
  { immediate: true },
)

const selectedTrace = computed(() => traceSummaries.value.find((t) => t.traceId === selectedTraceId.value) ?? null)

function selectTrace(traceId) {
  selectedTraceId.value = traceId
  const t = traceSummaries.value.find((s) => s.traceId === traceId)
  selectedSpanId.value = t?.root?.spanId ?? t?.spans[0]?.spanId ?? null
}

// ── Waterfall rows for the selected trace ─────────────────────────────────
const waterfallRows = computed(() => {
  const t = selectedTrace.value
  if (!t) return []
  const byId = new Map(t.spans.map((s) => [s.spanId, s]))
  function depthOf(span) {
    let d = 0
    let cur = span
    const seen = new Set()
    while (cur?.parentSpanId && byId.has(cur.parentSpanId) && !seen.has(cur.parentSpanId)) {
      seen.add(cur.parentSpanId)
      d++
      cur = byId.get(cur.parentSpanId)
    }
    return d
  }
  const rootReplyMs = t.root?.durationMs ?? t.total
  const rowById = new Map(
    t.spans.map((span) => {
      const offset = ownStart(span) - t.at
      const parent = span.parentSpanId ? byId.get(span.parentSpanId) : null
      return [
        span.spanId,
        {
          span,
          depth: depthOf(span),
          offset,
          durationMs: span.durationMs || 0,
          account: accountOf(span),
          crossing: !!parent && accountOf(span) !== accountOf(parent),
          kind: span.statusCode === 'ERROR' ? 'bad' : offset >= rootReplyMs && span !== t.root ? 'evtl' : 'sync',
        },
      ]
    }),
  )
  // Walk the known parentSpanId tree (pre-order: a span always immediately
  // precedes its own subtree) rather than flat-sorting every row by `offset`
  // alone — `offset` only breaks ties AMONG SIBLINGS below, never reorders a
  // span relative to its own ancestor/descendant. A flat sort trusted
  // `ownStart` (finish minus `durationMs`) completely, but `durationMs` is
  // whole-millisecond-truncated server-side (Go's `time.Duration.
  // Milliseconds()`): a parent whose true duration is only a fraction of a
  // millisecond longer than its child's can truncate to the SAME integer
  // duration, so subtracting that duration from each one's own (precise)
  // finish time can land the parent's estimated start AFTER the child's —
  // inverting a relationship parentSpanId already tells us is true, no
  // clock-precision bug required. Real example (Phase 28m): an HTTP root
  // span (66ms) and its own outbound rpc.* child (66ms, truncated from a
  // hair less) sorted the child above the root. Walking the tree makes
  // parent-before-descendant structural rather than incidental.
  const childrenByParent = new Map()
  for (const row of rowById.values()) {
    const parentId = row.span.parentSpanId
    const key = parentId && rowById.has(parentId) ? parentId : null
    if (!childrenByParent.has(key)) childrenByParent.set(key, [])
    childrenByParent.get(key).push(row)
  }
  for (const siblings of childrenByParent.values()) siblings.sort((a, b) => a.offset - b.offset)
  const rows = []
  ;(function visit(parentKey) {
    for (const row of childrenByParent.get(parentKey) ?? []) {
      rows.push(row)
      visit(row.span.spanId)
    }
  })(null)

  if (!t.consistentMs) return rows

  const out = []
  let ackInserted = false
  for (const row of rows) {
    if (!ackInserted && row.kind === 'evtl') {
      out.push({ ack: true, replyMs: rootReplyMs, tailMs: t.consistentMs - rootReplyMs })
      ackInserted = true
    }
    out.push(row)
  }
  return out
})

const selectedSpan = computed(() => selectedTrace.value?.spans.find((s) => s.spanId === selectedSpanId.value) ?? null)
const selectedSpanAccount = computed(() => accountOf(selectedSpan.value))

// ── Formatting ──────────────────────────────────────────────────────────────
function formatDuration(ms) {
  if (ms == null) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${Math.round(ms)}ms`
}
function formatTimeMs(ms) {
  if (!ms) return '—'
  const d = new Date(ms)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  const msPart = String(d.getMilliseconds()).padStart(3, '0')
  return `${hh}:${mm}:${ss}.${msPart}`
}
// Splits one span's already-merged headers/attributes (mergeHeaders in
// natstrace.go puts both the caller's and the handler's onto the same span)
// into a Request side and a Response side by key name — no new wire field,
// just classifying what's already there so "who called this" and "who
// answered" stop reading as one undifferentiated list.
const REQUEST_HEADER_KEYS = new Set(['Nats-Requestor', 'traceparent', 'http.method', 'http.path'])
const REQUEST_ATTRIBUTE_KEYS = new Set(['http.method', 'http.path', 'rpc.retry_count'])

function splitRows(obj, requestKeys) {
  const req = []
  const resp = []
  for (const [k, v] of Object.entries(obj ?? {})) {
    ;(requestKeys.has(k) ? req : resp).push({ k, v: Array.isArray(v) ? v.join(', ') : String(v) })
  }
  return { req, resp }
}

function headerValue(headers, key) {
  const v = headers?.[key]
  if (v == null) return null
  return Array.isArray(v) ? v.join(', ') : String(v)
}

const headerSplit = computed(() => splitRows(selectedSpan.value?.headers, REQUEST_HEADER_KEYS))
const attributeSplit = computed(() => splitRows(selectedSpan.value?.attributes, REQUEST_ATTRIBUTE_KEYS))
const requestedBy = computed(() => headerValue(selectedSpan.value?.headers, 'Nats-Requestor'))
const respondedBy = computed(() => headerValue(selectedSpan.value?.headers, 'Nats-Responder'))
// accounts-service's HTTPMiddleware mints its span from r.URL.Path (natstrace.go's
// StartFromHeaders), so an HTTP-transport span's subject always starts with "/" —
// the same signal that distinguishes it from every rpc.*/api.*/evt.* NATS subject.
// Phase 28i closed the bug where a real NATS span could show no requestor at all
// (StartFromHeaders discarding inbound headers, StartOutbound never capturing its
// own) — what's left really is just "HTTP has no NATS header to report", not an
// unexplained gap, so the empty state should say that rather than read as a bug.
const requestedByEmptyLabel = computed(() =>
  selectedSpan.value?.subject?.startsWith('/')
    ? 'HTTP entry point — no NATS requestor'
    : '— (no Nats-Requestor on this span)',
)
const AXIS_TICKS = [0, 0.25, 0.5, 0.75, 1]
</script>

<template>
  <div class="trace-waterfall">
    <div class="tw-toolbar">
      <Tag
        :severity="platformConnected ? 'success' : 'danger'"
        :value="platformConnected ? 'live' : 'disconnected'"
      />
      <span class="search-box">
        <i class="pi pi-search" />
        <input
          v-model="searchText"
          type="text"
          placeholder="filter traces by root subject"
        >
      </span>
      <button
        type="button"
        class="chip"
        :class="{ on: filters.errorsOnly, err: true }"
        @click="toggleFilter('errorsOnly')"
      >
        errors
      </button>
      <button
        type="button"
        class="chip"
        :class="{ on: filters.slowOnly }"
        @click="toggleFilter('slowOnly')"
      >
        slow &gt;100ms
      </button>
      <div
        class="kind-group"
        role="group"
        aria-label="filter by transport"
      >
        <button
          v-for="k in ['all', 'rest', 'nats']"
          :key="k"
          type="button"
          :data-k="k"
          :class="{ on: kindFilter === k }"
          @click="setKindFilter(k)"
        >
          <i v-if="k !== 'all'" />{{ k }}
        </button>
      </div>
      <button
        type="button"
        class="pause-btn"
        @click="togglePause"
      >
        {{ paused ? '▶ resume' : '⏸ pause' }}
      </button>
    </div>

    <div
      v-if="displayedSummaries.length > 0"
      class="pulse-strip"
    >
      <div class="pulse-card">
        <div class="pulse-head">
          <span class="pulse-label">requests</span>
          <span class="pulse-window">last {{ pulse.total }} trace{{ pulse.total === 1 ? '' : 's' }}</span>
        </div>
        <div class="pulse-value-row">
          <span class="pulse-value accent">{{ pulse.total }}</span>
        </div>
        <div class="pulse-chart">
          <div
            v-for="(b, i) in pulse.reqBars"
            :key="i"
            class="pulse-bar"
            :class="{ now: b.now }"
            :style="{ height: b.h + 'px' }"
          />
        </div>
      </div>

      <div class="pulse-card">
        <div class="pulse-head">
          <span class="pulse-label">errors</span>
          <span class="pulse-window">{{ pulse.errPct }} of window</span>
        </div>
        <div class="pulse-value-row">
          <span class="pulse-value err">{{ pulse.errCount }}</span>
        </div>
        <div class="pulse-chart">
          <div
            v-for="(b, i) in pulse.errBars"
            :key="i"
            class="pulse-bar err"
            :class="{ now: b.now, empty: b.empty }"
            :style="{ height: b.h + 'px' }"
          />
        </div>
      </div>

      <div class="pulse-card">
        <div class="pulse-head">
          <span class="pulse-label">avg latency</span>
          <span class="pulse-window">{{ pulse.currentLatency }}ms now</span>
        </div>
        <div class="pulse-value-row">
          <span class="pulse-value accent">{{ pulse.avgLatency }}</span>
          <span class="pulse-unit">ms avg</span>
        </div>
        <div class="pulse-line-wrap">
          <svg
            viewBox="0 0 200 34"
            preserveAspectRatio="none"
          >
            <template v-if="pulse.hasLat">
              <polygon
                :points="pulse.latArea"
                class="pulse-area"
              />
              <polyline
                :points="pulse.latPoints"
                class="pulse-line"
              />
            </template>
          </svg>
        </div>
      </div>
    </div>

    <div
      class="tw-split"
      :style="{ gridTemplateColumns: `${ui.traceRailWidth}px 6px minmax(0, 1fr)` }"
    >
      <div class="tw-list">
        <div class="tw-list-head">
          <span class="tw-panel-title">traces</span><span class="tw-panel-count">{{ displayedSummaries.length }}</span>
        </div>
        <div class="tw-list-body">
          <button
            v-for="t in displayedSummaries"
            :key="t.traceId"
            type="button"
            class="tw-trace"
            :aria-current="t.traceId === selectedTraceId"
            @click="selectTrace(t.traceId)"
          >
            <span
              class="tw-dot"
              :class="t.ok ? 'ok' : 'err'"
            />
            <span class="tw-trace-main">
              <span class="tw-trace-subject">
                <span
                  class="kind-tag"
                  :class="t.kind"
                >{{ t.kind }}</span>
                <SubjectPath :subject="t.root?.subject || ''" />
              </span>
              <span class="tw-trace-meta">
                <span>{{ formatTimeMs(t.at) }}</span>
                <span>{{ t.accountCount }} account{{ t.accountCount === 1 ? '' : 's' }}</span>
              </span>
            </span>
            <span class="tw-trace-right">
              <span
                class="tw-trace-dur"
                :class="{ err: !t.ok }"
              >{{ formatDuration(t.total) }}</span>
              <span class="tw-trace-spans">{{ t.spanCount }} spans</span>
            </span>
          </button>
          <div
            v-if="!displayedSummaries.length"
            class="lab-muted tw-empty"
          >
            Waiting for obs.trace.* traffic — trigger any api.*/rpc.* call to see a trace here.
          </div>
        </div>
      </div>

      <div
        class="tw-resize-handle"
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize trace list"
        :aria-valuenow="ui.traceRailWidth"
        :aria-valuemin="RAIL_WIDTH_MIN"
        :aria-valuemax="RAIL_WIDTH_MAX"
        :class="{ active: resizingRail }"
        tabindex="0"
        @mousedown="startResize"
        @keydown.left="adjustRailWidth(-RAIL_WIDTH_STEP)"
        @keydown.right="adjustRailWidth(RAIL_WIDTH_STEP)"
      >
        <span class="tw-grip">
          <span /><span /><span />
        </span>
      </div>

      <div class="tw-wf">
        <template v-if="selectedTrace">
          <div class="tw-wf-head">
            <span class="tw-wf-tid">trace <b>{{ selectedTrace.traceId }}</b></span>
            <span class="tw-stat"><span class="k">reply</span><span
              class="v"
              :class="{ bad: !selectedTrace.ok }"
            >{{ formatDuration(selectedTrace.replyMs) }}</span></span>
            <span class="tw-stat">
              <span class="k">read model consistent</span>
              <span
                class="v"
                :class="{ evtl: selectedTrace.consistentMs }"
              >
                <template v-if="selectedTrace.consistentMs">{{ formatDuration(selectedTrace.consistentMs) }} <small>· +{{ Math.round(selectedTrace.consistentMs - selectedTrace.replyMs) }}ms</small></template>
                <small v-else>never — no async tail</small>
              </span>
            </span>
            <span class="tw-stat"><span class="k">spans</span><span class="v">{{ selectedTrace.spanCount }}</span></span>
            <span class="tw-stat"><span class="k">accounts</span><span class="v">{{ selectedTrace.accountCount }}</span></span>
          </div>

          <div
            class="tw-wf-body"
            :style="{ gridTemplateRows: `${ui.spanListHeight}px 6px minmax(0, 1fr)` }"
          >
            <div class="tw-panel tw-panel-list">
              <div class="tw-panel-head">
                <span class="tw-panel-title">Span list</span>
                <span class="tw-panel-count">{{ selectedTrace.spanCount }} span{{ selectedTrace.spanCount === 1 ? '' : 's' }}</span>
              </div>
              <div class="tw-panel-scroll">
                <div class="tw-grid tw-axis">
                  <span /><span /><span />
                  <span class="tw-ticks">
                    <span
                      v-for="f in AXIS_TICKS"
                      :key="f"
                      class="tw-tick"
                      :style="{ left: f * 100 + '%' }"
                    >{{ formatDuration(Math.round(selectedTrace.total * f)) }}</span>
                  </span>
                </div>

                <div class="tw-rows">
                  <template
                    v-for="(row, i) in waterfallRows"
                    :key="i"
                  >
                    <div
                      v-if="row.ack"
                      class="tw-ack"
                    >
                      <span class="l">reply sent · {{ formatDuration(row.replyMs) }} — client unblocked</span>
                      <span class="r">everything below is eventual · read model catches up {{ Math.round(row.tailMs) }}ms later</span>
                    </div>
                    <button
                      v-else
                      type="button"
                      class="tw-row tw-grid"
                      :aria-current="row.span.spanId === selectedSpanId"
                      @click="selectedSpanId = row.span.spanId"
                    >
                      <span
                        class="tw-acct"
                        :class="[row.account.toLowerCase(), { cross: row.crossing }]"
                        :title="row.account"
                      >
                        {{ row.crossing ? '⇥ ' + row.account : row.account }}
                      </span>
                      <span class="tw-nm">
                        <span
                          v-for="d in row.depth"
                          :key="d"
                          class="tw-rail"
                        />
                        <span class="tw-txt"><SubjectPath :subject="row.span.subject" /></span>
                        <span class="tw-svc">{{ row.span.service }}</span>
                      </span>
                      <span
                        class="tw-dur"
                        :class="{ bad: row.span.statusCode === 'ERROR' }"
                      >{{ formatDuration(row.durationMs) }}</span>
                      <span class="tw-track">
                        <span
                          v-for="f in AXIS_TICKS"
                          :key="f"
                          class="tw-gl"
                          :style="{ left: f * 100 + '%' }"
                        />
                        <span
                          class="tw-bar"
                          :class="row.kind"
                          :style="{ left: (row.offset / (selectedTrace.total || 1)) * 100 + '%', width: Math.max((row.durationMs / (selectedTrace.total || 1)) * 100, 0.4) + '%' }"
                        />
                      </span>
                    </button>
                  </template>
                </div>
              </div>
            </div>

            <div
              class="tw-vresize-handle"
              role="separator"
              aria-orientation="horizontal"
              aria-label="Resize span list"
              :aria-valuenow="ui.spanListHeight"
              :aria-valuemin="SPAN_LIST_HEIGHT_MIN"
              :aria-valuemax="SPAN_LIST_HEIGHT_MAX"
              :class="{ active: resizingSpanList }"
              tabindex="0"
              @mousedown="startSpanListResize"
              @keydown.up="adjustSpanListHeight(-SPAN_LIST_HEIGHT_STEP)"
              @keydown.down="adjustSpanListHeight(SPAN_LIST_HEIGHT_STEP)"
            >
              <span class="tw-grip tw-grip-h">
                <span /><span /><span />
              </span>
            </div>

            <div class="tw-panel tw-panel-details">
              <div class="tw-panel-head">
                <span class="tw-panel-title">Span details</span>
              </div>
              <div class="tw-panel-scroll">
                <template v-if="selectedSpan">
                  <div class="tw-det-head">
                    <SubjectPath :subject="selectedSpan.subject" />
                    <span class="tw-dur">{{ formatDuration(selectedSpan.durationMs) }}</span>
                    <span
                      class="tw-acct"
                      :class="selectedSpanAccount.toLowerCase()"
                    >{{ selectedSpanAccount }}</span>
                  </div>
                  <div
                    v-if="selectedSpan.error"
                    class="tw-errbox"
                  >
                    {{ selectedSpan.error }}
                  </div>

                  <div class="tw-rr-grid">
              <div class="tw-rr-seam" />

              <div class="tw-rr-cap q">
                <span class="tw-arrow">→</span> Request
              </div>
              <div class="tw-rr-cap a">
                <span class="tw-arrow resp">←</span> Response
              </div>

              <div class="tw-rr-cell q">
                <div class="sect-label">
                  identity
                </div>
                <span
                  class="tw-who-id"
                  :class="{ unknown: !requestedBy }"
                >{{ requestedBy || requestedByEmptyLabel }}</span>
              </div>
              <div class="tw-rr-cell a">
                <div class="sect-label">
                  identity
                </div>
                <span
                  class="tw-who-id"
                  :class="{ unknown: !respondedBy }"
                >{{ respondedBy || '— (span not yet finished)' }}</span>
              </div>

              <div class="tw-rr-sep" />

              <div class="tw-rr-cell q">
                <div class="sect-label">
                  headers <span
                    v-if="headerSplit.req.length"
                    class="count"
                  >({{ headerSplit.req.length }})</span>
                </div>
                <div
                  v-if="headerSplit.req.length"
                  class="kv"
                >
                  <div
                    v-for="h in headerSplit.req"
                    :key="h.k"
                    class="row"
                  >
                    <span class="k">{{ h.k }}</span><span class="v">{{ h.v }}</span>
                  </div>
                </div>
                <span
                  v-else
                  class="lab-muted no-headers"
                >none</span>
                <template v-if="attributeSplit.req.length">
                  <div
                    class="sect-label"
                    style="margin-top: 8px"
                  >
                    attributes
                  </div>
                  <div class="kv">
                    <div
                      v-for="a in attributeSplit.req"
                      :key="a.k"
                      class="row"
                    >
                      <span class="k">{{ a.k }}</span><span class="v">{{ a.v }}</span>
                    </div>
                  </div>
                </template>
              </div>
              <div class="tw-rr-cell a">
                <div class="sect-label">
                  headers <span
                    v-if="headerSplit.resp.length"
                    class="count"
                  >({{ headerSplit.resp.length }})</span>
                </div>
                <div
                  v-if="headerSplit.resp.length"
                  class="kv"
                >
                  <div
                    v-for="h in headerSplit.resp"
                    :key="h.k"
                    class="row"
                  >
                    <span class="k">{{ h.k }}</span><span
                      class="v"
                      :class="{ errv: h.k.startsWith('Nats-Service-Error') }"
                    >{{ h.v }}</span>
                  </div>
                </div>
                <span
                  v-else
                  class="lab-muted no-headers"
                >none</span>
                <template v-if="attributeSplit.resp.length">
                  <div
                    class="sect-label"
                    style="margin-top: 8px"
                  >
                    attributes
                  </div>
                  <div class="kv">
                    <div
                      v-for="a in attributeSplit.resp"
                      :key="a.k"
                      class="row"
                    >
                      <span class="k">{{ a.k }}</span><span class="v">{{ a.v }}</span>
                    </div>
                  </div>
                </template>
              </div>

              <div class="tw-rr-sep" />

              <div class="tw-rr-cell q">
                <div class="sect-label">
                  body
                </div>
                <pre
                  class="json"
                  v-html="highlightJson(selectedSpan.requestPayload) || '—'"
                />
              </div>
              <div class="tw-rr-cell a">
                <div class="sect-label">
                  body
                </div>
                <pre
                  class="json"
                  v-html="highlightJson(selectedSpan.payload) || '—'"
                />
              </div>
            </div>
                </template>
                <span
                  v-else
                  class="lab-muted tw-empty"
                >Select a span above to inspect its request/response detail.</span>
              </div>
            </div>
          </div>
        </template>
        <span
          v-else
          class="lab-muted tw-empty"
        >Select a trace to inspect its waterfall.</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.trace-waterfall {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.tw-toolbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  margin: 0.5rem 0 0;
}
.search-box {
  flex: 1;
  min-width: 160px;
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--lab-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.search-box input {
  flex: 1;
  min-width: 0;
  background: none;
  border: none;
  outline: none;
  color: var(--p-text-color);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  line-height: 16px;
  padding: 1px 8px;
  border-radius: 3px;
  border: 1px solid var(--lab-panel-border);
  color: var(--p-text-muted-color);
  cursor: pointer;
  background: transparent;
  font-family: inherit;
}
.chip.on {
  border-color: var(--lab-accent);
  color: var(--lab-accent);
  background: rgba(0, 111, 255, 0.1);
}
.chip.err.on {
  border-color: #e5484d;
  color: #e5484d;
  background: rgba(229, 72, 77, 0.1);
}
.pause-btn {
  border: 1px solid var(--lab-panel-border);
  background: transparent;
  color: var(--p-text-muted-color);
  border-radius: 3px;
  font-size: 11px;
  padding: 1px 9px;
  cursor: pointer;
  font-family: inherit;
}
.pause-btn:hover {
  color: var(--p-text-color);
  border-color: var(--p-text-disabled-color);
}

/* ── rest/nats transport filter — a joined three-way segmented control, not
   two independent toggle chips like errors/slow above: rest/nats is one
   axis with three states (all/rest/nats), and two AND-combinable booleans
   could land on "both on," which just means "all" again with no way to
   tell from the chips alone. */
.kind-group {
  display: inline-flex;
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  overflow: hidden;
}
.kind-group button {
  border: none;
  border-right: 1px solid var(--lab-panel-border);
  background: transparent;
  color: var(--p-text-muted-color);
  font: inherit;
  font-size: 11px;
  line-height: 16px;
  padding: 1px 9px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.kind-group button:last-child {
  border-right: none;
}
.kind-group button i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.kind-group button[data-k='rest'] i {
  background: #a78bfa;
}
.kind-group button[data-k='nats'] i {
  background: #2dd4bf;
}
.kind-group button.on[data-k='rest'] {
  background: rgba(167, 139, 250, 0.14);
  color: #a78bfa;
}
.kind-group button.on[data-k='nats'] {
  background: rgba(45, 212, 191, 0.12);
  color: #2dd4bf;
}
.kind-group button.on[data-k='all'] {
  background: rgba(255, 255, 255, 0.06);
  color: var(--p-text-color);
}

/* ── pulse strip (Phase 28p) — request/error/latency histograms over the
   currently displayed (filtered) trace window; sits directly under the
   toolbar so toggling errors/slow/rest-nats up there reshapes this strip
   and the trace list together, same data, two views. All colors reuse
   existing tokens — accent for neutral volume/latency, the same err red
   already used by .tw-dot.err/.chip.err, no new palette. */
.pulse-strip {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 1px;
  background: var(--lab-panel-border);
  border-bottom: 1px solid var(--lab-panel-border);
  flex: none;
}
.pulse-card {
  background: var(--lab-panel-bg);
  padding: 10px 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.pulse-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}
.pulse-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
}
.pulse-window {
  font-size: 10px;
  color: var(--p-text-disabled-color);
}
.pulse-value-row {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.pulse-value {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 20px;
  font-weight: 600;
  line-height: 1;
}
.pulse-value.accent {
  color: var(--lab-accent);
}
.pulse-value.err {
  color: #e5484d;
}
.pulse-unit {
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.pulse-chart {
  height: 34px;
  display: flex;
  align-items: flex-end;
  gap: 2px;
}
.pulse-bar {
  flex: 1;
  border-radius: 1.5px 1.5px 0 0;
  background: var(--lab-accent);
  opacity: 0.85;
  min-height: 2px;
}
.pulse-bar.err {
  background: #e5484d;
}
.pulse-bar.err.empty {
  opacity: 0.18;
}
.pulse-bar.now {
  opacity: 1;
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.15);
}
.pulse-line-wrap {
  height: 34px;
}
.pulse-line-wrap svg {
  display: block;
  width: 100%;
  height: 100%;
  overflow: visible;
}
.pulse-area {
  fill: var(--lab-accent);
  opacity: 0.12;
}
.pulse-line {
  fill: none;
  stroke: var(--lab-accent);
  stroke-width: 1.5;
  stroke-linejoin: round;
  stroke-linecap: round;
}

/* ── split body: trace rail | waterfall ── */
.tw-split {
  flex: 1;
  min-height: 0;
  display: grid;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  overflow: hidden;
}
.tw-list {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.tw-resize-handle {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: col-resize;
  background: transparent;
  touch-action: none;
}
.tw-resize-handle::after {
  content: '';
  position: absolute;
  inset: 0 auto 0 50%;
  width: 2px;
  transform: translateX(-50%);
  background: var(--lab-panel-border);
}
.tw-resize-handle:hover::after,
.tw-resize-handle.active::after {
  width: 3px;
  background: var(--p-primary-color, #006fff);
}
.tw-resize-handle:focus-visible {
  outline: 2px solid var(--p-primary-color, #006fff);
  outline-offset: -2px;
}
/* grip: a stack of dots centered on the handle, at rest a visible affordance
   (not just an on-hover reveal) so the divider reads as draggable. */
.tw-grip {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 5px 3px;
  border-radius: 3px;
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  pointer-events: none;
}
.tw-grip span {
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: var(--p-text-disabled-color, #8a8a8a);
}
.tw-resize-handle:hover .tw-grip,
.tw-resize-handle.active .tw-grip {
  border-color: var(--p-primary-color, #006fff);
}
.tw-resize-handle:hover .tw-grip span,
.tw-resize-handle.active .tw-grip span {
  background: var(--p-primary-color, #006fff);
}
.tw-list-head {
  flex: none;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-weight: 600;
  padding: 5px 10px;
  background: rgba(0, 111, 255, 0.06);
  border-bottom: 1px solid var(--lab-panel-border);
  display: flex;
  justify-content: space-between;
}
.tw-list-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.tw-empty {
  padding: 10px;
  font-size: 12px;
}
.tw-trace {
  display: grid;
  grid-template-columns: 7px minmax(0, 1fr) auto;
  gap: 8px;
  align-items: start;
  width: 100%;
  text-align: left;
  padding: 5px 10px;
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--lab-panel-border);
  color: inherit;
  font: inherit;
  cursor: pointer;
}
.tw-trace:hover {
  background: rgba(255, 255, 255, 0.03);
}
.tw-trace[aria-current='true'] {
  background: rgba(0, 111, 255, 0.08);
}
.tw-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  margin-top: 5px;
}
.tw-dot.ok {
  background: #2fbf71;
}
.tw-dot.err {
  background: #e5484d;
}
.tw-trace-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.tw-trace-subject {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
}
/* Same left-border-plus-tint vocabulary as .tw-acct's PLATFORM/TENANT tags
   below — this reads as one more tag in this row's existing vocabulary,
   not a new UI idiom. Colors deliberately avoid --lab-accent (already
   means "selected") and the TENANT tag's amber, so a kind tag is never
   mistaken for an account tag. */
.kind-tag {
  flex: none;
  display: inline-flex;
  align-items: center;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  padding: 1px 5px 1px 4px;
  border-radius: 2px;
}
.kind-tag.rest {
  color: #a78bfa;
  background: rgba(167, 139, 250, 0.14);
  border-left: 2px solid #a78bfa;
}
.kind-tag.nats {
  color: #2dd4bf;
  background: rgba(45, 212, 191, 0.12);
  border-left: 2px solid #2dd4bf;
}
.tw-trace-meta {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  color: var(--p-text-disabled-color);
  display: flex;
  gap: 7px;
  flex-wrap: wrap;
}
.tw-trace-right {
  text-align: right;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.tw-trace-dur {
  color: var(--p-text-color);
  display: block;
}
.tw-trace-dur.err {
  color: #e5484d;
}
.tw-trace-spans {
  color: var(--p-text-disabled-color);
  font-size: 10px;
}

/* ── waterfall ── */
.tw-wf {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.tw-wf-head {
  flex: none;
  padding: 8px 12px;
  border-bottom: 1px solid var(--lab-panel-border);
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 6px 18px;
}
.tw-wf-tid {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  color: var(--p-text-disabled-color);
  flex-basis: 100%;
}
.tw-wf-tid b {
  color: var(--p-text-muted-color);
  font-weight: 400;
}
.tw-stat {
  display: flex;
  flex-direction: column;
}
.tw-stat .k {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--p-text-disabled-color);
}
.tw-stat .v {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 14px;
  font-variant-numeric: tabular-nums;
  color: var(--p-text-color);
}
.tw-stat .v.bad {
  color: #e5484d;
}
.tw-stat .v.evtl {
  color: #e2b86b;
}
.tw-stat .v small {
  font-size: 10px;
  color: var(--p-text-disabled-color);
}

/* ── Span list / Span details grouping (Phase 28j) — one continuous
   waterfall/detail column split into two labeled cards, resizable the same
   way the trace rail is (px-in-the-ui-store + draggable handle), instead of
   the two sections just stacking and scrolling together as one blob. */
.tw-wf-body {
  flex: 1;
  min-height: 0;
  display: grid;
}
.tw-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.tw-panel-head {
  flex: none;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-weight: 600;
  padding: 5px 12px;
  background: rgba(0, 111, 255, 0.06);
  border-bottom: 1px solid var(--lab-panel-border);
  display: flex;
  justify-content: space-between;
  align-items: center;
}
/* Subtle highlight — same accent-as-card-title convention as AccountsPanel's
   "ACCOUNTS" heading, so these two sections read as titled cards rather than
   blending into the surrounding muted chrome. */
.tw-panel-title {
  color: var(--lab-accent);
}
.tw-panel-count {
  font-weight: 400;
  text-transform: none;
  letter-spacing: normal;
  color: var(--p-text-disabled-color);
}
.tw-panel-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.tw-vresize-handle {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: row-resize;
  background: transparent;
  touch-action: none;
}
.tw-vresize-handle::after {
  content: '';
  position: absolute;
  inset: 50% 0 auto 0;
  height: 2px;
  transform: translateY(-50%);
  background: var(--lab-panel-border);
}
.tw-vresize-handle:hover::after,
.tw-vresize-handle.active::after {
  height: 3px;
  background: var(--p-primary-color, #006fff);
}
.tw-vresize-handle:focus-visible {
  outline: 2px solid var(--p-primary-color, #006fff);
  outline-offset: -2px;
}
.tw-grip-h {
  flex-direction: row;
}
.tw-vresize-handle:hover .tw-grip-h,
.tw-vresize-handle.active .tw-grip-h {
  border-color: var(--p-primary-color, #006fff);
}
.tw-vresize-handle:hover .tw-grip-h span,
.tw-vresize-handle.active .tw-grip-h span {
  background: var(--p-primary-color, #006fff);
}

.tw-grid {
  display: grid;
  grid-template-columns: 78px minmax(200px, 1.75fr) 56px minmax(110px, 1.4fr);
  gap: 8px;
  align-items: center;
  padding: 0 12px;
}
.tw-axis {
  flex: none;
  height: 22px;
  border-bottom: 1px solid var(--lab-panel-border);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  color: var(--p-text-disabled-color);
}
.tw-ticks {
  position: relative;
  height: 100%;
}
.tw-tick {
  position: absolute;
  top: 5px;
  transform: translateX(-50%);
  white-space: nowrap;
}
.tw-tick:first-child {
  transform: none;
}
.tw-tick:last-child {
  transform: translateX(-100%);
}

.tw-rows {
  flex: none;
  display: flex;
  flex-direction: column;
}
.tw-row {
  width: 100%;
  text-align: left;
  font: inherit;
  color: inherit;
  background: transparent;
  border: none;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  height: 26px;
  cursor: pointer;
}
.tw-row:hover {
  background: rgba(255, 255, 255, 0.03);
}
.tw-row[aria-current='true'] {
  background: rgba(0, 111, 255, 0.08);
}

.tw-acct {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 9px;
  letter-spacing: 0.04em;
  color: var(--p-text-disabled-color);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  border-left: 2px solid var(--lab-panel-border);
  padding-left: 6px;
}
.tw-acct.platform {
  border-left-color: rgba(0, 111, 255, 0.65);
}
.tw-acct.tenant {
  border-left-color: rgba(226, 184, 107, 0.55);
}
.tw-acct.cross {
  color: var(--lab-accent);
  font-weight: 600;
}

.tw-nm {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.tw-rail {
  flex: none;
  border-left: 1px solid var(--lab-panel-border);
  align-self: stretch;
  margin-left: 5px;
}
.tw-txt {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tw-svc {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 9px;
  color: var(--p-text-disabled-color);
  flex: none;
}

.tw-dur {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  text-align: right;
  color: var(--p-text-muted-color);
}
.tw-dur.bad {
  color: #e5484d;
}

.tw-track {
  position: relative;
  height: 100%;
}
.tw-gl {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: rgba(255, 255, 255, 0.04);
}
.tw-bar {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  height: 8px;
  border-radius: 2px;
  min-width: 2px;
}
.tw-bar.sync {
  background: var(--lab-accent);
}
.tw-bar.evtl {
  background: rgba(226, 184, 107, 0.26);
  border: 1px solid #e2b86b;
}
.tw-bar.bad {
  background: #e5484d;
}

.tw-ack {
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 12px;
  background: rgba(226, 184, 107, 0.08);
  border-top: 1px dashed rgba(226, 184, 107, 0.55);
  border-bottom: 1px dashed rgba(226, 184, 107, 0.55);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
}
.tw-ack .l {
  color: #e2b86b;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.tw-ack .r {
  color: var(--p-text-disabled-color);
  margin-left: auto;
  text-align: right;
}

/* ── span detail (lives inside .tw-panel-details, Phase 28j) ── */
.tw-det-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 7px 12px;
  border-bottom: 1px solid var(--lab-panel-border);
}
.tw-who-id {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 13px;
  color: var(--p-text-color);
  overflow-wrap: anywhere;
}
.tw-who-id.unknown {
  color: var(--p-text-disabled-color);
  font-style: italic;
  font-size: 12px;
}
.tw-arrow {
  color: var(--p-primary-color, #006fff);
}
.tw-arrow.resp {
  color: #7fd8a4;
}
/* tw-rr-grid — Phase 28i: one continuous request|response split, not the old
   two-part layout (a horizontal identity strip, then a bordered two-column
   block for headers, then two full-width stacked bodies) whose direction
   flipped partway down. Request is always the left column, response always
   the right, joined by one seam (.tw-rr-seam, grid-row 1/-1) that runs the
   full height of the pane — so the axis only needs naming once, in the
   column captions, and every section below drops its "Request "/"Response "
   prefix (sect-label just says "headers", "attributes", "body"). Rows are
   auto-placed in source order: captions, identity, a full-width separator,
   headers/attributes, another separator, bodies — each pair of `.q`/`.a`
   cells lands in the same auto-placed row so they stay aligned regardless of
   which side is taller. */
.tw-rr-grid {
  position: relative;
  display: grid;
  grid-template-columns: 1fr 1fr;
}
.tw-rr-seam {
  grid-column: 1 / -1;
  grid-row: 1 / -1;
  width: 1px;
  height: 100%;
  background: var(--lab-panel-border);
  justify-self: center;
}
.tw-rr-cap {
  padding: 7px 12px;
  border-bottom: 1px solid var(--lab-panel-border);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  display: flex;
  align-items: center;
  gap: 6px;
}
.tw-rr-cell {
  padding: 9px 12px 12px;
  min-width: 0;
}
.tw-rr-sep {
  grid-column: 1 / -1;
  height: 1px;
  background: var(--lab-panel-border);
}
.sect-label {
  font-size: 10px;
  font-weight: 600;
  color: var(--p-text-muted-color);
  letter-spacing: 0.04em;
  margin-bottom: 2px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.sect-label .count {
  color: var(--p-text-disabled-color);
  font-weight: 400;
}
.no-headers {
  font-size: 11px;
}
.kv {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  overflow: hidden;
}
.kv .row {
  display: grid;
  grid-template-columns: 170px 1fr;
}
.kv .row:nth-child(odd) {
  background: rgba(255, 255, 255, 0.02);
}
.kv .k {
  color: var(--p-text-muted-color);
  padding: 1px 8px;
  border-right: 1px solid var(--lab-panel-border);
  overflow-wrap: anywhere;
}
.kv .v {
  color: var(--p-text-color);
  padding: 1px 8px;
  overflow-wrap: anywhere;
}
.kv .v.errv {
  color: #e5484d;
}
.json {
  margin: 0;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  line-height: 17px;
  background: var(--lab-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  padding: 6px 8px;
  overflow: auto;
  white-space: pre;
  color: var(--p-text-muted-color);
}
.json :deep(.jk) {
  color: #7fb3ff;
}
.json :deep(.js) {
  color: #7fd8a4;
}
.json :deep(.jn) {
  color: #e2b86b;
}
.json :deep(.jp) {
  color: var(--p-text-disabled-color);
}
.tw-errbox {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  color: #e5484d;
  background: rgba(229, 72, 77, 0.08);
  border: 1px solid rgba(229, 72, 77, 0.4);
  border-radius: 3px;
  padding: 4px 8px;
  margin: 9px 12px 0;
  overflow-wrap: anywhere;
}
</style>
