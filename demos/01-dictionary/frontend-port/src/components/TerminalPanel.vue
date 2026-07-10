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

import { loadContainer, registerContainer } from '../api'
import { usePortStore } from '../stores/port'

const store = usePortStore()
const toast = useToast()

// ── Register container (popup) ────────────────────────────────────────────────

const registerVisible = ref(false)
const registerBusy = ref(false)
const registerError = ref('')
const registerForm = reactive({ containerID: '', cargo: '', destPort: '' })

const destPortOptions = computed(() => store.knownPorts.filter((p) => p !== store.port))

function openRegister() {
  registerForm.containerID = ''
  registerForm.cargo = ''
  registerForm.destPort = ''
  registerError.value = ''
  registerVisible.value = true
}

async function submitRegister() {
  registerError.value = ''
  registerBusy.value = true
  try {
    await registerContainer({
      context: store.context,
      containerID: registerForm.containerID.trim(),
      cargo: registerForm.cargo.trim(),
      originPort: store.port,
      destPort: registerForm.destPort,
    })
    toast.add({ severity: 'success', summary: 'Container registered', detail: registerForm.containerID, life: 2500 })
    registerVisible.value = false
  } catch (err) {
    registerError.value = err.message
  } finally {
    registerBusy.value = false
  }
}

// ── Load container onto a docked ship ─────────────────────────────────────────

const loadForm = reactive({ containerID: '', shipID: '' })
const loadBusy = ref(false)
const loadError = ref('')

const yardContainerOptions = computed(() => store.yardContainers.map((c) => c.containerID))
const dockedShipOptions = computed(() => store.dockedShips.map((s) => s.shipID))

async function submitLoad() {
  loadError.value = ''
  loadBusy.value = true
  try {
    await loadContainer({ context: store.context, containerID: loadForm.containerID, shipID: loadForm.shipID })
    toast.add({ severity: 'success', summary: 'Container loaded', detail: `${loadForm.containerID} → ${loadForm.shipID}`, life: 2500 })
    loadForm.containerID = ''
    loadForm.shipID = ''
  } catch (err) {
    loadError.value = err.message
  } finally {
    loadBusy.value = false
  }
}
</script>

<template>
  <section class="lab-panel">
    <h3>Terminal Yard — {{ store.port || '—' }}</h3>

    <div v-if="!store.port" class="lab-muted no-port">
      Select or add a port to register and load containers.
    </div>
    <div v-else class="ops">
      <Button label="Register container" icon="pi pi-plus" size="small" outlined @click="openRegister" />

      <div class="op-row">
        <Select
          v-model="loadForm.containerID"
          :options="yardContainerOptions"
          placeholder="container in yard"
          size="small"
          style="width:200px"
        />
        <Select
          v-model="loadForm.shipID"
          :options="dockedShipOptions"
          placeholder="docked ship"
          size="small"
        />
        <Button
          label="Load"
          size="small"
          :disabled="loadBusy || !loadForm.containerID || !loadForm.shipID"
          :loading="loadBusy"
          @click="submitLoad"
        />
      </div>
      <div v-if="loadError" class="domain-error">{{ loadError }}</div>
    </div>

    <DataTable :value="store.yardContainers" size="small" data-key="containerID" resizableColumns columnResizeMode="expand">
      <template #empty>
        <span class="lab-muted">No containers in this yard — register one above.</span>
      </template>
      <Column field="containerID" header="Container" style="font-family:monospace;font-size:12px" />
      <Column field="cargo" header="Cargo" />
      <Column field="originPort" header="Origin" style="width:110px" />
      <Column header="Destination" style="width:130px">
        <template #body="{ data }">
          <Tag :severity="data.destPort === store.port ? 'success' : 'info'" :value="data.destPort" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="registerVisible" header="Register container" modal style="width:26rem">
      <div class="dialog-fields">
        <InputText v-model.trim="registerForm.containerID" placeholder="container ID, e.g. TCKU1234567" size="small" />
        <InputText v-model.trim="registerForm.cargo" placeholder="cargo, e.g. Electronics" size="small" />
        <Select v-model="registerForm.destPort" :options="destPortOptions" placeholder="destination port" editable size="small" />
        <span class="lab-muted">Origin terminal: <code>{{ store.port }}</code></span>
      </div>
      <div v-if="registerError" class="domain-error">{{ registerError }}</div>
      <template #footer>
        <Button label="Cancel" text size="small" @click="registerVisible = false" />
        <Button
          label="Register"
          size="small"
          :disabled="registerBusy || !registerForm.containerID || !registerForm.cargo || !registerForm.destPort"
          :loading="registerBusy"
          @click="submitRegister"
        />
      </template>
    </Dialog>
  </section>
</template>

<style scoped>
.ops {
  margin-bottom: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
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
.dialog-fields {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
</style>
