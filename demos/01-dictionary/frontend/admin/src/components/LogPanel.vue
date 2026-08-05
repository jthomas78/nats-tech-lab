<script setup>
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import ToggleButton from 'primevue/togglebutton'
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'

import { getNatsLog } from '../api'

// Log panel — tails NATS's own log_file (GET /api/nats/log), REST-polled
// like every other NATS panel in this app rather than a push/follow
// transport (see the design discussion this implements: level + one
// free-text filter is the whole query surface, tail is hard-capped
// server-side — no general grep DSL).
const REFRESH_MS = 4000

const LEVEL_OPTIONS = [
  { label: 'All levels', value: '' },
  { label: 'Error', value: 'error' },
  { label: 'Warn', value: 'warn' },
  { label: 'Info', value: 'info' },
  { label: 'Debug', value: 'debug' },
  { label: 'Trace', value: 'trace' },
]
const TAIL_OPTIONS = [
  { label: '100 lines', value: 100 },
  { label: '200 lines', value: 200 },
  { label: '500 lines', value: 500 },
  { label: '1000 lines', value: 1000 },
]

const level = ref('')
const q = ref('')
const tail = ref(200)
const live = ref(true)

const lines = ref([])
const truncated = ref(false)
const loading = ref(true)
const errorMsg = ref('')
const configured = ref(true) // false only on a 503 ("NATS_LOG_PATH unset") — distinct from a transient fetch error

const scrollEl = ref(null)

async function refresh() {
  try {
    const res = await getNatsLog({ level: level.value, q: q.value, tail: tail.value })
    lines.value = res.lines
    truncated.value = res.truncated
    errorMsg.value = ''
    configured.value = true
    await stickToBottomIfNear()
  } catch (err) {
    if (err.status === 503) {
      configured.value = false
    } else {
      errorMsg.value = err.message || 'Failed to load the NATS log'
    }
  } finally {
    loading.value = false
  }
}

// Auto-scroll only if the reader was already at (or near) the bottom before
// this refresh — jumping the view out from under someone who scrolled up to
// read an older line would be actively hostile.
async function stickToBottomIfNear() {
  const el = scrollEl.value
  if (!el) return
  const wasNear = el.scrollHeight - el.scrollTop - el.clientHeight < 60
  await nextTick()
  if (wasNear) el.scrollTop = el.scrollHeight
}

let timer = null
function scheduleRefresh() {
  clearInterval(timer)
  if (live.value) timer = setInterval(refresh, REFRESH_MS)
}
watch(live, scheduleRefresh)

// Debounced re-query on filter change — a plain setTimeout is enough here;
// this isn't a shared utility because nothing else in this app filters via
// free text against a REST-polled endpoint.
let debounceTimer = null
function onFilterChange() {
  loading.value = true
  clearTimeout(debounceTimer)
  debounceTimer = setTimeout(refresh, 300)
}
watch([level, q, tail], onFilterChange)

onMounted(() => {
  refresh()
  scheduleRefresh()
})
onUnmounted(() => {
  clearInterval(timer)
  clearTimeout(debounceTimer)
})

function levelOf(line) {
  if (line.includes('[ERR]')) return 'err'
  if (line.includes('[WRN]')) return 'wrn'
  if (line.includes('[INF]')) return 'inf'
  if (line.includes('[DBG]')) return 'dbg'
  if (line.includes('[TRC]')) return 'trc'
  return ''
}
</script>

<template>
  <div class="lab-panel log-panel">
    <div class="panel-header">
      <span class="panel-title">Log</span>
      <div class="header-actions">
        <ToggleButton v-model="live" onLabel="Live" offLabel="Paused" onIcon="pi pi-play" offIcon="pi pi-pause" size="small" />
        <Button icon="pi pi-refresh" text rounded size="small" :loading="loading" aria-label="Refresh" @click="refresh" />
      </div>
    </div>

    <p class="lab-muted description">
      Tails NATS's own log file (level + text filters applied server-side, most recent {{ tail }} matching lines) —
      no rotation, so this resets on container restart.
    </p>

    <div class="filters">
      <Select v-model="level" :options="LEVEL_OPTIONS" optionLabel="label" optionValue="value" size="small" />
      <InputText v-model="q" placeholder="Filter text, e.g. $SRV.STATS" size="small" class="q-input" />
      <Select v-model="tail" :options="TAIL_OPTIONS" optionLabel="label" optionValue="value" size="small" />
    </div>

    <p v-if="errorMsg" class="error-text">{{ errorMsg }}</p>

    <div v-if="!configured" class="log-unavailable lab-muted">
      Log tailing isn't configured for this deployment (NATS_LOG_PATH unset) — see nats/nats.conf's log_file.
    </div>
    <template v-else>
      <div ref="scrollEl" class="log-viewport">
        <div v-if="!loading && lines.length === 0" class="lab-muted log-empty">No lines match the current filter.</div>
        <div v-for="(line, i) in lines" :key="i" class="log-line" :class="levelOf(line)">{{ line }}</div>
      </div>
      <p v-if="truncated" class="lab-muted truncated-note">
        Showing the most recent {{ tail }} matching lines — older matches exist but aren't shown.
      </p>
    </template>
  </div>
</template>

<style scoped>
.log-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-height: 0;
  flex: 1;
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.description {
  margin: 0;
  font-size: 0.85rem;
}
.error-text {
  margin: 0;
  color: var(--p-red-400, #f87171);
  font-size: 0.85rem;
}
.filters {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.q-input {
  flex: 1 1 auto;
  min-width: 0;
}

.log-unavailable {
  padding: 1rem;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
}

.log-viewport {
  flex: 1;
  min-height: 320px;
  max-height: 60vh;
  overflow-y: auto;
  background: var(--lab-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 0.4rem 0.6rem;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11.5px;
  line-height: 1.5;
}
.log-empty {
  padding: 0.5rem;
}
.log-line {
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--p-text-muted-color);
}
.log-line.err {
  color: var(--p-red-400, #f87171);
}
.log-line.wrn {
  color: var(--p-amber-400, #fbbf24);
}
.log-line.inf {
  color: var(--p-text-color);
}
.log-line.dbg,
.log-line.trc {
  color: var(--p-text-disabled-color, #737c87);
}
.truncated-note {
  margin: 0;
  font-size: 0.75rem;
}
</style>
