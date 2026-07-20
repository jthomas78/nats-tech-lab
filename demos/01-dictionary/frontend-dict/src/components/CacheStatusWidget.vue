<script setup>
import Tag from 'primevue/tag'

import { useDictionaryStore } from '../stores/dictionary'

// Reads the single cacheStatus fetched by the store (selectType /
// selectCategoryType / the SSE _meta watch) — the compact header chip in
// ItemGrid reads the same state, so there's one getCacheStatus call per type
// change, not one per consumer.
const store = useDictionaryStore()
</script>

<template>
  <div class="lab-panel cache-status">
    <h3>Cache Status</h3>
    <p
      v-if="!store.selectedType"
      class="lab-muted"
    >
      Select a type to see its cache status.
    </p>
    <template v-else-if="store.cacheStatus">
      <div class="status-row">
        <span class="lab-muted">Postgres version</span>
        <span>{{ store.cacheStatus.postgresVersion }}</span>
      </div>
      <div class="status-row">
        <span class="lab-muted">KV cache version</span>
        <span>{{ store.cacheStatus.kvVersion }}</span>
      </div>
      <div class="status-row">
        <span class="lab-muted">KV item count</span>
        <span>{{ store.cacheStatus.kvItemCount }}</span>
      </div>
      <Tag
        :severity="store.cacheStatus.inSync ? 'success' : 'warning'"
        :value="store.cacheStatus.inSync ? 'in sync' : 'stale'"
        class="sync-tag"
      />
    </template>
  </div>
</template>

<style scoped>
.status-row {
  display: flex;
  justify-content: space-between;
  padding: 2px 0;
  font-size: 12px;
}
.sync-tag {
  margin-top: 0.5rem;
}
</style>
