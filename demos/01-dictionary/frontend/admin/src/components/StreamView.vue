<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { onMounted, onUnmounted, ref, watch } from 'vue'

import SubjectPath from './SubjectPath.vue'
import { getJetstreamReplay } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'

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

// ── Messages (live) — Phase 23: notify.{context}.shipping.raw.{entity}.{event}
// on the tenant NATS connection, replacing the DeliverNew EventSource. Only
// wired for the SHIPPING stream: that's the only stream
// eventhandler.publishRawNotify covers (Main-POC-Plan.md Phase 23) — a
// stream other than SHIPPING (e.g. REFDATA) simply has no raw notify
// publisher, so this tab stays empty for it rather than silently
// misreporting "live" for a feed that will never arrive.
const { connected: tenantConnected, subscribe } = useNatsConnection()
const liveEvents = ref([])
const liveConnected = ref(false)
let unsubscribeLive = null

// Reconstructs an evt.{context}.shipping.{entity}.{id}.{event} display
// subject from the notify subject + raw event payload, so subjectSeverity/
// subjectLabel/SubjectPath below behave identically to the old SSE feed's
// jsEvent.subject.
function parseRawShippingNotifySubject(subject) {
  const parts = subject.split('.')
  // notify.{context}.shipping.raw.{entity}.{event}
  if (parts.length !== 6 || parts[0] !== 'notify' || parts[2] !== 'shipping' || parts[3] !== 'raw') return null
  return { context: parts[1], entity: parts[4], event: parts[5] }
}

// Live rows have no JetStream sequence number (they never touch JetStream —
// notify.* is plain core NATS pub/sub) but the "live-" + seq key in the
// template needs something unique per row, so this synthesizes one.
let liveSeqCounter = 0

function connectLive() {
  if (props.stream !== 'SHIPPING' || !tenantConnected.value) return
  const appendLive = batchedAppender(liveEvents, { prepend: true })
  unsubscribeLive = subscribe(`notify.*.shipping.raw.>`, (payload, subject) => {
    const parsed = parseRawShippingNotifySubject(subject)
    if (!parsed) return
    const id = payload?.shipID ?? payload?.containerID ?? ''
    appendLive({
      subject: `evt.${parsed.context}.shipping.${parsed.entity}.${id}.${parsed.event}`,
      seq: `live-${++liveSeqCounter}`,
      timestamp: payload?.occurredAt ?? new Date().toISOString(),
      payload,
    })
  })
  liveConnected.value = true
}

function disconnectLive() {
  unsubscribeLive?.()
  unsubscribeLive = null
  liveConnected.value = false
}

// ── Stream (replay all) — Phase 23: one-shot GET /api/jetstream/replay,
// replacing the DeliverAll EventSource. A snapshot at request time, not a
// live feed — re-fetch (e.g. re-mount) to see anything published since.
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

function disconnectStream() {
  streamConnected.value = false
}

onMounted(() => {
  connectLive()
  connectStream()
})
onUnmounted(() => { disconnectLive(); disconnectStream() })

// Retry the live subscription once the tenant connection comes up (mount-
// order race — see KvInspector.vue's identical guard).
watch(tenantConnected, (isConnected) => {
  if (isConnected) connectLive()
})

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
