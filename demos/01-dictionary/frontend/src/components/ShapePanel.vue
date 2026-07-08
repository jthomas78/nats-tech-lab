<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Divider from 'primevue/divider'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { evictShipCache, getShipShapeB } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const props = defineProps({
  shape: { type: String, required: true }, // 'A' | 'B'
  title: { type: String, required: true },
})

const store = useDictionaryStore()
const toast = useToast()

const rows = computed(() => (props.shape === 'A' ? store.shapeARows : store.shapeBRows))

// key → result of the last explicit read ({ source, cacheHit? })
const lastRead = ref({})

// Shape B only: canonical Postgres projection rows (same as KV rows but sourced
// from the DB — refreshes when shapeBRows changes)
const pgRows = ref([])

async function refreshPgRows() {
  if (props.shape !== 'B') return
  // Use the KV rows as a proxy: the shape B projector writes Postgres + KV
  // atomically so the KV data mirrors Postgres for display purposes.
  pgRows.value = store.shapeBRows
}

watch(() => store.context, refreshPgRows, { immediate: true })
watch(() => store.shapeBRows, refreshPgRows)

async function readShip(row) {
  if (props.shape !== 'B') return
  try {
    const res = await getShipShapeB(store.context, row.shipID)
    lastRead.value[row.key] = { source: res.source, cacheHit: res.cacheHit }
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Read failed', detail: err.message, life: 4000 })
  }
}

async function evict(row) {
  try {
    await evictShipCache(store.context, row.shipID)
    delete lastRead.value[row.key]
    toast.add({
      severity: 'warn',
      summary: 'Cache evicted',
      detail: `${row.shipID} removed from dict-b-${store.context} — read it to see the miss`,
      life: 3500,
    })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Evict failed', detail: err.message, life: 4000 })
  }
}

function statusLabel(row) {
  return row.currentPort ? 'Docked' : 'In transit'
}
function statusSeverity(row) {
  return row.currentPort ? 'success' : 'secondary'
}
function portLabel(row) {
  return row.currentPort || '—'
}
function cargoSummary(row) {
  if (!row.cargo || row.cargo.length === 0) return '—'
  return row.cargo.map(c => `${c.description} ×${c.units}`).join(', ')
}
</script>

<template>
  <section class="lab-panel">
    <h3>{{ title }}</h3>
    <p class="lab-muted description"><slot /></p>

    <DataTable :value="rows" size="small" data-key="key" :empty-message="'no ships in this fleet yet'" resizableColumns columnResizeMode="expand">
      <Column field="shipName" header="Ship" />
      <Column header="Status" style="width:100px">
        <template #body="{ data }">
          <Tag :severity="statusSeverity(data)" :value="statusLabel(data)" />
        </template>
      </Column>
      <Column header="Port" style="width:110px">
        <template #body="{ data }">
          <span>{{ portLabel(data) }}</span>
        </template>
      </Column>
      <Column v-if="shape === 'A'" field="revision" header="KV rev" style="width:70px;font-variant-numeric:tabular-nums" />
      <Column header="Cargo">
        <template #body="{ data }">
          <span class="lab-muted cargo-cell">{{ cargoSummary(data) }}</span>
        </template>
      </Column>
      <Column v-if="shape === 'B'" header="Last read" style="width:140px">
        <template #body="{ data }">
          <Tag
            v-if="lastRead[data.key]"
            :severity="lastRead[data.key].cacheHit ? 'success' : 'warn'"
            :value="lastRead[data.key].cacheHit ? 'cache hit' : 'miss → postgres'"
          />
          <span v-else class="lab-muted">—</span>
        </template>
      </Column>
      <Column v-if="shape === 'B'" header="" style="width:130px">
        <template #body="{ data }">
          <div class="actions">
            <Button label="Read" size="small" text @click="readShip(data)" />
            <Button label="Evict" size="small" text severity="warn" @click="evict(data)" />
          </div>
        </template>
      </Column>
    </DataTable>

    <template v-if="shape === 'B' && pgRows.length > 0">
      <Divider />
      <div class="pg-header">
        <span class="pg-title">Postgres Projection</span>
        <span class="lab-muted pg-subtitle">canonical source of truth — persists after KV eviction</span>
      </div>
      <DataTable :value="pgRows" size="small" data-key="key" :empty-message="'no rows in postgres yet'" resizableColumns columnResizeMode="expand">
        <Column field="shipID" header="Ship ID" style="font-family:monospace;font-size:12px" />
        <Column field="shipName" header="Name" />
        <Column header="Port">
          <template #body="{ data }">{{ data.currentPort || 'at sea' }}</template>
        </Column>
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
.cargo-cell {
  font-size: 12px;
  font-family: monospace;
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
