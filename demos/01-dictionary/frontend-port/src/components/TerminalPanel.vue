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

// BR-016: TCKU + 7 digits (case-sensitive), mirrors the domain check in
// ContainerAggregate.Register(). Client-side only for fast feedback — the
// backend is the source of truth and re-validates on submit.
const CONTAINER_ID_PATTERN = /^TCKU[0-9]{7}$/
const containerIDValid = computed(() => CONTAINER_ID_PATTERN.test(registerForm.containerID))

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

// Yard containers split by destination: outbound still needs to travel from
// here; arrived means terminalPort == destPort (BR-008/BR-009 territory —
// the domain has no separate "delivered" status, this is a client-side view
// of the same in-terminal containers). Only outbound containers are offered
// for loading — loading an arrived container is always rejected by BR-008.
const outboundContainers = computed(() => store.yardContainers.filter((c) => c.destPort !== store.port))
const arrivedContainers = computed(() => store.yardContainers.filter((c) => c.destPort === store.port))

const yardContainerOptions = computed(() => outboundContainers.value.map((c) => c.containerID))
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

    <h4>Outbound</h4>
    <DataTable :value="outboundContainers" size="small" data-key="containerID" resizableColumns columnResizeMode="expand">
      <template #empty>
        <span class="lab-muted">No outbound containers in this yard — register one above.</span>
      </template>
      <Column field="containerID" header="Container" style="font-family:monospace;font-size:12px" />
      <Column field="cargo" header="Cargo" />
      <Column field="originPort" header="Origin" style="width:110px" />
      <Column header="Destination" style="width:130px">
        <template #body="{ data }">
          <Tag severity="info" :value="data.destPort" />
        </template>
      </Column>
    </DataTable>

    <h4>Arrived</h4>
    <DataTable :value="arrivedContainers" size="small" data-key="containerID" resizableColumns columnResizeMode="expand">
      <template #empty>
        <span class="lab-muted">No containers have arrived at their destination here.</span>
      </template>
      <Column field="containerID" header="Container" style="font-family:monospace;font-size:12px" />
      <Column field="cargo" header="Cargo" />
      <Column field="originPort" header="Origin" style="width:110px" />
      <Column header="Destination" style="width:130px">
        <template #body="{ data }">
          <Tag severity="success" :value="data.destPort" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="registerVisible" header="Register container" modal style="width:26rem">
      <div class="dialog-fields">
        <InputText v-model.trim="registerForm.containerID" placeholder="container ID, e.g. TCKU1234567" size="small" />
        <span v-if="registerForm.containerID && !containerIDValid" class="format-hint">
          Must be TCKU followed by 7 digits, e.g. TCKU1234567
        </span>
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
          :disabled="registerBusy || !containerIDValid || !registerForm.cargo || !registerForm.destPort"
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
.format-hint {
  margin-top: -0.35rem;
  font-size: 0.75rem;
  color: var(--p-red-400, #f87171);
}
h4 {
  margin: 0.75rem 0 0.35rem;
  font-size: 11px;
  line-height: 16px;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--p-text-muted-color);
}
h4:first-of-type {
  margin-top: 0;
}
</style>
