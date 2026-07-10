<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Divider from 'primevue/divider'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref, watch } from 'vue'

import { arrivePort, departPort, unloadContainer } from '../api'
import { usePortStore } from '../stores/port'

const store = usePortStore()
const toast = useToast()

const expandedShips = ref({})

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
// Unload is inline on each manifest row (not a separate ship/container
// picker) — the row already carries both, and destPort tells us whether the
// action is even legal here.

const unloadBusyID = ref('')
const unloadErrorByShip = reactive({})

async function submitUnload(shipID, containerID) {
  delete unloadErrorByShip[shipID]
  unloadBusyID.value = containerID
  try {
    await unloadContainer({ context: store.context, containerID, shipID })
    toast.add({ severity: 'success', summary: 'Container unloaded', detail: containerID, life: 2500 })
  } catch (err) {
    unloadErrorByShip[shipID] = err.message
  } finally {
    unloadBusyID.value = ''
  }
}

// Docked-ship select only ever lists ships at the current port, but the
// underlying v-model value is a plain string — switching ports doesn't clear
// a stale selection just because it drops out of the option list, so the
// depart form must be reset explicitly or a stale ship can still pass the
// "field set" enablement check.
watch(
  () => store.port,
  () => {
    departShipID.value = ''
    departError.value = ''
    for (const key of Object.keys(unloadErrorByShip)) delete unloadErrorByShip[key]
  },
)
</script>

<template>
  <section class="lab-panel">
    <h3>Ships at Port — {{ store.port || '—' }}</h3>

    <div v-if="!store.port" class="lab-muted no-port">
      Select or add a port to move ships in and out.
    </div>
    <div v-else class="ops">
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
    </div>

    <DataTable
      v-model:expandedRows="expandedShips"
      :value="store.dockedShips"
      size="small"
      data-key="shipID"
      resizableColumns
      columnResizeMode="expand"
    >
      <template #empty>
        <span class="lab-muted">No ships docked here — send an arrival above.</span>
      </template>
      <Column expander style="width:2.5rem" />
      <Column field="shipID" header="Ship ID" style="font-family:monospace;font-size:12px" />
      <Column field="shipName" header="Name" />
      <Column header="Status" style="width:100px">
        <template #body>
          <Tag severity="success" value="Docked" />
        </template>
      </Column>
      <Column header="Manifest" style="width:100px">
        <template #body="{ data }">
          <span class="lab-muted manifest-count">{{ store.manifestFor(data.shipID).length }} container(s)</span>
        </template>
      </Column>

      <template #expansion="{ data }">
        <DataTable :value="store.manifestFor(data.shipID)" size="small" data-key="containerID" class="manifest-table">
          <template #empty>
            <span class="lab-muted">No containers on this ship.</span>
          </template>
          <Column field="containerID" header="Container" style="font-family:monospace;font-size:12px" />
          <Column field="cargo" header="Cargo" />
          <Column field="originPort" header="Origin" style="width:110px" />
          <Column header="Destination" style="width:130px">
            <template #body="{ data: container }">
              <Tag :severity="container.destPort === store.port ? 'success' : 'info'" :value="container.destPort" />
            </template>
          </Column>
          <Column header="" style="width:100px">
            <template #body="{ data: container }">
              <Button
                label="Unload"
                size="small"
                :disabled="container.destPort !== store.port || unloadBusyID === container.containerID"
                :loading="unloadBusyID === container.containerID"
                @click="submitUnload(data.shipID, container.containerID)"
              />
            </template>
          </Column>
        </DataTable>
        <div v-if="unloadErrorByShip[data.shipID]" class="domain-error manifest-error">
          {{ unloadErrorByShip[data.shipID] }}
        </div>
      </template>
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
.no-port {
  margin-bottom: 0.75rem;
  font-size: 0.85rem;
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
.manifest-count {
  font-size: 12px;
}
.manifest-table {
  margin: 0.25rem 0 0.5rem 2.5rem;
  border-left: 2px solid var(--lab-accent);
  border-radius: 3px;
  --p-datatable-header-cell-background: color-mix(in srgb, var(--lab-panel-bg) 90%, var(--lab-accent) 10%);
  --p-datatable-row-background: color-mix(in srgb, var(--lab-panel-bg) 96%, var(--lab-accent) 4%);
}
.manifest-error {
  margin: 0 0 0.5rem 2.5rem;
}
</style>
