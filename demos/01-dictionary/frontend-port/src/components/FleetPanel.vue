<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { computed, ref } from 'vue'

import { usePortStore } from '../stores/port'

// Fleet-wide, read-only view of every ship in the context, regardless of the
// selected port. A status filter narrows it to docked or in-transit ships;
// 'all' is the default. Docked-vs-in-transit is derived from currentPort
// ('' == at sea). A ship still carries its loaded containers while at sea (the
// onShipID join survives departure), so the manifest count stays meaningful.
const store = usePortStore()

const STATUS_FILTERS = [
  { label: 'All', value: 'all' },
  { label: 'Docked', value: 'docked' },
  { label: 'In transit', value: 'in-transit' },
]
const statusFilter = ref('all')

const filteredShips = computed(() => {
  if (statusFilter.value === 'docked') return store.allShips.filter((s) => s.currentPort !== '')
  if (statusFilter.value === 'in-transit') return store.allShips.filter((s) => s.currentPort === '')
  return store.allShips
})
</script>

<template>
  <section class="lab-panel">
    <div class="fleet-head">
      <h3>Fleet</h3>
      <div class="fleet-head-controls">
        <label class="lab-muted" for="fleet-status">Status</label>
        <Select
          id="fleet-status"
          v-model="statusFilter"
          :options="STATUS_FILTERS"
          option-label="label"
          option-value="value"
          size="small"
        />
      </div>
    </div>

    <DataTable :value="filteredShips" size="small" data-key="shipID">
      <template #empty>
        <span class="lab-muted">No ships match this filter.</span>
      </template>
      <Column field="shipID" header="Ship ID" style="font-family:monospace;font-size:12px" />
      <Column field="shipName" header="Name" />
      <Column header="Status" style="width:110px">
        <template #body="{ data }">
          <Tag
            :severity="data.currentPort ? 'success' : 'info'"
            :value="data.currentPort ? 'Docked' : 'In transit'"
          />
        </template>
      </Column>
      <Column header="Port" style="width:140px">
        <template #body="{ data }">
          <span :class="data.currentPort ? '' : 'lab-muted'">{{ data.currentPort || 'at sea' }}</span>
        </template>
      </Column>
      <Column header="Manifest" style="width:100px">
        <template #body="{ data }">
          <span class="lab-muted manifest-count">{{ store.manifestFor(data.shipID).length }} container(s)</span>
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.fleet-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.fleet-head h3 {
  margin: 0;
}
.fleet-head-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.manifest-count {
  font-size: 12px;
}
</style>
