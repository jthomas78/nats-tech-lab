<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import { computed, reactive, ref } from 'vue'

import PulsePanel from './PulsePanel.vue'
import SubjectPath from './SubjectPath.vue'
import TraceWaterfall from './TraceWaterfall.vue'
import { highlightJson } from '../jsonHighlight.js'
import { useTraceFeed } from '../nats/useTraceFeed.js'
import { useUiStore } from '../stores/ui.js'

const ui = useUiStore()

// [messages] tab retired obs.rpc.*/obs.api.* (Phase 28g) — see BR-026's
// Phase 28g amendment in BUSINESS_RULES-SHIPPING.md. Both channels were
// already dead before this retirement even started: Phase 28a-28e replaced
// every adapter's publishObs call with a natstrace span, so this view had
// shown nothing for any of the five services since then — retiring it is
// deleting a live pipe carrying nothing, not migrating working traffic.
//
// It now derives from the exact same obs.trace.* data [traces]
// (TraceWaterfall.vue) reads — the trace-request-reply KV bucket, bootstrap fetch plus
// notify._platform.kv.trace-request-reply.> live subscribe on the PLATFORM connection —
// flattened to one row per SPAN instead of one row per TRACE. This is the
// flat "is anything arriving on this subject" view BR-035 always intended
// this tab to keep being, just fed by the new pipe instead of the retired
// one. Bootstrap/subscribe itself lives in useTraceFeed.js (shared with
// PulsePanel.vue/TraceWaterfall.vue — an architecture review replaced three
// drifted copies of the same adapter with this one seam); the flattening
// below (upsertSpan/order/MAX_ROWS) stays local to this tab via
// useTraceFeed's onUpsert hook, since grouped-by-trace vs. flat-by-span
// insertion order can't be reconstructed from a plain watch() on the
// composable's Map.
//
// One real, unavoidable difference from the old paired Request/Reply view:
// a natstrace span carries only the REPLY side (BR-037's one-span-per-call
// design — natstrace.go's Span.finish always publishes Direction: "reply";
// there is no wire signal for the request side at all, confirmed against
// the live code before this migration was written). The detail pane below
// is therefore a single Body/Headers section, not the old two-pane
// Request | Reply split — an eternally-empty "Request" pane would
// misrepresent absence-of-data as a value that just hasn't arrived yet.
// There is also no more "pending" status: a span row only ever appears
// already finished (natstrace publishes once, at End/Fail), so the
// three-state status model (pending/ok/error) becomes two (ok/error).

const spansById = reactive({})
const order = ref([]) // spanIds, newest first
const MAX_ROWS = 500
const truncated = ref(false) // sticky — set once eviction has happened at least once

// api.*/rpc.* subjects have fixed 6-token arity — family, context, service,
// entity, action, version (ARCHITECTURE-COMMUNICATIONS.md §2 decision 4) —
// so a token's position tells you what it means without parsing the value.
// Facet filtering below is purely positional for that reason.
const FACET_POSITION_NAMES = ['family', 'context', 'service', 'entity', 'action', 'version']

function upsertSpan(span) {
  if (!span?.spanId) return
  if (!(span.spanId in spansById)) {
    order.value = [span.spanId, ...order.value]
    if (order.value.length > MAX_ROWS) {
      for (const id of order.value.slice(MAX_ROWS)) delete spansById[id]
      order.value = order.value.slice(0, MAX_ROWS)
      truncated.value = true
    }
  }
  spansById[span.spanId] = span
}

const { connected, bootstrapFailed, everDisconnected } = useTraceFeed({
  onUpsert: (traceId, spans) => {
    for (const span of spans) upsertSpan(span)
  },
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
const statusOn = reactive({ ok: true, error: true })
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

function spanFamily(span) {
  return span.subject?.split('.')[0] || ''
}
function spanStatus(span) {
  return span.statusCode === 'ERROR' ? 'error' : 'ok'
}

function rowMatchesFilters(span) {
  if (!familyOn[spanFamily(span)]) return false
  if (!statusOn[spanStatus(span)]) return false
  if (searchText.value && !span.subject?.toLowerCase().includes(searchText.value.toLowerCase())) return false
  if (facets.value.length) {
    const tokens = span.subject?.split('.') || []
    for (const f of facets.value) {
      if (tokens[f.index] !== f.value) return false
    }
  }
  return true
}

const rows = computed(() => displayedOrder.value.map((id) => spansById[id]).filter((r) => r && rowMatchesFilters(r)))

// ── Selection / detail pane ─────────────────────────────────────────────
const selectedId = ref(null)
const selectedSpan = computed(() => (selectedId.value ? spansById[selectedId.value] : null))
function selectRow(span) {
  // A click-drag to select row text (e.g. to copy the subject or a value)
  // still fires a native 'click' on mouseup. Without this guard that click
  // toggles the row's selection off from underneath the drag, leaving the
  // detail panel closed even though the user only meant to select text.
  const selection = window.getSelection()
  if (selection && selection.toString().length > 0) return
  selectedId.value = selectedId.value === span.spanId ? null : span.spanId
}
function closeDetail() {
  selectedId.value = null
}

// ── Formatting helpers ──────────────────────────────────────────────────
function statusSeverity(status) {
  return status === 'error' ? 'danger' : 'success'
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
function spanTimeMs(span) {
  return span.timestamp ? new Date(span.timestamp).getTime() : null
}

function formatDuration(ms) {
  return ms == null ? '—' : `${ms} ms`
}

function formatBytes(n) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  return `${(n / 1024).toFixed(1)} KB`
}

function headerCount(headers) {
  return headers ? Object.keys(headers).length : 0
}
function headerRows(headers) {
  if (!headers) return []
  return Object.entries(headers).map(([k, v]) => ({ k, v: Array.isArray(v) ? v.join(', ') : String(v) }))
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
    <Tabs
      v-model:value="ui.rpcTab"
      class="panel-tabs rpc-tabs"
    >
      <TabList>
        <Tab value="pulse">Pulse</Tab>
        <Tab value="traces">Traces</Tab>
        <Tab value="messages">Messages</Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="pulse">
          <div class="lab-panel rpc-card">
            <KeepAlive>
              <PulsePanel v-if="ui.rpcTab === 'pulse'" />
            </KeepAlive>
          </div>
        </TabPanel>
        <TabPanel value="traces">
          <div class="lab-panel rpc-card">
            <KeepAlive>
              <TraceWaterfall v-if="ui.rpcTab === 'traces'" />
            </KeepAlive>
          </div>
        </TabPanel>
        <TabPanel value="messages">
          <div
            v-if="ui.rpcTab === 'messages'"
            class="lab-panel rpc-card"
          >
            <div class="rpc-toolbar">
        <Tag
          :severity="connected ? 'success' : 'danger'"
          :value="connected ? 'live' : 'disconnected'"
        />
        <span class="search-box">
          <i class="pi pi-search" />
          <input
            v-model="searchText"
            type="text"
            placeholder="filter subjects — or click any subject token below"
          >
        </span>
        <button
          v-for="fam in ['rpc', 'api']"
          :key="fam"
          type="button"
          class="chip"
          :class="{ on: familyOn[fam] }"
          @click="toggleFamily(fam)"
        >
          {{ fam }}
        </button>
        <button
          v-for="st in ['ok', 'error']"
          :key="st"
          type="button"
          class="chip"
          :class="{ on: statusOn[st], err: st === 'error' }"
          @click="toggleStatus(st)"
        >
          {{ st }}
        </button>
        <button
          v-for="f in facets"
          :key="f.index"
          type="button"
          class="chip facet"
          @click="removeFacet(f.index)"
        >
          <span class="facet-key">{{ f.name }}:</span>{{ f.value }}<span class="facet-x">✕</span>
        </button>
        <button
          type="button"
          class="pause-btn"
          @click="togglePause"
        >
          {{ paused ? '▶ resume' : '⏸ pause' }}
        </button>
      </div>

      <p
        v-if="bootstrapFailed || everDisconnected"
        class="err-line"
      >
        {{ bootstrapFailed ? 'Initial trace snapshot failed to load.' : 'Live feed dropped at least once — some spans may be missing.' }}
      </p>
      <div
        v-if="truncated"
        class="paged-note"
        :title="`Only the most recent ${MAX_ROWS.toLocaleString()} spans are kept in this view — older rows are evicted as new ones arrive.`"
      >
        showing the most recent {{ MAX_ROWS.toLocaleString() }} — older rows evicted
      </div>

      <DataTable
        :value="rows"
        size="small"
        scrollable
        scroll-height="flex"
        class="rpc-table"
        resizable-columns
        column-resize-mode="expand"
        data-key="spanId"
        selection-mode="single"
        :meta-key-selection="false"
        @row-click="selectRow($event.data)"
      >
        <template #empty>
          <span class="lab-muted">Waiting for rpc.*/api.* traffic — trigger a shipping-service → refdata-service item lookup, or a Sea Freight Flow action, to see it here.</span>
        </template>
        <Column
          header="Status"
          style="width:80px"
        >
          <template #body="{ data }">
            <Tag
              :severity="statusSeverity(spanStatus(data))"
              :value="spanStatus(data)"
            />
          </template>
        </Column>
        <Column
          header="Fam"
          style="width:50px"
        >
          <template #body="{ data }">
            <span
              class="fam-badge"
              :class="spanFamily(data)"
            >{{ spanFamily(data) }}</span>
          </template>
        </Column>
        <Column header="Subject">
          <template #body="{ data }">
            <SubjectPath
              :subject="data.subject"
              clickable
              @token-click="onTokenClick"
            />
          </template>
        </Column>
        <Column
          header="Time"
          style="width:100px;font-variant-numeric:tabular-nums"
        >
          <template #body="{ data }">
            {{ formatTimeMs(spanTimeMs(data)) }}
          </template>
        </Column>
        <Column
          header="Duration"
          style="width:80px"
          body-class="num-cell"
        >
          <template #body="{ data }">
            {{ formatDuration(data.durationMs) }}
          </template>
        </Column>
        <Column
          header="Size"
          style="width:80px"
          body-class="num-cell"
        >
          <template #body="{ data }">
            {{ formatBytes(data.payloadBytes) }}
          </template>
        </Column>
      </DataTable>

      <section
        v-if="selectedSpan"
        class="detail"
      >
        <div class="detail-head">
          <SubjectPath
            :subject="selectedSpan.subject"
            clickable
            @token-click="onTokenClick"
          />
          <span class="meta">
            <Tag
              :severity="statusSeverity(spanStatus(selectedSpan))"
              :value="spanStatus(selectedSpan)"
            />
            <span>duration <b>{{ formatDuration(selectedSpan.durationMs) }}</b></span>
            <span>trace <b :title="selectedSpan.traceId">{{ selectedSpan.traceId }}</b></span>
          </span>
          <span
            class="close"
            title="Close"
            @click="closeDetail"
          >✕</span>
        </div>
        <div class="pane-body">
          <div
            v-if="selectedSpan.error"
            class="err-banner"
          >
            {{ selectedSpan.error }}
          </div>
          <div class="sect">
            <div class="sect-label">
              Headers <span class="count">({{ headerCount(selectedSpan.headers) }})</span>
              <span
                v-if="headerCount(selectedSpan.headers)"
                class="copy"
                @click="copyHeaders(selectedSpan.headers)"
              >copy</span>
            </div>
            <div
              v-if="headerCount(selectedSpan.headers)"
              class="kv"
            >
              <div
                v-for="h in headerRows(selectedSpan.headers)"
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
            >no headers</span>
          </div>
          <div class="sect">
            <div class="sect-label">
              Body <span
                class="copy"
                @click="copyPayload(selectedSpan.payload)"
              >copy</span>
            </div>
            <pre
              class="json"
              v-html="highlightJson(selectedSpan.payload) || '—'"
            />
          </div>
        </div>
      </section>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>

<style scoped>
.rpc-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

/* ── [pulse]/[traces]/[messages] tabs (Phase 28g, BR-035; real Tabs since
   Phase 28j; pulse tab added Phase 44 — see the "panel top tabs" rule in
   shared/unifi-theme/LAYOUT.md) — the panel below the tab strip needs the
   full height a page-level Tabs normally isn't asked to fill
   (TraceWaterfall's own rail/waterfall split, the messages table's
   scroll-height="flex"), so p-tabs/p-tabpanels/p-tabpanel all stay flex
   columns down to the active panel rather than PrimeVue's plain block
   default. */
.rpc-tabs.p-tabs {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
/* Flush on the page, same as AccountsView's Tabs — no card wraps the tab
   strip itself, so no background/margin override is needed here (that was
   only necessary while the Tabs sat nested inside a .lab-panel card). The
   card treatment now applies per-tab, below, via .rpc-card. */
.rpc-tabs :deep(.p-tablist) {
  flex: none;
}
/* Keeps Aura's default TOP padding (the same gap AccountsView's tabpanels
   get, unmodified there) — without it the .rpc-card below sits flush against
   the tablist's hairline, and the two borders visually merge into one line.
   The default's horizontal padding is zeroed globally instead, in
   `.panel-tabs.p-tabs .p-tabpanels` (shared/unifi-theme/unifi.css) — it was
   stacking on top of the shell's own `.main-inner` inset. */
.rpc-tabs :deep(.p-tabpanels) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.rpc-tabs :deep(.p-tabpanel) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
/* The card each tab's content lives in — .lab-panel's own border/background/
   padding, plus the flex sizing .lab-panel itself doesn't provide (this
   panel fills all remaining height, unlike a typical page-scrolled
   .lab-panel). */
.rpc-card {
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

.err-line {
  flex: none;
  margin: 0;
  font-size: 12px;
  color: #e5484d;
}
.paged-note {
  margin-top: 5px;
  font-size: 10px;
  line-height: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--p-amber-400, #fbbf24);
  cursor: help;
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
.pane-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 6px 10px 8px;
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
