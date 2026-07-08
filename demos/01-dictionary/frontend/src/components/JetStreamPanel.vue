<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import { onMounted, onUnmounted, ref } from 'vue'

import { jetstreamStreamUrl, jetstreamWatchUrl } from '../api'

// ── Messages tab (live, DeliverNew) ──────────────────────────────────────────
const liveEvents = ref([])
const liveConnected = ref(false)
let liveSource = null

function connectLive() {
  liveSource = new EventSource(jetstreamWatchUrl)
  liveSource.onopen = () => { liveConnected.value = true }
  liveSource.onmessage = (e) => {
    const ev = JSON.parse(e.data)
    liveEvents.value = [ev, ...liveEvents.value].slice(0, 50)
  }
  liveSource.onerror = () => { liveConnected.value = false }
}

function disconnectLive() {
  liveSource?.close()
  liveConnected.value = false
}

// ── Stream tab (replay all, DeliverAll) ──────────────────────────────────────
const streamEvents = ref([])
const streamConnected = ref(false)
let streamSource = null

function connectStream() {
  if (streamSource) return
  streamSource = new EventSource(jetstreamStreamUrl)
  streamSource.onopen = () => { streamConnected.value = true }
  streamSource.onmessage = (e) => {
    const ev = JSON.parse(e.data)
    // append newest last so stream tab reads chronologically
    streamEvents.value = [...streamEvents.value, ev].slice(-200)
  }
  streamSource.onerror = () => { streamConnected.value = false }
}

function disconnectStream() {
  streamSource?.close()
  streamConnected.value = false
}

onMounted(connectLive)
onUnmounted(() => { disconnectLive(); disconnectStream() })

// ── Collapse ──────────────────────────────────────────────────────────────────
const collapsed = ref(false)
const activeTab = ref('0')

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

function handleTabChange(value) {
  if (value === '1') connectStream()
}
</script>

<template>
  <div class="lab-panel js-panel">
    <div class="panel-header" @click="collapsed = !collapsed; if (!collapsed) activeTab = '0'">
      <div class="panel-header-left">
        <span class="collapse-icon">{{ collapsed ? '▶' : '▼' }}</span>
        <span class="panel-title">JetStream</span>
        <span class="lab-muted panel-subtitle">DICTIONARY stream</span>
        <span v-if="liveEvents.length > 0" class="msg-count">{{ liveEvents.length }}</span>
      </div>
    </div>

    <Tabs v-if="!collapsed" default-value="0" @update:value="handleTabChange">
      <TabList>
        <Tab value="0">
          Messages
          <Tag
            :severity="liveConnected ? 'success' : 'danger'"
            :value="liveConnected ? 'live' : 'off'"
            class="tab-tag"
            @click.stop
          />
        </Tab>
        <Tab value="1">
          Stream
          <Tag
            :severity="streamConnected ? 'success' : 'secondary'"
            :value="streamConnected ? 'connected' : 'idle'"
            class="tab-tag"
            @click.stop
          />
        </Tab>
      </TabList>

      <TabPanels>
        <!-- Messages: live session only -->
        <TabPanel value="0">
          <DataTable :value="liveEvents" size="small" paginator :rows="5" class="js-table" resizableColumns columnResizeMode="expand">
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
                <div class="payload-cell" @click="togglePayload('live-' + data.seq)">
                  <span v-if="expandedPayload !== 'live-' + data.seq" class="payload-preview lab-muted">{{ payloadPreview(data.payload) }}</span>
                  <pre v-else class="payload-expanded">{{ fullPayload(data.payload) }}</pre>
                </div>
              </template>
            </Column>
          </DataTable>
        </TabPanel>

        <!-- Stream: full history, DeliverAll -->
        <TabPanel value="1">
          <DataTable :value="streamEvents" size="small" paginator :rows="5" class="js-table" resizableColumns columnResizeMode="expand">
            <template #empty>
              <span class="lab-muted">No stream history yet.</span>
            </template>
            <Column field="seq" header="Seq" style="width:60px;font-variant-numeric:tabular-nums" />
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
        </TabPanel>
      </TabPanels>
    </Tabs>
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
.tab-tag {
  margin-left: 0.4rem;
  font-size: 10px;
}
.subject-full {
  font-size: 11px;
}
.js-panel :deep(.p-datatable-tbody > tr > td) {
  padding-top: 3px;
  padding-bottom: 3px;
}
.js-panel :deep(.p-tabs) {
  --p-tabs-tablist-border-width: 0 0 1px 0;
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
