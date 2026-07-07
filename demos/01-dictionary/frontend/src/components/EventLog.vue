<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import InputText from 'primevue/inputtext'
import SelectButton from 'primevue/selectbutton'
import Tag from 'primevue/tag'
import { computed, ref, watch } from 'vue'

import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()

const OP_SEVERITY = { PUT: 'success', DEL: 'warn', PURGE: 'danger' }

const SHAPE_OPTIONS = ['All', 'A', 'B']
const OP_OPTIONS = ['All', 'PUT', 'DEL', 'PURGE']

const filterShape = ref('All')
const filterOp = ref('All')
const filterKey = ref('')
const currentPage = ref(1)

// debounced key input
const debouncedKey = ref('')
let debounceTimer = null
watch(filterKey, (val) => {
  clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => { debouncedKey.value = val }, 200)
})

// reset to page 1 whenever filters change
watch([filterShape, filterOp, debouncedKey], () => { currentPage.value = 1 })

const filteredEvents = computed(() => {
  return store.events.filter((ev) => {
    if (filterShape.value !== 'All' && ev.shape !== filterShape.value) return false
    if (filterOp.value !== 'All' && ev.op !== filterOp.value) return false
    if (debouncedKey.value && !ev.key?.includes(debouncedKey.value)) return false
    return true
  })
})
</script>

<template>
  <section class="lab-panel">
    <h3>KV Watch Stream</h3>
    <p class="lab-muted description">
      Raw change feed for <code>{{ store.context }}</code>: KV watch → SSE → this Pinia store. The
      panels above are projections of these events — same pattern as the server side.
    </p>

    <div class="filters">
      <div class="filter-group">
        <label class="lab-muted filter-label">Shape</label>
        <SelectButton v-model="filterShape" :options="SHAPE_OPTIONS" size="small" />
      </div>
      <div class="filter-group">
        <label class="lab-muted filter-label">Op</label>
        <SelectButton v-model="filterOp" :options="OP_OPTIONS" size="small" />
      </div>
      <div class="filter-group filter-group--grow">
        <label class="lab-muted filter-label">Key</label>
        <InputText v-model="filterKey" size="small" placeholder="filter by key…" class="key-input" />
      </div>
    </div>

    <DataTable
      :value="filteredEvents"
      size="small"
      :rows="8"
      paginator
      :first="(currentPage - 1) * 8"
      @page="currentPage = $event.page + 1"
    >
      <template #empty>
        <span class="lab-muted">No events match the current filter.</span>
      </template>
      <Column field="at" header="Time" style="width: 8rem" />
      <Column field="shape" header="Shape" style="width: 5rem" />
      <Column header="Op" style="width: 7rem">
        <template #body="{ data }">
          <Tag :severity="OP_SEVERITY[data.op] ?? 'info'" :value="data.op" />
        </template>
      </Column>
      <Column field="key" header="Key" />
      <Column field="revision" header="Rev" style="width: 5rem" />
      <Column header="Value">
        <template #body="{ data }">
          <code class="value">{{ data.value ? JSON.stringify(data.value) : '' }}</code>
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.description {
  margin: 0 0 0.75rem;
  font-size: 0.85rem;
}
.filters {
  display: flex;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
  margin-bottom: 0.5rem;
}
.filter-group {
  display: flex;
  align-items: center;
  gap: 0.375rem;
}
.filter-group--grow {
  flex: 1;
  min-width: 160px;
}
.filter-label {
  font-size: 11px;
  white-space: nowrap;
}
.key-input {
  width: 100%;
}
.value {
  font-size: 0.75rem;
  word-break: break-all;
}
</style>
