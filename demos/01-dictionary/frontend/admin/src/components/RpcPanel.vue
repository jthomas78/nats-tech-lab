<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'

import SubjectPath from './SubjectPath.vue'
import { rpcWatchUrl } from '../api'
import { useTenantStore } from '../stores/tenant'

// Request/Reply panel v2 (Main-POC-Plan.md Phase 17b) — rebuilt to match the
// approved static reference, frontend/admin/request-reply-reference.html.
// Traffic: obs.rpc.* (backend-to-backend, Phase 12.10) plus obs.api.*
// (browser-to-service, Phase 16). A connection replays up to the last 10
// minutes of retained obs.rpc.* traffic from RPCTRACE before switching to
// live delivery (BR-D29, ARCHITECTURE-COMMUNICATIONS.md §6); obs.api.* is
// live-only — it publishes inside the active tenant's NATS account, which
// RPCTRACE (DEFAULT account) doesn't capture. The backend sends replayed
// and live events identically over the same SSE stream, so this component
// doesn't distinguish them; rows just appear in the order the server
// emitted them. Request and reply arrive as two separate obs.rpc.*/
// obs.api.* messages sharing a correlationId (the request's reply-to
// inbox); this pairs them into one row per call instead of two unrelated
// list entries.
//
// The server pins its obs.api.> subscription to the tenant that was active
// when the SSE connection opened (rest/sse.go watchRPCObs), so a tenant
// switch reconnects — same pattern as the KV watches in stores/*.js.
//
// Headers/timestamp/payloadBytes (BR-D36/BR-026, Phase 17a) are optional on
// the wire — an event retained before that change, or ever missing them,
// must render with "—" placeholders rather than break.

// Keyed by correlationId so a reply can update its request's row in place
// without an index shifting under a prepend (order tracked separately).
const rowsById = reactive({})
const order = ref([]) // correlationIds, newest first
const MAX_ROWS = 500

// api.*/rpc.* subjects have fixed 6-token arity — family, context, service,
// entity, action, version (ARCHITECTURE-COMMUNICATIONS.md §2 decision 4) —
// so a token's position tells you what it means without parsing the value.
// Facet filtering below is purely positional for that reason.
const FACET_POSITION_NAMES = ['family', 'context', 'service', 'entity', 'action', 'version']

function upsertRow(event) {
  let row = rowsById[event.correlationId]
  if (!row) {
    row = {
      correlationId: event.correlationId,
      subject: event.subject,
      family: event.subject?.split('.')[0] || '',
      requestPayload: null,
      requestHeaders: null,
      requestTimestamp: null, // ms epoch, server-side (BR-D36/BR-026); null until a real timestamp arrives
      requestBytes: null,
      replyPayload: null,
      replyHeaders: null,
      replyTimestamp: null,
      replyBytes: null,
      error: '',
      status: 'pending',
      time: Date.now(), // arrival-clock fallback for display when no server timestamp exists yet
    }
    rowsById[event.correlationId] = row
    order.value = [event.correlationId, ...order.value]
    if (order.value.length > MAX_ROWS) {
      for (const id of order.value.slice(MAX_ROWS)) delete rowsById[id]
      order.value = order.value.slice(0, MAX_ROWS)
    }
  }
  const serverTs = event.timestamp ? new Date(event.timestamp).getTime() : null
  if (event.direction === 'request') {
    row.requestPayload = event.payload
    row.requestHeaders = event.headers || null
    row.requestTimestamp = serverTs
    row.requestBytes = typeof event.payloadBytes === 'number' ? event.payloadBytes : null
  } else {
    row.replyPayload = event.payload
    row.replyHeaders = event.headers || null
    row.replyTimestamp = serverTs
    row.replyBytes = typeof event.payloadBytes === 'number' ? event.payloadBytes : null
    row.error = event.error || ''
    row.status = event.error ? 'error' : 'ok'
  }
}

const connected = ref(false)
let source = null

function connect() {
  source = new EventSource(rpcWatchUrl())
  source.onopen = () => { connected.value = true }
  source.onerror = () => { connected.value = false }
  source.onmessage = (e) => upsertRow(JSON.parse(e.data))
}

function disconnect() {
  source?.close()
  source = null
  connected.value = false
}

onMounted(connect)
onUnmounted(disconnect)

// Reconnect on tenant switch so the server re-pins its obs.api.>
// subscription to the new tenant's connection. Rows are kept: the obs.rpc.*
// replay would re-deliver most of them anyway (rowsById upserts by
// correlationId, so replayed duplicates collapse in place).
const tenantStore = useTenantStore()
watch(() => tenantStore.tenant, (next, prev) => {
  if (prev === null || next === prev) return // initial load, not a switch
  disconnect()
  connect()
})

// ── Pause — freezes which rows are VISIBLE, not the SSE connection itself.
// Live events keep landing in rowsById/order underneath (so a row already
// on screen still updates in place, e.g. a pending call resolving), but the
// visible list stops growing until resumed.
const paused = ref(false)
const frozenOrder = ref([])
function togglePause() {
  if (!paused.value) frozenOrder.value = [...order.value]
  paused.value = !paused.value
}
const displayedOrder = computed(() => (paused.value ? frozenOrder.value : order.value))

// ── Filtering ────────────────────────────────────────────────────────────
const searchText = ref('')
const familyOn = reactive({ rpc: true, api: true })
const statusOn = reactive({ ok: true, error: true, pending: true })
const facets = ref([]) // [{ index, name, value }] — at most one per position

function toggleFamily(fam) {
  familyOn[fam] = !familyOn[fam]
}
function toggleStatus(st) {
  statusOn[st] = !statusOn[st]
}
function removeFacet(index) {
  facets.value = facets.value.filter((f) => f.index !== index)
}
function onTokenClick({ index, text }) {
  if (index === 0) {
    // The family token (rpc/api) is already surfaced as its own toggle —
    // clicking it flips that chip instead of adding a redundant facet.
    if (text === 'rpc' || text === 'api') familyOn[text] = true
    return
  }
  const existing = facets.value.find((f) => f.index === index)
  if (existing && existing.value === text) {
    removeFacet(index) // clicking the same active facet again clears it
    return
  }
  facets.value = [...facets.value.filter((f) => f.index !== index), { index, name: FACET_POSITION_NAMES[index] || `pos${index}`, value: text }]
}

function rowMatchesFilters(row) {
  if (!familyOn[row.family]) return false
  if (!statusOn[row.status]) return false
  if (searchText.value && !row.subject.toLowerCase().includes(searchText.value.toLowerCase())) return false
  if (facets.value.length) {
    const tokens = row.subject.split('.')
    for (const f of facets.value) {
      if (tokens[f.index] !== f.value) return false
    }
  }
  return true
}

const rows = computed(() => displayedOrder.value.map((id) => rowsById[id]).filter((r) => r && rowMatchesFilters(r)))

// ── Selection / detail pane ─────────────────────────────────────────────
const selectedId = ref(null)
const selectedRow = computed(() => (selectedId.value ? rowsById[selectedId.value] : null))
function selectRow(row) {
  // A click-drag to select row text (e.g. to copy the subject or a value)
  // still fires a native 'click' on mouseup. Without this guard that click
  // toggles the row's selection off from underneath the drag, leaving the
  // detail panel closed even though the user only meant to select text.
  const selection = window.getSelection()
  if (selection && selection.toString().length > 0) return
  selectedId.value = selectedId.value === row.correlationId ? null : row.correlationId
}
function closeDetail() {
  selectedId.value = null
}

// ── Formatting helpers ──────────────────────────────────────────────────
function statusSeverity(status) {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'danger'
  return 'warn'
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

function rowLatency(row) {
  if (row.requestTimestamp == null || row.replyTimestamp == null) return null
  return row.replyTimestamp - row.requestTimestamp
}
function formatLatency(row) {
  const ms = rowLatency(row)
  return ms == null ? '—' : `${ms} ms`
}

function formatBytes(n) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  return `${(n / 1024).toFixed(1)} KB`
}
function formatSizes(row) {
  return `${formatBytes(row.requestBytes)} ⁄ ${formatBytes(row.replyBytes)}`
}

function headerCount(headers) {
  return headers ? Object.keys(headers).length : 0
}
function headerRows(headers) {
  if (!headers) return []
  return Object.entries(headers).map(([k, v]) => ({ k, v: Array.isArray(v) ? v.join(', ') : String(v) }))
}

// Pretty-printed, syntax-tinted JSON for the detail panes. Escapes HTML
// first, then tints via regex over the already-escaped string — the
// standard safe pattern (tinting never introduces raw user content into the
// DOM, only wraps already-escaped text in <span>).
function escapeHtml(str) {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
function highlightJson(value) {
  if (value === null || value === undefined) return ''
  const json = escapeHtml(JSON.stringify(value, null, 2))
  return json.replace(
    /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g,
    (match) => {
      let cls = 'jn'
      if (/^"/.test(match)) {
        cls = /:$/.test(match) ? 'jk' : 'js'
      } else if (/true|false|null/.test(match)) {
        cls = 'jp'
      }
      return `<span class="${cls}">${match}</span>`
    },
  )
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    // Clipboard access can be denied (permissions, insecure context) — a
    // failed copy isn't worth surfacing an error for in a debug panel.
  }
}
function copyPayload(payload) {
  if (!payload) return
  copyText(typeof payload === 'string' ? payload : JSON.stringify(payload, null, 2))
}
function copyHeaders(headers) {
  if (!headers) return
  copyText(JSON.stringify(headers, null, 2))
}
</script>

<template>
  <div class="rpc-panel">
    <div class="rpc-toolbar">
      <Tag :severity="connected ? 'success' : 'danger'" :value="connected ? 'live' : 'disconnected'" />
      <span class="search-box">
        <i class="pi pi-search" />
        <input v-model="searchText" type="text" placeholder="filter subjects — or click any subject token below" />
      </span>
      <button
        v-for="fam in ['rpc', 'api']"
        :key="fam"
        type="button"
        class="chip"
        :class="{ on: familyOn[fam] }"
        @click="toggleFamily(fam)"
      >{{ fam }}</button>
      <button
        v-for="st in ['ok', 'error', 'pending']"
        :key="st"
        type="button"
        class="chip"
        :class="{ on: statusOn[st], err: st === 'error' }"
        @click="toggleStatus(st)"
      >{{ st }}</button>
      <button
        v-for="f in facets"
        :key="f.index"
        type="button"
        class="chip facet"
        @click="removeFacet(f.index)"
      ><span class="facet-key">{{ f.name }}:</span>{{ f.value }}<span class="facet-x">✕</span></button>
      <button type="button" class="pause-btn" @click="togglePause">{{ paused ? '▶ resume' : '⏸ pause' }}</button>
    </div>

    <DataTable
      :value="rows"
      size="small"
      scrollable
      scroll-height="flex"
      class="rpc-table"
      resizableColumns
      columnResizeMode="expand"
      data-key="correlationId"
      selectionMode="single"
      :metaKeySelection="false"
      @row-click="selectRow($event.data)"
    >
      <template #empty>
        <span class="lab-muted">Waiting for rpc.*/api.* traffic — trigger a shipping-service → refdata-service item lookup, or a Sea Freight Flow action, to see it here.</span>
      </template>
      <Column header="Status" style="width:80px">
        <template #body="{ data }">
          <Tag :severity="statusSeverity(data.status)" :value="data.status" />
        </template>
      </Column>
      <Column header="Fam" style="width:50px">
        <template #body="{ data }"><span class="fam-badge" :class="data.family">{{ data.family }}</span></template>
      </Column>
      <Column header="Subject">
        <template #body="{ data }">
          <SubjectPath :subject="data.subject" clickable @token-click="onTokenClick" />
        </template>
      </Column>
      <Column header="Time" style="width:100px;font-variant-numeric:tabular-nums">
        <template #body="{ data }">{{ formatTimeMs(data.requestTimestamp ?? data.time) }}</template>
      </Column>
      <Column header="Latency" style="width:70px" bodyClass="num-cell">
        <template #body="{ data }">{{ formatLatency(data) }}</template>
      </Column>
      <Column header="Size" style="width:110px" bodyClass="num-cell">
        <template #body="{ data }">{{ formatSizes(data) }}</template>
      </Column>
    </DataTable>

    <section v-if="selectedRow" class="detail">
      <div class="detail-head">
        <SubjectPath :subject="selectedRow.subject" clickable @token-click="onTokenClick" />
        <span class="meta">
          <Tag :severity="statusSeverity(selectedRow.status)" :value="selectedRow.status" />
          <span>latency <b>{{ formatLatency(selectedRow) }}</b></span>
          <span>corr <b :title="selectedRow.correlationId">{{ selectedRow.correlationId }}</b></span>
        </span>
        <span class="close" title="Close" @click="closeDetail">✕</span>
      </div>
      <div class="panes">
        <div class="pane">
          <div class="pane-title">
            <span class="dir req">→</span> Request
            <span class="pane-subtitle lab-muted">{{ formatBytes(selectedRow.requestBytes) }} · {{ formatTimeMs(selectedRow.requestTimestamp) }}</span>
          </div>
          <div class="pane-body">
            <div class="sect">
              <div class="sect-label">
                Headers <span class="count">({{ headerCount(selectedRow.requestHeaders) }})</span>
                <span v-if="headerCount(selectedRow.requestHeaders)" class="copy" @click="copyHeaders(selectedRow.requestHeaders)">copy</span>
              </div>
              <div v-if="headerCount(selectedRow.requestHeaders)" class="kv">
                <div class="row" v-for="h in headerRows(selectedRow.requestHeaders)" :key="h.k">
                  <span class="k">{{ h.k }}</span><span class="v">{{ h.v }}</span>
                </div>
              </div>
              <span v-else class="lab-muted no-headers">no headers</span>
            </div>
            <div class="sect">
              <div class="sect-label">Body <span class="copy" @click="copyPayload(selectedRow.requestPayload)">copy</span></div>
              <pre class="json" v-html="highlightJson(selectedRow.requestPayload) || '—'"></pre>
            </div>
          </div>
        </div>
        <div class="pane">
          <div class="pane-title">
            <span class="dir rep">←</span> Reply
            <span class="pane-subtitle lab-muted">{{ formatBytes(selectedRow.replyBytes) }} · {{ formatTimeMs(selectedRow.replyTimestamp) }}</span>
          </div>
          <div class="pane-body">
            <div v-if="selectedRow.error" class="err-banner">{{ selectedRow.error }}</div>
            <div class="sect">
              <div class="sect-label">
                Headers <span class="count">({{ headerCount(selectedRow.replyHeaders) }})</span>
                <span v-if="headerCount(selectedRow.replyHeaders)" class="copy" @click="copyHeaders(selectedRow.replyHeaders)">copy</span>
              </div>
              <div v-if="headerCount(selectedRow.replyHeaders)" class="kv">
                <div class="row" v-for="h in headerRows(selectedRow.replyHeaders)" :key="h.k">
                  <span class="k">{{ h.k }}</span><span class="v" :class="{ errv: h.k.startsWith('Nats-Service-Error') }">{{ h.v }}</span>
                </div>
              </div>
              <span v-else class="lab-muted no-headers">no headers</span>
            </div>
            <div class="sect">
              <div class="sect-label">Body <span class="copy" @click="copyPayload(selectedRow.replyPayload)">copy</span></div>
              <pre class="json" v-html="highlightJson(selectedRow.replyPayload) || '—'"></pre>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.rpc-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

/* ── filter bar ── */
.rpc-toolbar {
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
.chip.facet {
  border-color: rgba(0, 111, 255, 0.45);
  color: var(--p-text-color);
  background: rgba(0, 111, 255, 0.1);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
}
.facet-key {
  color: var(--p-text-disabled-color);
}
.facet-x {
  color: var(--p-text-disabled-color);
  margin-left: 2px;
}
.chip.facet:hover .facet-x {
  color: #e5484d;
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

/* ── table ── */
.rpc-table {
  flex: 1;
  min-height: 0;
}
.rpc-table :deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
.rpc-table :deep(.p-datatable-tbody > tr > td) {
  padding-top: 3px;
  padding-bottom: 3px;
}
.rpc-table :deep(.num-cell) {
  font-variant-numeric: tabular-nums;
  color: var(--p-text-muted-color);
}
.fam-badge {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  font-weight: 700;
  border-radius: 3px;
  padding: 0 5px;
  line-height: 15px;
  display: inline-block;
}
.fam-badge.rpc {
  color: #b18cff;
  background: rgba(148, 101, 255, 0.13);
}
.fam-badge.api {
  color: #4cc2ff;
  background: rgba(56, 178, 255, 0.12);
}

/* ── detail split ── */
.detail {
  flex: none;
  height: 46%;
  min-height: 220px;
  display: flex;
  flex-direction: column;
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
}
.detail-head {
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 10px;
  border-bottom: 1px solid var(--lab-panel-border);
}
.meta {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-size: 11px;
  color: var(--p-text-muted-color);
  font-variant-numeric: tabular-nums;
}
.meta b {
  color: var(--p-text-color);
  font-weight: 600;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  vertical-align: bottom;
}
.detail-head .close {
  margin-left: auto;
  color: var(--p-text-disabled-color);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 2px 4px;
  border-radius: 3px;
}
.detail-head .close:hover {
  color: var(--p-text-color);
  background: rgba(255, 255, 255, 0.05);
}
.panes {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
}
.pane {
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.pane + .pane {
  border-left: 1px solid var(--lab-panel-border);
}
.pane-title {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
  padding: 5px 10px 3px;
}
.pane-subtitle {
  text-transform: none;
  font-weight: 400;
  letter-spacing: normal;
}
.dir {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 9px;
}
.dir.req {
  background: rgba(56, 178, 255, 0.15);
  color: #4cc2ff;
}
.dir.rep {
  background: rgba(47, 191, 113, 0.14);
  color: #2fbf71;
}
.pane-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 0 10px 8px;
}
.sect {
  margin-top: 6px;
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
.copy {
  margin-left: auto;
  font-size: 10px;
  color: var(--p-text-disabled-color);
  cursor: pointer;
  border: 1px solid transparent;
  border-radius: 3px;
  padding: 0 5px;
}
.copy:hover {
  color: var(--p-text-color);
  border-color: var(--lab-panel-border);
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
.err-banner {
  margin-top: 6px;
  font-size: 11px;
  color: #e5484d;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  border: 1px solid rgba(229, 72, 77, 0.4);
  background: rgba(229, 72, 77, 0.08);
  border-radius: 3px;
  padding: 4px 8px;
}
</style>
