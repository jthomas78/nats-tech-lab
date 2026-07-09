<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Divider from 'primevue/divider'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref } from 'vue'

import { arrivePort, departPort, unloadContainer } from '../api'
import { usePortStore } from '../stores/port'

const store = usePortStore()
const toast = useToast()

function manifestSummary(ship) {
  const manifest = store.manifestFor(ship.shipID)
  if (manifest.length === 0) return '—'
  return manifest.map((c) => `${c.containerID} (${c.cargo} → ${c.destPort})`).join(', ')
}

const dockedShipOptions = computed(() => store.dockedShips.map((s) => s.shipID))

// ── Ship arrives ───────────────────────────────────────────────────────────────

const arriveForm = reactive({ shipID: '', shipName: '' })
const arriveBusy = ref(false)
const arriveError = ref('')

async function submitArrive() {
  arriveError.value = ''
  arriveBusy.value = true
  try {
    await arrivePort({
      context: store.context,
      shipID: arriveForm.shipID.trim(),
      shipName: arriveForm.shipName.trim() || arriveForm.shipID.trim(),
      port: store.port,
    })
    toast.add({ severity: 'success', summary: 'Ship arrived', detail: arriveForm.shipID, life: 2500 })
    arriveForm.shipID = ''
    arriveForm.shipName = ''
  } catch (err) {
    arriveError.value = err.message
  } finally {
    arriveBusy.value = false
  }
}

// ── Ship departs ───────────────────────────────────────────────────────────────

const departShipID = ref('')
const departBusy = ref(false)
const departError = ref('')

async function submitDepart() {
  departError.value = ''
  departBusy.value = true
  try {
    await departPort({ context: store.context, shipID: departShipID.value, port: store.port })
    toast.add({ severity: 'success', summary: 'Ship departed', detail: departShipID.value, life: 2500 })
    departShipID.value = ''
  } catch (err) {
    departError.value = err.message
  } finally {
    departBusy.value = false
  }
}

// ── Unload container ───────────────────────────────────────────────────────────

const unloadForm = reactive({ shipID: '', containerID: '' })
const unloadBusy = ref(false)
const unloadError = ref('')

const unloadManifestOptions = computed(() =>
  unloadForm.shipID ? store.manifestFor(unloadForm.shipID).map((c) => c.containerID) : [],
)

async function submitUnload() {
  unloadError.value = ''
  unloadBusy.value = true
  try {
    await unloadContainer({ context: store.context, containerID: unloadForm.containerID, shipID: unloadForm.shipID })
    toast.add({ severity: 'success', summary: 'Container unloaded', detail: unloadForm.containerID, life: 2500 })
    unloadForm.containerID = ''
  } catch (err) {
    unloadError.value = err.message
  } finally {
    unloadBusy.value = false
  }
}
</script>

<template>
  <section class="lab-panel">
    <h3>Ships at Port — {{ store.port || '—' }}</h3>

    <div class="ops">
      <div class="op-row">
        <InputText v-model.trim="arriveForm.shipID" placeholder="ship ID, e.g. orient-express" size="small" />
        <InputText v-model.trim="arriveForm.shipName" placeholder="ship name (first arrival only)" size="small" />
        <Button label="Arrive" size="small" :disabled="arriveBusy || !arriveForm.shipID" :loading="arriveBusy" @click="submitArrive" />
      </div>
      <div v-if="arriveError" class="domain-error">{{ arriveError }}</div>

      <Divider />

      <div class="op-row">
        <Select v-model="departShipID" :options="dockedShipOptions" placeholder="docked ship" size="small" />
        <Button label="Depart" size="small" :disabled="departBusy || !departShipID" :loading="departBusy" @click="submitDepart" />
      </div>
      <div v-if="departError" class="domain-error">{{ departError }}</div>

      <Divider />

      <div class="op-row">
        <Select v-model="unloadForm.shipID" :options="dockedShipOptions" placeholder="docked ship" size="small" />
        <Select
          v-model="unloadForm.containerID"
          :options="unloadManifestOptions"
          placeholder="container on ship"
          size="small"
          style="width:200px"
        />
        <Button
          label="Unload"
          size="small"
          :disabled="unloadBusy || !unloadForm.shipID || !unloadForm.containerID"
          :loading="unloadBusy"
          @click="submitUnload"
        />
      </div>
      <div v-if="unloadError" class="domain-error">{{ unloadError }}</div>
    </div>

    <DataTable :value="store.dockedShips" size="small" data-key="shipID" resizableColumns columnResizeMode="expand">
      <template #empty>
        <span class="lab-muted">No ships docked here — send an arrival above.</span>
      </template>
      <Column field="shipID" header="Ship ID" style="font-family:monospace;font-size:12px" />
      <Column field="shipName" header="Name" />
      <Column header="Status" style="width:100px">
        <template #body>
          <Tag severity="success" value="Docked" />
        </template>
      </Column>
      <Column header="Manifest">
        <template #body="{ data }">
          <span class="manifest-cell">{{ manifestSummary(data) }}</span>
        </template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.ops {
  margin-bottom: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.op-row {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
  align-items: center;
}
.domain-error {
  font-size: 0.85rem;
  color: var(--p-red-400, #f87171);
  background: rgba(248, 113, 113, 0.08);
  border: 1px solid rgba(248, 113, 113, 0.25);
  border-radius: 4px;
  padding: 0.35rem 0.6rem;
}
.manifest-cell {
  font-size: 12px;
  font-family: monospace;
}
</style>
