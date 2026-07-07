<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { onMounted, onUnmounted, ref } from 'vue'

import { jetstreamWatchUrl } from '../api'

const events = ref([])
const connected = ref(false)
const expandedPayload = ref(null)
const collapsed = ref(false)
let source = null

function connect() {
  source = new EventSource(jetstreamWatchUrl)
  source.onopen = () => { connected.value = true }
  source.onmessage = (e) => {
    const ev = JSON.parse(e.data)
    events.value = [ev, ...events.value].slice(0, 50)
  }
  source.onerror = () => { connected.value = false }
}

function disconnect() {
  source?.close()
  connected.value = false
}

onMounted(connect)
onUnmounted(disconnect)

function subjectSeverity(subject) {
  return subject?.endsWith('.created') ? 'success' : 'warn'
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

function togglePayload(seq) {
  expandedPayload.value = expandedPayload.value === seq ? null : seq
}

function fullPayload(payload) {
  if (!payload) return ''
  return typeof payload === 'string' ? payload : JSON.stringify(payload, null, 2)
}
</script>

<template>
  <div class="lab-panel js-panel">
    <div class="panel-header" @click="collapsed = !collapsed">
      <div class="panel-header-left">
        <span class="collapse-icon">{{ collapsed ? '▶' : '▼' }}</span>
        <span class="panel-title">JetStream</span>
        <span class="lab-muted panel-subtitle">DICTIONARY stream — live messages</span>
        <span v-if="events.length > 0" class="msg-count">{{ events.length }}</span>
      </div>
      <Tag :severity="connected ? 'success' : 'danger'" :value="connected ? 'connected' : 'disconnected'" @click.stop />
    </div>

    <DataTable
      v-if="!collapsed"
      :value="events"
      size="small"
      paginator
      :rows="5"
      class="js-table"
    >
      <template #empty>
        <span class="lab-muted">Waiting for messages — publish an entry to see it here.</span>
      </template>
      <Column header="Event" style="width:90px">
        <template #body="{ data }">
          <Tag :severity="subjectSeverity(data.subject)" :value="subjectLabel(data.subject)" />
        </template>
      </Column>
      <Column header="Subject" style="width:220px">
        <template #body="{ data }">
          <span class="subject-full lab-muted">{{ data.subject }}</span>
        </template>
      </Column>
      <Column field="seq" header="Seq" style="width:60px;font-variant-numeric:tabular-nums" />
      <Column header="Time" style="width:90px;font-variant-numeric:tabular-nums">
        <template #body="{ data }">{{ formatTime(data.timestamp) }}</template>
      </Column>
      <Column header="Payload">
        <template #body="{ data }">
          <div class="payload-cell" @click="togglePayload(data.seq)">
            <span v-if="expandedPayload !== data.seq" class="payload-preview lab-muted">{{ payloadPreview(data.payload) }}</span>
            <pre v-else class="payload-expanded">{{ fullPayload(data.payload) }}</pre>
          </div>
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.js-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  cursor: pointer;
  user-select: none;
}
.panel-header-left {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.collapse-icon {
  font-size: 9px;
  color: var(--p-text-muted-color);
  width: 10px;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.panel-subtitle {
  font-size: 12px;
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
.subject-full {
  font-size: 11px;
}
.js-panel :deep(.p-datatable-tbody > tr > td) {
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
