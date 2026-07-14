<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref } from 'vue'

import { arrivePort } from '../api'
import { usePortStore } from '../stores/port'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'

// Fleet-wide, read-only view of every ship in the context, regardless of the
// selected port. A status filter narrows it to docked or in-transit ships;
// 'all' is the default. Docked-vs-in-transit is derived from currentPort
// ('' == at sea). A ship still carries its loaded containers while at sea (the
// onShipID join survives departure), so the manifest count stays meaningful.
const store = usePortStore()
const toast = useToast()
const { statusLabel } = useRefdataLabels()

// Filter options — the docked/in-transit labels resolve from refdata (so they
// re-translate with the locale switcher); "All" is a filter meta-option with
// no reference-data code, so it stays as-is.
const statusFilters = computed(() => [
  { label: 'All', value: 'all' },
  { label: statusLabel('docked', 'Docked'), value: 'docked' },
  { label: statusLabel('in-transit', 'In transit'), value: 'in-transit' },
])
const statusFilter = ref('all')

const filteredShips = computed(() => {
  if (statusFilter.value === 'docked') return store.allShips.filter((s) => s.currentPort !== '')
  if (statusFilter.value === 'in-transit') return store.allShips.filter((s) => s.currentPort === '')
  return store.allShips
})

// ── Register a new ship (popup) ─────────────────────────────────────────────
// Registration IS a ship's first arrival — the domain has no separate
// "register ship" command, so this dialog captures the same fields
// ShipsAtPortPanel's old Arrive form did (ship ID, name, port) and calls the
// same arrivePort command. Fleet-scoped, not gated on the selected port,
// since a new ship can arrive anywhere in the fleet.

const registerVisible = ref(false)
const registerBusy = ref(false)
const registerError = ref('')
const registerForm = reactive({ shipID: '', shipName: '', port: '' })

function openRegister() {
  registerForm.shipID = ''
  registerForm.shipName = ''
  registerForm.port = ''
  registerError.value = ''
  registerVisible.value = true
}

async function submitRegister() {
  registerError.value = ''
  registerBusy.value = true
  try {
    await arrivePort({
      context: store.context,
      shipID: registerForm.shipID.trim(),
      shipName: registerForm.shipName.trim() || registerForm.shipID.trim(),
      port: registerForm.port,
    })
    toast.add({ severity: 'success', summary: 'Ship registered', detail: registerForm.shipID, life: 2500 })
    registerVisible.value = false
  } catch (err) {
    registerError.value = err.message
  } finally {
    registerBusy.value = false
  }
}
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
          :options="statusFilters"
          option-label="label"
          option-value="value"
          size="small"
        />
        <Button
          icon="pi pi-plus"
          aria-label="Register a new ship"
          text
          rounded
          size="small"
          @click="openRegister"
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
            :value="statusLabel(data.status, data.currentPort ? 'Docked' : 'In transit')"
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

    <Dialog v-model:visible="registerVisible" header="Register ship" modal style="width:26rem">
      <div class="dialog-fields">
        <InputText v-model.trim="registerForm.shipID" placeholder="ship ID, e.g. orient-express" size="small" />
        <InputText v-model.trim="registerForm.shipName" placeholder="ship name, e.g. Orient Express" size="small" />
        <Select v-model="registerForm.port" :options="store.knownPorts" placeholder="arrival port" size="small" />
      </div>
      <div v-if="registerError" class="domain-error">{{ registerError }}</div>
      <template #footer>
        <Button label="Cancel" text size="small" @click="registerVisible = false" />
        <Button
          label="Register"
          size="small"
          :disabled="registerBusy || !registerForm.shipID || !registerForm.port"
          :loading="registerBusy"
          @click="submitRegister"
        />
      </template>
    </Dialog>
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
.dialog-fields {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.domain-error {
  font-size: 0.85rem;
  color: var(--p-red-400, #f87171);
  background: rgba(248, 113, 113, 0.08);
  border: 1px solid rgba(248, 113, 113, 0.25);
  border-radius: 4px;
  padding: 0.35rem 0.6rem;
}
</style>
