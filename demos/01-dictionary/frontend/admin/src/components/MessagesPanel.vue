<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { computed, reactive, ref } from 'vue'

import SubjectPath from './SubjectPath.vue'
import { highlightJson } from '../jsonHighlight.js'
import { usePubsubFeed } from '../nats/usePubsubFeed.js'

// Cross-tenant pub/sub wire tap (Phase 43c, BR-048). Its own SYSTEM → NATS
// nav entry rather than a fourth RpcPanel tab: RpcPanel's three tabs are all
// views of ONE dataset (obs.trace.* request/reply spans — flat, grouped, and
// aggregated), whereas this reads a different bucket fed by a different
// stream, carries a tenant column none of them can populate, and is the
// publish side rather than the call side. Making it a tab would put two
// unrelated pipes behind one label.
//
// Rows come from usePubsubFeed (the pubsub-messages bucket); the family
// filter, row cap and pause below are this panel's own concern, exactly as
// RpcPanel keeps its flattening local to itself.
const messagesById = reactive({})
const order = ref([]) // spanIds, newest first
const MAX_ROWS = 500
const truncated = ref(false) // sticky — set once eviction has happened at least once

const { upsertMessage: feedUpsert, connected, bootstrapFailed, everDisconnected } = usePubsubFeed({
  onUpsert: (spanId, record) => {
    if (!messagesById[spanId]) order.value = [spanId, ...order.value]
    messagesById[spanId] = { ...record.span, tenant: record.tenant }
    if (order.value.length > MAX_ROWS) {
      for (const id of order.value.slice(MAX_ROWS)) delete messagesById[id]
      order.value = order.value.slice(0, MAX_ROWS)
      truncated.value = true
    }
  },
})
// Re-exported so the panel drives the same seam its feed does — the tests
// push rows through here rather than reaching into the reactive map.
const upsertMessage = feedUpsert

// ── Pause ────────────────────────────────────────────────────────────────
// The feed keeps ingesting while paused; only the visible ordering freezes,
// so resuming shows everything that arrived rather than a gap.
const paused = ref(false)
const frozenOrder = ref([])
function togglePause() {
  if (!paused.value) frozenOrder.value = [...order.value]
  paused.value = !paused.value
}
const displayedOrder = computed(() => (paused.value ? frozenOrder.value : order.value))

// ── Filtering ────────────────────────────────────────────────────────────
// notify.* defaults OFF: it is largely a fan-out of the same state changes
// already visible on the evt.* side, so leaving it on would double the rows
// for no new information on first open.
const searchText = ref('')
const familyOn = reactive({ evt: true, notify: false })
const tenantFilter = ref('')

function toggleFamily(fam) {
  familyOn[fam] = !familyOn[fam]
}
function msgFamily(msg) {
  return msg.subject?.split('.')[0] || ''
}
function onTokenClick({ index, text }) {
  if (index === 0) {
    if (text in familyOn) familyOn[text] = true
    return
  }
  searchText.value = searchText.value === text ? '' : text
}
function selectTenant(tenant) {
  tenantFilter.value = tenantFilter.value === tenant ? '' : tenant
}

function rowMatchesFilters(msg) {
  if (!familyOn[msgFamily(msg)]) return false
  if (tenantFilter.value && msg.tenant !== tenantFilter.value) return false
  if (searchText.value && !msg.subject?.toLowerCase().includes(searchText.value.toLowerCase())) return false
  return true
}

const rows = computed(() => displayedOrder.value.map((id) => messagesById[id]).filter((r) => r && rowMatchesFilters(r)))

// ── Selection / detail pane ─────────────────────────────────────────────
const selectedId = ref(null)
const selectedMsg = computed(() => (selectedId.value ? messagesById[selectedId.value] : null))
function selectRow(msg) {
  const selection = window.getSelection()
  if (selection && selection.toString().length > 0) return
  selectedId.value = selectedId.value === msg.spanId ? null : msg.spanId
}
function closeDetail() {
  selectedId.value = null
}

// ── Formatting helpers ──────────────────────────────────────────────────
function formatTimeMs(ms) {
  if (!ms) return '—'
  const d = new Date(ms)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${hh}:${mm}:${ss}.${String(d.getMilliseconds()).padStart(3, '0')}`
}
function msgTimeMs(msg) {
  return msg.timestamp ? new Date(msg.timestamp).getTime() : null
}
function formatBytes(n) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  return `${(n / 1024).toFixed(1)} KB`
}

defineExpose({ upsertMessage, MAX_ROWS })
</script>

<template>
  <div class="lab-panel msg-card">
    <div class="msg-toolbar">
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
        v-for="fam in ['evt', 'notify']"
        :key="fam"
        type="button"
        class="chip"
        :class="{ on: familyOn[fam] }"
        @click="toggleFamily(fam)"
      >
        {{ fam }}
      </button>
      <button
        v-if="tenantFilter"
        type="button"
        class="chip facet"
        @click="selectTenant(tenantFilter)"
      >
        <span class="facet-key">tenant:</span>{{ tenantFilter }}<span class="facet-x">✕</span>
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
      {{ bootstrapFailed ? 'Initial message snapshot failed to load.' : 'Live feed dropped at least once — some messages may be missing.' }}
    </p>
    <div
      v-if="truncated"
      class="paged-note"
      :title="`Only the most recent ${MAX_ROWS.toLocaleString()} messages are kept in this view — older rows are evicted as new ones arrive.`"
    >
      showing the most recent {{ MAX_ROWS.toLocaleString() }} — older rows evicted
    </div>

    <DataTable
      :value="rows"
      size="small"
      scrollable
      scroll-height="flex"
      class="msg-table"
      resizable-columns
      column-resize-mode="expand"
      data-key="spanId"
      selection-mode="single"
      :meta-key-selection="false"
      @row-click="selectRow($event.data)"
    >
      <template #empty>
        <span class="lab-muted">Waiting for evt.*/notify.* traffic — publish a shipping event, or act on a tenant, to see it here.</span>
      </template>
      <Column
        header="Tenant"
        style="width:110px"
      >
        <template #body="{ data }">
          <button
            type="button"
            class="tenant-btn"
            data-testid="msg-tenant"
            :class="{ platform: data.tenant === '_platform' }"
            title="Filter to this tenant"
            @click.stop="selectTenant(data.tenant)"
          >{{ data.tenant }}</button>
        </template>
      </Column>
      <Column
        header="Fam"
        style="width:60px"
      >
        <template #body="{ data }">
          <span
            class="fam-badge"
            :class="msgFamily(data)"
          >{{ msgFamily(data) }}</span>
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
          {{ formatTimeMs(msgTimeMs(data)) }}
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
      v-if="selectedMsg"
      class="detail"
    >
      <div class="detail-head">
        <SubjectPath
          :subject="selectedMsg.subject"
          clickable
          @token-click="onTokenClick"
        />
        <span class="meta">
          <span>tenant <b>{{ selectedMsg.tenant }}</b></span>
          <span>trace <b :title="selectedMsg.traceId">{{ selectedMsg.traceId }}</b></span>
        </span>
        <span
          class="close"
          title="Close"
          @click="closeDetail"
        >✕</span>
      </div>
      <div class="pane-body">
        <div
          v-if="selectedMsg.redacted?.length || selectedMsg.truncated"
          class="redact-banner"
        >
          <template v-if="selectedMsg.redacted?.length">
            redacted: {{ selectedMsg.redacted.join(', ') }}
          </template>
          <template v-if="selectedMsg.truncated">
            payload truncated to the observation cap
          </template>
        </div>
        <div class="sect">
          <div class="sect-label">
            Body
          </div>
          <pre
            class="json"
            v-html="highlightJson(selectedMsg.payload) || '—'"
          />
        </div>
      </div>
    </section>

    <p class="foot-note">
      Best-effort feed: observations are fire-and-forget publishes on a bounded,
      short-retention stream (BR-047), so this is a sample of the wire, not an audit log.
    </p>
  </div>
</template>

<style scoped>
.msg-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

/* ── filter bar ── (same idioms as RpcPanel's .rpc-toolbar) */
.msg-toolbar {
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
.chip.facet {
  border-color: rgba(0, 111, 255, 0.45);
  color: var(--p-text-color);
  background: rgba(0, 111, 255, 0.1);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
}
.facet-key,
.facet-x {
  color: var(--p-text-disabled-color);
}
.facet-x {
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
.foot-note {
  flex: none;
  margin: 0;
  font-size: 10px;
  line-height: 14px;
  color: var(--p-text-disabled-color);
}

/* ── table ── */
.msg-table {
  flex: 1;
  min-height: 0;
}
.msg-table :deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
.msg-table :deep(.p-datatable-tbody > tr > td) {
  padding-top: 3px;
  padding-bottom: 3px;
  font-size: 11px;
}
.tenant-btn {
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  color: var(--lab-accent);
}
.tenant-btn.platform {
  color: var(--p-text-muted-color);
}
.fam-badge {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  color: var(--p-text-muted-color);
}
.fam-badge.evt {
  color: var(--lab-accent);
}

/* ── detail pane ── */
.detail {
  flex: none;
  max-height: 45%;
  overflow: auto;
  border-top: 1px solid var(--lab-panel-border);
  padding-top: 6px;
}
.detail-head {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.detail-head .meta {
  display: flex;
  gap: 12px;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.detail-head .close {
  margin-left: auto;
  cursor: pointer;
  color: var(--p-text-disabled-color);
}
.redact-banner {
  margin: 6px 0;
  font-size: 11px;
  color: var(--p-amber-400, #fbbf24);
}
.sect-label {
  margin: 6px 0 2px;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--p-text-disabled-color);
}
.json {
  margin: 0;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  line-height: 16px;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
