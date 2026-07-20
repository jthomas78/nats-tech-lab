<script setup>
import { computed } from 'vue'

import { useDictionaryStore } from '../stores/dictionary'

// Always-on pipeline telemetry — the "is the stream alive" glance every
// observability tool has. Everything here is derived from the same KV-watch
// read model the panels use; nothing is faked.
const store = useDictionaryStore()

// Highest KV revision seen across both shapes — the freshest projected write.
const kvRev = computed(() => {
  const revs = [...store.shapeARows, ...store.shapeBRows]
    .map((r) => r.revision)
    .filter((r) => typeof r === 'number')
  return revs.length ? Math.max(...revs) : 0
})

const ships = computed(() => store.shapeARows.length)
const lastAt = computed(() => store.events[0]?.at ?? '—')
</script>

<template>
  <footer class="telemetry">
    <span class="cell">
      <span class="dot" :class="store.connected ? 'ok' : 'bad'" />
      <span class="lbl">conn</span>
      <span class="val">{{ store.connected ? 'watching' : 'disconnected' }}</span>
    </span>
    <span class="cell"><span class="lbl">stream</span><span class="val">SHIPPING</span></span>
    <span class="cell"><span class="lbl">ships</span><span class="val">{{ ships }}</span></span>
    <span class="cell"><span class="lbl">kv rev</span><span class="val">{{ kvRev }}</span></span>
    <span class="cell"><span class="lbl">watch buffer</span><span class="val">{{ store.events.length }}</span></span>
    <span class="cell"><span class="lbl">last</span><span class="val">{{ lastAt }}</span></span>
  </footer>
</template>

<style scoped>
.telemetry {
  position: sticky;
  bottom: 0;
  border-top: 1px solid var(--lab-panel-border);
  background: color-mix(in srgb, var(--lab-panel-bg) 92%, transparent);
  backdrop-filter: blur(6px);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  color: var(--p-text-muted-color);
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  padding: 5px 0.75rem;
}
.cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 12px;
  white-space: nowrap;
}
.cell + .cell {
  border-left: 1px solid var(--lab-panel-border);
}
.lbl {
  color: var(--p-text-disabled-color);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-size: 10px;
}
.val {
  color: var(--p-text-color);
  font-variant-numeric: tabular-nums;
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}
.dot.ok {
  background: var(--p-green-400, #34d399);
  animation: pulse 2s ease-in-out infinite;
}
.dot.bad {
  background: var(--p-red-400, #f87171);
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
@media (prefers-reduced-motion: reduce) {
  .dot.ok { animation: none; }
}
</style>
