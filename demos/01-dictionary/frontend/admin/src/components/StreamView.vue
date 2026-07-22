<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { onMounted, onUnmounted, ref } from 'vue'

import SubjectPath from './SubjectPath.vue'
import { jetstreamStreamUrl, jetstreamWatchUrl } from '../api'

// One stream's content: Messages (live, DeliverNew) + Stream (replay,
// DeliverAll). Kept alive via v-show at the call site rather than v-if, so a
// background stream's connection and message count keep going while another
// one is focused. Both sub-views connect on mount (not lazily on tab click)
// so either one has data ready the moment you land here.
//
// The Messages/Stream switcher below is plain buttons + v-show, the same
// pattern as the stream tab bar above it — deliberately not PrimeVue's Tabs
// component, so its active-state color stays in our palette instead of
// PrimeVue's default accent-blue tab text.
const props = defineProps({
  stream: { type: String, required: true },
})

// A DeliverAll replay (the "Stream" tab) can dump hundreds or thousands of
// messages in a single burst — e.g. REFDATA re-publishes its whole seed set
// on every service restart. Appending to the reactive array once per message
// was an O(n) copy + full DataTable re-render on EVERY message, so a big
// burst froze the tab solid. Buffer arrivals in a plain array and flush into
// the reactive ref at most once per animation frame instead.
function batchedAppender(targetRef, { prepend = false } = {}) {
  let pending = []
  let scheduled = false
  function flush() {
    scheduled = false
    if (!pending.length) return
    const batch = pending
    pending = []
    targetRef.value = prepend
      ? [...batch.reverse(), ...targetRef.value]
      : [...targetRef.value, ...batch]
  }
  function push(ev) {
    pending.push(ev)
    if (!scheduled) {
      scheduled = true
      requestAnimationFrame(flush)
    }
  }
  return push
}

// ── Messages (live, DeliverNew) ──────────────────────────────────────────────
const liveEvents = ref([])
const liveConnected = ref(false)
let liveSource = null

function connectLive() {
  const appendLive = batchedAppender(liveEvents, { prepend: true })
  liveSource = new EventSource(jetstreamWatchUrl(props.stream))
  liveSource.onopen = () => { liveConnected.value = true }
  liveSource.onmessage = (e) => appendLive(JSON.parse(e.data))
  liveSource.onerror = () => { liveConnected.value = false }
}

function disconnectLive() {
  liveSource?.close()
  liveConnected.value = false
}

// ── Stream (replay all, DeliverAll) ──────────────────────────────────────────
const streamEvents = ref([])
const streamConnected = ref(false)
let streamSource = null

function connectStream() {
  // append newest last so this reads chronologically
  const appendStream = batchedAppender(streamEvents)
  streamSource = new EventSource(jetstreamStreamUrl(props.stream))
  streamSource.onopen = () => { streamConnected.value = true }
  streamSource.onmessage = (e) => appendStream(JSON.parse(e.data))
  streamSource.onerror = () => { streamConnected.value = false }
}

function disconnectStream() {
  streamSource?.close()
  streamSource = null
  streamConnected.value = false
}

onMounted(() => {
  connectLive()
  connectStream()
})
onUnmounted(() => { disconnectLive(); disconnectStream() })

const activeSubTab = ref('messages')

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
    <div class="sub-tabbar">
      <button type="button" class="sub-tab" :class="{ active: activeSubTab === 'messages' }" @click="activeSubTab = 'messages'">
        <span>Messages</span>
        <Tag :severity="liveConnected ? 'success' : 'danger'" :value="liveConnected ? 'live' : 'off'" class="sub-tab-tag" />
        <span v-if="liveEvents.length" class="msg-count">{{ liveEvents.length }}</span>
      </button>
      <button type="button" class="sub-tab" :class="{ active: activeSubTab === 'stream' }" @click="activeSubTab = 'stream'">
        <span>Stream</span>
        <Tag :severity="streamConnected ? 'success' : 'secondary'" :value="streamConnected ? 'connected' : 'idle'" class="sub-tab-tag" />
        <span v-if="streamEvents.length" class="msg-count">{{ streamEvents.length }}</span>
      </button>
    </div>

    <!-- Messages: live session only -->
    <div v-show="activeSubTab === 'messages'" class="sub-panel">
      <DataTable
        :value="liveEvents"
        size="small"
        scrollable
        scroll-height="flex"
        class="js-table"
        resizableColumns
        columnResizeMode="expand"
      >
        <template #empty>
          <span class="lab-muted">Waiting for messages — execute a shipping command to see it here.</span>
        </template>
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
    </div>

    <!-- Stream: full history, DeliverAll -->
    <div v-show="activeSubTab === 'stream'" class="sub-panel">
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
.sub-tabbar {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  margin: 0.5rem 0 0.4rem;
}
.sub-tab {
  all: unset;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 4px 2px;
  font-size: 13px;
  font-weight: 600;
  color: var(--p-text-muted-color);
  border-bottom: 2px solid transparent;
}
.sub-tab + .sub-tab {
  margin-left: 0.75rem;
}
.sub-tab:hover {
  color: var(--p-text-color);
}
.sub-tab.active {
  color: var(--p-text-color);
  border-bottom-color: var(--lab-accent);
}
.sub-tab:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: 2px;
}
.sub-tab-tag {
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
