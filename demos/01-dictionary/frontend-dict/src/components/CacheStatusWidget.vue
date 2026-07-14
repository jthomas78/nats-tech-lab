<script setup>
import Tag from 'primevue/tag'
import { onMounted, ref, watch } from 'vue'

import { getCacheStatus } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()
const status = ref(null)
const loading = ref(false)

async function refresh() {
  if (!store.selectedType) {
    status.value = null
    return
  }
  loading.value = true
  try {
    status.value = await getCacheStatus(store.context, store.selectedType)
  } finally {
    loading.value = false
  }
}

// Re-check whenever the selected type changes, or the SSE watch reports a
// change to this type's _meta key — the Q5 versioned-read protocol made
// visible: Postgres's set version vs the KV cache's stamped version.
watch(() => store.selectedType, refresh, { immediate: true })
watch(
  () => store.lastCacheEvent,
  (event) => {
    if (event?.key === `${store.selectedType}._meta`) refresh()
  },
)

onMounted(refresh)
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
    <template v-else-if="status">
      <div class="status-row">
        <span class="lab-muted">Postgres version</span>
        <span>{{ status.postgresVersion }}</span>
      </div>
      <div class="status-row">
        <span class="lab-muted">KV cache version</span>
        <span>{{ status.kvVersion }}</span>
      </div>
      <div class="status-row">
        <span class="lab-muted">KV item count</span>
        <span>{{ status.kvItemCount }}</span>
      </div>
      <Tag
        :severity="status.inSync ? 'success' : 'warning'"
        :value="status.inSync ? 'in sync' : 'stale'"
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
