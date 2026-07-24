<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'

import SubjectPath from './SubjectPath.vue'
import { rpcWatchUrl } from '../api'

// obs.rpc.* dual-transport RPC traffic (Phase 12.10) — live only, no replay
// (ARCHITECTURE-COMMUNICATIONS.md §6: the requirement is "only show while
// the app is open," which plain pub/sub already satisfies for free — no
// stream to provision). Request and reply arrive as two separate obs.rpc.*
// messages sharing a correlationId (the request's reply-to inbox); this
// pairs them into one row per call instead of two unrelated list entries.

// Keyed by correlationId so a reply can update its request's row in place
// without an index shifting under a prepend (order tracked separately).
const rowsById = reactive({})
const order = ref([]) // correlationIds, newest first
const MAX_ROWS = 500

function upsertRow(event) {
  let row = rowsById[event.correlationId]
  if (!row) {
    row = {
      correlationId: event.correlationId,
      subject: event.subject,
      requestPayload: null,
      replyPayload: null,
      error: '',
      status: 'pending',
      time: Date.now(),
    }
    rowsById[event.correlationId] = row
    order.value = [event.correlationId, ...order.value]
    if (order.value.length > MAX_ROWS) {
      for (const id of order.value.slice(MAX_ROWS)) delete rowsById[id]
      order.value = order.value.slice(0, MAX_ROWS)
    }
  }
  if (event.direction === 'request') {
    row.requestPayload = event.payload
  } else {
    row.replyPayload = event.payload
    row.error = event.error || ''
    row.status = event.error ? 'error' : 'ok'
  }
}

const rows = computed(() => order.value.map((id) => rowsById[id]))

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

function statusSeverity(status) {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'danger'
  return 'warn'
}

function formatTime(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function payloadPreview(payload) {
  if (!payload) return '—'
  const str = typeof payload === 'string' ? payload : JSON.stringify(payload)
  return str.length > 80 ? str.slice(0, 80) + '…' : str
}

const expandedPayload = ref(null)
function togglePayload(key) {
  expandedPayload.value = expandedPayload.value === key ? null : key
}
function fullPayload(payload) {
  if (!payload) return ''
  return typeof payload === 'string' ? payload : JSON.stringify(payload, null, 2)
}
</script>

<template>
  <div class="rpc-panel">
    <div class="rpc-toolbar">
      <Tag :severity="connected ? 'success' : 'danger'" :value="connected ? 'live' : 'disconnected'" />
      <span class="lab-muted rpc-hint">Live only — no history before this panel opened.</span>
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
    >
      <template #empty>
        <span class="lab-muted">Waiting for rpc.* traffic — trigger a shipping-service → refdata-service item lookup to see it here.</span>
      </template>
      <Column header="Status" style="width:90px">
        <template #body="{ data }">
          <Tag :severity="statusSeverity(data.status)" :value="data.status" />
        </template>
      </Column>
      <Column header="Subject" style="width:320px">
        <template #body="{ data }">
          <SubjectPath :subject="data.subject" />
        </template>
      </Column>
      <Column header="Time" style="width:90px;font-variant-numeric:tabular-nums">
        <template #body="{ data }">{{ formatTime(data.time) }}</template>
      </Column>
      <Column header="Request">
        <template #body="{ data }">
          <div class="payload-cell" @click="togglePayload('req-' + data.correlationId)">
            <span v-if="expandedPayload !== 'req-' + data.correlationId" class="payload-preview lab-muted">{{ payloadPreview(data.requestPayload) }}</span>
            <pre v-else class="payload-expanded">{{ fullPayload(data.requestPayload) }}</pre>
          </div>
        </template>
      </Column>
      <Column header="Reply">
        <template #body="{ data }">
          <div class="payload-cell" @click="togglePayload('rep-' + data.correlationId)">
            <span v-if="expandedPayload !== 'rep-' + data.correlationId" class="payload-preview lab-muted">{{ data.error || payloadPreview(data.replyPayload) }}</span>
            <pre v-else class="payload-expanded">{{ data.error || fullPayload(data.replyPayload) }}</pre>
          </div>
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.rpc-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.rpc-toolbar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 0.5rem 0 0.4rem;
}
.rpc-hint {
  font-size: 12px;
}
.rpc-table {
  flex: 1;
  min-height: 0;
}
.rpc-table :deep(.p-datatable-tbody > tr > td) {
  padding-top: 3px;
  padding-bottom: 3px;
}
.payload-cell {
  cursor: pointer;
}
.payload-preview {
  font-size: 12px;
  font-family: monospace;
}
.payload-expanded {
  font-size: 11px;
  font-family: monospace;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--p-text-color);
}
</style>
