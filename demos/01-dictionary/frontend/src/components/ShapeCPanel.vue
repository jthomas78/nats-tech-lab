<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { onMounted, ref } from 'vue'

import { getFleet } from '../api'

const fleet = ref([])
const loading = ref(false)
const lastReconstructed = ref(null)
const collapsed = ref(false)

async function reconstruct() {
  loading.value = true
  try {
    const res = await getFleet()
    fleet.value = res?.fleet ?? []
    lastReconstructed.value = new Date().toLocaleTimeString()
  } catch {
    fleet.value = []
  } finally {
    loading.value = false
  }
}

onMounted(reconstruct)

function statusLabel(ship) {
  return ship.currentPort ? 'Docked' : 'In transit'
}
function statusSeverity(ship) {
  return ship.currentPort ? 'success' : 'secondary'
}
function portLabel(ship) {
  return ship.currentPort || '—'
}

function cargoSummary(ship) {
  if (!ship.cargo || ship.cargo.length === 0) return '—'
  return ship.cargo.map(c => `${c.description} ×${c.units}`).join(', ')
}
</script>

<template>
  <section class="lab-panel shape-c-panel">
    <div class="panel-header" @click="collapsed = !collapsed">
      <div class="panel-header-left">
        <span class="collapse-icon">{{ collapsed ? '▶' : '▼' }}</span>
        <span class="panel-title">Shape C — Event Sourcing Reconstruction</span>
      </div>
      <div v-if="!collapsed" class="header-actions" @click.stop>
        <span v-if="lastReconstructed" class="lab-muted ts">
          reconstructed {{ lastReconstructed }}
        </span>
        <Button
          label="Reconstruct"
          size="small"
          text
          :loading="loading"
          @click="reconstruct"
        />
      </div>
    </div>

    <template v-if="!collapsed">
      <p class="description">
        No KV, no Postgres. Current fleet state is derived entirely from replaying
        <code>DICTIONARY.*</code> from <code>seq=1</code> — demonstrating Fowler's Event Sourcing property.
        Clear KV / Postgres, click Reconstruct: the correct fleet still appears.
      </p>

      <DataTable
        :value="fleet"
        size="small"
        data-key="shipID"
        :loading="loading"
        resizableColumns
        columnResizeMode="expand"
      >
        <template #empty>
          <span class="lab-muted">No events in the stream yet — publish a ship command first.</span>
        </template>
        <Column field="shipID" header="Ship ID" style="font-family:monospace;font-size:12px" />
        <Column field="shipName" header="Name" />
        <Column header="Status" style="width:100px">
          <template #body="{ data }">
            <Tag :severity="statusSeverity(data)" :value="statusLabel(data)" />
          </template>
        </Column>
        <Column header="Port" style="width:120px">
          <template #body="{ data }">
            <span>{{ portLabel(data) }}</span>
          </template>
        </Column>
        <Column header="Cargo manifest">
          <template #body="{ data }">
            <span class="cargo-cell">{{ cargoSummary(data) }}</span>
          </template>
        </Column>
      </DataTable>
    </template>
  </section>
</template>

<style scoped>
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  cursor: pointer;
  user-select: none;
}
.panel-header-left {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.collapse-icon {
  font-size: 9px;
  color: var(--p-text-muted-color);
  width: 10px;
}
.panel-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--p-text-color);
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.ts {
  font-size: 11px;
}
.description {
  margin: 0.25rem 0 0.75rem;
  font-size: 0.85rem;
  color: var(--p-text-color);
}
.cargo-cell {
  font-size: 13px;
  color: var(--p-text-color);
}
</style>
