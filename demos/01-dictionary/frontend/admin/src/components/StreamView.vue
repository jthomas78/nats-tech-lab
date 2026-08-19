<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { computed, onMounted, ref } from 'vue'

import SubjectPath from './SubjectPath.vue'
import { getJetstreamReplay } from '../api'

// One stream's content: a DeliverAll replay snapshot.
// account is required alongside stream because a stream name is only unique
// WITHIN a NATS account — every tenant provisions its own SHIPPING — so the
// replay fetch would otherwise be ambiguous about which one it means.
const props = defineProps({
  account: { type: String, required: true },
  stream: { type: String, required: true },
  // One row of listStreams' response for this stream, or undefined if the
  // rail hasn't polled yet — purely for the header's status line.
  status: { type: Object, default: undefined },
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
    streamEvents.value = (await getJetstreamReplay(props.account, props.stream)) ?? []
    streamConnected.value = true
  } catch {
    streamEvents.value = []
  }
}

onMounted(connectStream)

// Every stream shows a point-in-time replay snapshot; the backend holds a
// connection per account and fetches it on the browser's behalf.
const snapshotReason = computed(() => {
  if (props.account === 'platform') {
    return 'Point-in-time snapshot, fetched by the backend over the PLATFORM connection.'
  }
  return 'Point-in-time snapshot, re-fetched when you reselect this stream.'
})

// A stream with exactly one configured subject filter (REFDATA, RPCTRACE) is
// the common case here, not an edge case, so the header shouldn't read
// "1 subject filters".
function pluralize(n, noun) {
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}

function formatBytes(n) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

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
    <header class="detail-head">
      <div class="detail-title">
        <code class="stream-name">{{ stream }}</code>
        <Tag severity="secondary" :value="account" class="stream-status-tag" />
        <Tag
          :severity="streamConnected ? 'success' : 'secondary'"
          :value="streamConnected ? 'snapshot' : 'idle'"
          class="stream-status-tag"
        />
        <span v-if="streamEvents.length" class="msg-count">{{ streamEvents.length }}</span>
      </div>
      <div v-if="status" class="detail-meta lab-muted">
        <span><strong>{{ status.messages }}</strong> messages</span>
        <span>· {{ formatBytes(status.bytes) }}</span>
        <span>· seq {{ status.firstSeq }}–{{ status.lastSeq }}</span>
        <span>· {{ pluralize(status.subjects, 'subject filter') }}</span>
        <span>· {{ pluralize(status.consumers, 'consumer') }}</span>
      </div>
      <p class="snapshot-note lab-muted">{{ snapshotReason }}</p>
    </header>

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
/* Header mirrors KvInspector.vue's detail-head, so the two cross-account
   inspector panels read as one pattern. */
.detail-head {
  flex-shrink: 0;
  margin: 0.5rem 0 0.4rem;
}
.detail-title {
  display: flex;
  align-items: center;
  gap: 6px;
}
.stream-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--p-text-color);
}
.stream-status-tag {
  font-size: 10px;
}
.detail-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  font-size: 11px;
  margin-top: 3px;
}
.detail-meta strong {
  color: var(--p-text-color);
  font-variant-numeric: tabular-nums;
}
.snapshot-note {
  font-size: 11px;
  margin: 3px 0 0;
  max-width: 78ch;
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
