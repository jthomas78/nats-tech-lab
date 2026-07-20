<script setup>
import Tag from 'primevue/tag'

import { useDictionaryStore } from '../stores/dictionary'

// Compact summary of the selected type's Postgres/KV sync state, placed next
// to the type heading so it reads as "this record's status" rather than a
// detached full-width panel. The fuller breakdown (versions, item count)
// stays available in CacheStatusWidget below.
const store = useDictionaryStore()
</script>

<template>
  <Tag
    v-if="store.cacheStatus"
    class="cache-sync-chip"
    :severity="store.cacheStatus.inSync ? 'success' : 'warning'"
    :value="store.cacheStatus.inSync ? `in sync · rev ${store.cacheStatus.kvVersion}` : 'stale'"
  />
</template>

<style scoped>
.cache-sync-chip {
  font-size: 10px;
  white-space: nowrap;
}
</style>
