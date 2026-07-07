<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Divider from 'primevue/divider'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { evictShapeBCache, getShapeA, getShapeB, listShapeB } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const props = defineProps({
  shape: { type: String, required: true }, // 'A' | 'B'
  title: { type: String, required: true },
})

const store = useDictionaryStore()
const toast = useToast()

const rows = computed(() => (props.shape === 'A' ? store.shapeARows : store.shapeBRows))

// key → result of the last explicit read ({ source, cacheHit?, revision? })
const lastRead = ref({})

// Shape B only: canonical Postgres projection rows
const pgRows = ref([])

async function refreshPgRows() {
  if (props.shape !== 'B') return
  try {
    const res = await listShapeB(store.context)
    pgRows.value = res?.entries ?? []
  } catch {
    // silently ignore — Postgres may not be running in dev
  }
}

watch(() => store.context, refreshPgRows, { immediate: true })
watch(() => store.shapeBRows, refreshPgRows)

async function readEntry(row) {
  try {
    if (props.shape === 'A') {
      const res = await getShapeA(store.context, row.entityType, row.id)
      lastRead.value[row.key] = { source: res.source, revision: res.revision }
    } else {
      const res = await getShapeB(store.context, row.entityType, row.id)
      lastRead.value[row.key] = { source: res.source, cacheHit: res.cacheHit }
    }
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Read failed', detail: err.message, life: 4000 })
  }
}

async function evict(row) {
  try {
    await evictShapeBCache(store.context, row.entityType, row.id)
    delete lastRead.value[row.key]
    toast.add({
      severity: 'warn',
      summary: 'Cache evicted',
      detail: `${row.key} removed from dict-b-${store.context} — read it to see the miss`,
      life: 3500,
    })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Evict failed', detail: err.message, life: 4000 })
  }
}
</script>

<template>
  <section class="lab-panel">
    <h3>{{ title }}</h3>
    <p class="lab-muted description"><slot /></p>
    <DataTable :value="rows" size="small" data-key="key" :empty-message="'no entries in this context yet'">
      <Column field="key" header="KV key" />
      <Column field="label" header="Label" />
      <Column v-if="shape === 'A'" field="revision" header="KV rev" />
      <Column v-if="shape === 'B'" field="version" header="PG version" />
      <Column header="Last read">
        <template #body="{ data }">
          <template v-if="lastRead[data.key]">
            <Tag
              v-if="shape === 'B'"
              :severity="lastRead[data.key].cacheHit ? 'success' : 'warn'"
              :value="lastRead[data.key].cacheHit ? 'cache hit' : 'miss → postgres'"
            />
            <Tag v-else severity="info" :value="`kv rev ${lastRead[data.key].revision}`" />
          </template>
          <span v-else class="lab-muted">—</span>
        </template>
      </Column>
      <Column header="">
        <template #body="{ data }">
          <div class="actions">
            <Button label="Read" size="small" text @click="readEntry(data)" />
            <Button
              v-if="shape === 'B'"
              label="Evict"
              size="small"
              text
              severity="warn"
              @click="evict(data)"
            />
          </div>
        </template>
      </Column>
    </DataTable>

    <template v-if="shape === 'B'">
      <Divider />
      <div class="pg-header">
        <span class="pg-title">Postgres Projection</span>
        <span class="lab-muted pg-subtitle">canonical source of truth — persists after KV eviction</span>
      </div>
      <DataTable :value="pgRows" size="small" data-key="key" :empty-message="'no rows in postgres yet'">
        <Column field="key" header="Key" />
        <Column field="label" header="Label" />
        <Column field="version" header="PG version" style="font-variant-numeric:tabular-nums" />
      </DataTable>
    </template>
  </section>
</template>

<style scoped>
.description {
  margin: 0 0 0.75rem;
  font-size: 0.85rem;
  min-height: 3.4em;
}
.actions {
  display: flex;
  gap: 0.25rem;
  justify-content: flex-end;
}
.pg-header {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  margin-bottom: 0.375rem;
}
.pg-title {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-muted-color);
}
.pg-subtitle {
  font-size: 11px;
}
</style>
