<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'

import StreamView from './StreamView.vue'
import { listStreams } from '../api'

// One tab per stream actually registered on the NATS server — not a
// user-managed open set. Only the ACTIVE tab's StreamView is mounted: each
// one opens two EventSource connections (Messages + Stream), and browsers
// cap concurrent connections per origin at 6 (HTTP/1.1) — counting this
// app's KV-watch and refdata-label SSE connections too, keeping every
// registered stream connected in the background would exhaust that budget
// as soon as a second or third stream shows up. Switching tabs disconnects
// the old stream and connects the new one fresh (the "Stream" replay tab
// re-fetches full history on every mount, so nothing is lost by this).
const REFRESH_MS = 15000

const streams = ref([])
const activeStream = ref(null)

// SHIPPING first (the stream most demos revolve around), then alphabetical —
// purely a display convention, not a registry concept.
function sortStreams(names) {
  return [...names].sort((a, b) => {
    if (a === 'SHIPPING') return -1
    if (b === 'SHIPPING') return 1
    return a.localeCompare(b)
  })
}

async function refresh() {
  try {
    const res = await listStreams()
    streams.value = sortStreams(res?.values ?? [])
  } catch {
    // Best-effort — keep showing whatever was last known.
    return
  }
  if (!activeStream.value || !streams.value.includes(activeStream.value)) {
    activeStream.value = streams.value[0] ?? null
  }
}

let refreshTimer = null
onMounted(() => {
  refresh()
  refreshTimer = setInterval(refresh, REFRESH_MS)
})
onUnmounted(() => clearInterval(refreshTimer))

const hasStreams = computed(() => streams.value.length > 0)
</script>

<template>
  <div class="jetstream-view">
    <div class="stream-tabbar">
      <button
        v-for="name in streams"
        :key="name"
        type="button"
        class="stream-tab"
        :class="{ active: name === activeStream }"
        @click="activeStream = name"
      >
        {{ name }}
      </button>
    </div>

    <div v-if="!hasStreams" class="empty-state lab-muted">
      No streams registered on the server yet.
    </div>
    <StreamView v-else-if="activeStream" :key="activeStream" :stream="activeStream" />
  </div>
</template>

<style scoped>
.jetstream-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.stream-tabbar {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  flex-shrink: 0;
  border-bottom: 1px solid var(--lab-panel-border);
  padding-bottom: 0.4rem;
}
.stream-tab {
  all: unset;
  box-sizing: border-box;
  cursor: pointer;
  padding: 3px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--p-text-muted-color);
}
.stream-tab:hover {
  background: var(--lab-panel-border);
  color: var(--p-text-color);
}
.stream-tab.active {
  background: var(--lab-panel-border);
  color: var(--p-text-color);
}
.stream-tab:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
.empty-state {
  font-size: 12px;
  padding: 1rem 0;
}
</style>
