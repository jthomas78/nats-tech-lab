<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { onMounted, ref, watch } from 'vue'

import SubjectPath from './SubjectPath.vue'
import { getJetstreamReplay } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'

// One stream's content: a DeliverAll replay snapshot (was previously split
// into "Messages"/live and "Stream"/replay sub-tabs; the live sub-tab was
// dropped — it only ever worked for the SHIPPING stream, and a snapshot
// re-fetched on every mount/tenant-switch covers the same "see what's in
// this stream" need without a second tab).
const props = defineProps({
  stream: { type: String, required: true },
})

// A DeliverAll replay can dump hundreds or thousands of messages in a
// single burst — e.g. REFDATA re-publishes its whole seed set on every
// service restart — so streamEvents is replaced wholesale per fetch rather
// than appended incrementally.
const streamEvents = ref([])
const streamConnected = ref(false)

async function connectStream() {
  streamConnected.value = false
  try {
    streamEvents.value = (await getJetstreamReplay(props.stream)) ?? []
    streamConnected.value = true
  } catch {
    streamEvents.value = []
  }
}

onMounted(connectStream)

// Re-fetch on tenant switch — this stream name can exist under more than
// one tenant's account (e.g. SHIPPING), so without this the panel kept
// showing the previous tenant's snapshot until the stream tab was
// remounted. Same mount-order-race guard pattern as KvInspector.vue.
const { connected: tenantConnected } = useNatsConnection()
watch(tenantConnected, (isConnected) => {
  if (isConnected) connectStream()
})

// ── Shared helpers ────────────────────────────────────────────────────────────
function subjectSeverity(subject) {
  if (!subject) return 'secondary'
  if (subject.endsWith('.arrived') || subject.endsWith('.loaded')) return 'success'
  return 'warn'
}

function subjectLabel(subject) {
  if (!subject) return subject
  const parts = subject.split('.')
  return parts[parts.length - 1]
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

function togglePayload(seq) {
  expandedPayload.value = expandedPayload.value === seq ? null : seq
}

function fullPayload(payload) {
  if (!payload) return ''
  return typeof payload === 'string' ? payload : JSON.stringify(payload, null, 2)
}
</script>

<template>
  <div class="stream-view">
    <div class="stream-status">
      <span class="stream-status-label">Stream</span>
      <Tag :severity="streamConnected ? 'success' : 'secondary'" :value="streamConnected ? 'connected' : 'idle'" class="stream-status-tag" />
      <span v-if="streamEvents.length" class="msg-count">{{ streamEvents.length }}</span>
    </div>

    <div class="sub-panel">
      <DataTable
        :value="streamEvents"
        size="small"
        scrollable
        scroll-height="flex"
        class="js-table"
        resizableColumns
        columnResizeMode="expand"
        paginator
        :rows="50"
        :rowsPerPageOptions="[50, 100, 250]"
      >
        <template #empty>
          <span class="lab-muted">No stream history yet.</span>
        </template>
        <Column field="seq" header="Seq" style="width:60px;font-variant-numeric:tabular-nums" />
        <Column header="Event" style="width:90px">
          <template #body="{ data }">
            <Tag :severity="subjectSeverity(data.subject)" :value="subjectLabel(data.subject)" />
          </template>
        </Column>
        <Column header="Subject" style="width:320px">
          <template #body="{ data }">
            <SubjectPath :subject="data.subject" />
          </template>
        </Column>
        <Column header="Time" style="width:90px;font-variant-numeric:tabular-nums">
          <template #body="{ data }">{{ formatTime(data.timestamp) }}</template>
        </Column>
        <Column header="Payload">
          <template #body="{ data }">
            <div class="payload-cell" @click="togglePayload('stream-' + data.seq)">
              <span v-if="expandedPayload !== 'stream-' + data.seq" class="payload-preview lab-muted">{{ payloadPreview(data.payload) }}</span>
              <pre v-else class="payload-expanded">{{ fullPayload(data.payload) }}</pre>
            </div>
          </template>
        </Column>
      </DataTable>
    </div>
  </div>
</template>

<style scoped>
.stream-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.stream-status {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  margin: 0.5rem 0 0.4rem;
}
.stream-status-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--p-text-color);
}
.stream-status-tag {
  font-size: 10px;
}
.msg-count {
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  background: var(--lab-panel-border);
  color: var(--p-text-muted-color);
  border-radius: 10px;
  padding: 0 6px;
  line-height: 18px;
}
.sub-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.js-table {
  flex: 1;
  min-height: 0;
}
.js-table :deep(.p-datatable-tbody > tr > td) {
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
