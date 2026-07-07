<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'

import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()

const OP_SEVERITY = { PUT: 'success', DEL: 'warn', PURGE: 'danger' }
</script>

<template>
  <section class="lab-panel">
    <h3>KV watch stream</h3>
    <p class="lab-muted description">
      Raw change feed for <code>{{ store.context }}</code>: KV watch → SSE → this Pinia store. The
      panels above are projections of these events — same pattern as the server side.
    </p>
    <DataTable :value="store.events" size="small" :rows="8" paginator>
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
.value {
  font-size: 0.75rem;
  word-break: break-all;
}
</style>
