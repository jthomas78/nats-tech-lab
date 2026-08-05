<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'

import StreamView from './StreamView.vue'
import { listStreams } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'

// A rail of every stream actually registered on the NATS server — not a
// user-managed open set — mirroring KvInspector.vue's bucket rail (Phase
// 24). Only the selected stream's StreamView is mounted: it holds a
// one-shot REST replay fetch (Stream tab, Phase 23) that gets re-run on
// every mount, so switching the selection disconnects the old stream and
// fetches the new one fresh with nothing lost.
//
// No per-row "connected" state in the rail: a JetStream stream doesn't
// have a live-connection concept the way a NATS client does — it's either
// registered on the server or it isn't, and listStreams() already only
// returns registered ones. The "connected"/"idle" Tag that does exist
// lives in the detail panel (StreamView.vue's header) because it reflects
// whether *this session's* replay fetch for the *selected* stream
// succeeded, which is only meaningful for the one stream actually being
// queried right now.
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

// Re-fetch immediately on tenant switch rather than waiting up to
// REFRESH_MS for the next scheduled poll — Deps.JS (and therefore this
// stream list) is swapped per-tenant server-side, so the rail otherwise
// shows the previous tenant's streams for a few seconds after switching.
const { connected: tenantConnected } = useNatsConnection()
watch(tenantConnected, (isConnected) => {
  if (isConnected) refresh()
})

const hasStreams = computed(() => streams.value.length > 0)
</script>

<template>
  <div class="jetstream-view">
    <aside class="rail" aria-label="Streams">
      <div class="rail-header">
        <span>Streams</span>
      </div>
      <div class="rail-group">
        <button
          v-for="name in streams"
          :key="name"
          type="button"
          class="rail-item"
          :class="{ active: name === activeStream }"
          @click="activeStream = name"
        >
          <code class="rail-name">{{ name }}</code>
        </button>
      </div>
      <p v-if="!hasStreams" class="lab-muted rail-empty">No streams registered on the server yet.</p>
    </aside>

    <StreamView v-if="activeStream" :key="activeStream" :stream="activeStream" class="detail" />
  </div>
</template>

<style scoped>
.jetstream-view {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 0.75rem;
}
/* ── Rail — matches KvInspector.vue's bucket rail exactly, so the two
   "pick one thing from a list, inspect it on the right" panels in this
   admin app read as one visual pattern. ── */
.rail {
  width: 220px;
  flex-shrink: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding-right: 0.25rem;
  border-right: 1px solid var(--lab-panel-border);
}
.rail-header {
  flex-shrink: 0;
  padding: 0 8px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-muted-color);
}
.rail-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.rail-item {
  all: unset;
  box-sizing: border-box;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 5px 8px;
  border-radius: 4px;
  border-left: 2px solid transparent;
  font-size: 12px;
  color: var(--p-text-muted-color);
}
.rail-item:hover {
  background: var(--lab-panel-border);
  color: var(--p-text-color);
}
.rail-item.active {
  background: var(--lab-panel-border);
  border-left-color: var(--lab-accent);
  color: var(--p-text-color);
  font-weight: 600;
}
.rail-item:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
.rail-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rail-empty {
  font-size: 12px;
  padding: 0 8px;
}
.detail {
  flex: 1;
  min-width: 0;
}
@media (max-width: 720px) {
  .jetstream-view {
    flex-direction: column;
  }
  .rail {
    width: auto;
    border-right: none;
    border-bottom: 1px solid var(--lab-panel-border);
    flex-direction: row;
    flex-wrap: wrap;
  }
}
</style>
