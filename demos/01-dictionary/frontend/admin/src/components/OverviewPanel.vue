<script setup>
import Tag from 'primevue/tag'
import { computed } from 'vue'

import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()

const kvRev = computed(() => {
  const revs = [...store.shapeARows, ...store.shapeBRows]
    .map((r) => r.revision)
    .filter((r) => typeof r === 'number')
  return revs.length ? Math.max(...revs) : 0
})

const stats = computed(() => [
  { k: 'Shape A entries', v: store.shapeARows.length, m: `KV read model · context ${store.context}` },
  { k: 'KV rev', v: kvRev.value, m: store.events[0] ? `last write ${store.events[0].at}` : 'no writes yet' },
  { k: 'Watch buffer', v: store.events.length, m: 'recent KV changes held' },
  { k: 'Shape B entries', v: store.shapeBRows.length, m: 'KV cache (dict-b) entries' },
])
</script>

<template>
  <section class="lab-panel">
    <div class="head">
      <div>
        <h2 class="panel-title">Pipeline health</h2>
        <p class="lab-muted sub">Live from the event stream · {{ store.context }}</p>
      </div>
      <Tag
        :severity="store.connected ? 'success' : 'danger'"
        :value="store.connected ? 'watching' : 'disconnected'"
      />
    </div>
    <div class="cards">
      <div v-for="s in stats" :key="s.k" class="stat">
        <div class="stat-k">{{ s.k }}</div>
        <div class="stat-v">{{ s.v }}</div>
        <div class="stat-m lab-muted">{{ s.m }}</div>
      </div>
    </div>
  </section>


</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}
.panel-title {
  margin: 0;
  font-size: 11px;
  line-height: 16px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.sub {
  margin: 2px 0 0;
  font-size: 11px;
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 0.625rem;
}
.stat {
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 0.625rem 0.75rem;
}
:root:not(.p-dark) .stat {
  background: rgba(0, 0, 0, 0.02);
}
.stat-k {
  color: var(--p-text-muted-color);
  font-size: 10px;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}
.stat-v {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 22px;
  line-height: 28px;
  font-variant-numeric: tabular-nums;
  margin-top: 2px;
}
.stat-m {
  font-size: 11px;
  margin-top: 2px;
}
</style>
