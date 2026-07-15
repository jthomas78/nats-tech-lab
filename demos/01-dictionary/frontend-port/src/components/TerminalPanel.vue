<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputGroup from 'primevue/inputgroup'
import InputGroupAddon from 'primevue/inputgroupaddon'
import InputText from 'primevue/inputtext'
import Menu from 'primevue/menu'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { useI18n } from 'vue-i18n'
import { computed, reactive, ref } from 'vue'

import { loadContainer, registerContainer } from '../api'
import { usePortStore } from '../stores/port'

const store = usePortStore()
const toast = useToast()
const { t } = useI18n()

// ── Register container (popup) ────────────────────────────────────────────────

const registerVisible = ref(false)
const registerBusy = ref(false)
const registerError = ref('')
const registerForm = reactive({ cargo: '', destPort: '' })

const destPortOptions = computed(() => store.knownPorts.filter((p) => p !== store.port))

// BR-016: TCKU + 7 digits (case-sensitive), mirrors the domain check in
// ContainerAggregate.Register(). The TCKU prefix is fixed and shown as a
// read-only addon — the user only ever types the 7-digit suffix, so an
// invalid prefix is not something they can enter here. Client-side only for
// fast feedback — the backend is the source of truth and re-validates on submit.
const CONTAINER_ID_PREFIX = 'TCKU'
const containerSuffix = ref('')
const fullContainerID = computed(() => `${CONTAINER_ID_PREFIX}${containerSuffix.value}`)
const containerIDValid = computed(() => /^[0-9]{7}$/.test(containerSuffix.value))

function onSuffixInput() {
  containerSuffix.value = containerSuffix.value.replace(/\D/g, '').slice(0, 7)
}

function openRegister() {
  containerSuffix.value = ''
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
      containerID: fullContainerID.value,
      cargo: registerForm.cargo.trim(),
      originPort: store.port,
      destPort: registerForm.destPort,
    })
    toast.add({ severity: 'success', summary: t('toast.containerRegistered'), detail: fullContainerID.value, life: 2500 })
    registerVisible.value = false
  } catch (err) {
    registerError.value = err.message
  } finally {
    registerBusy.value = false
  }
}

// ── Load container onto a docked ship ─────────────────────────────────────────
// Inline on each Outbound row: "Load" opens a popup menu of ships docked at
// this port (there is no separate ship-picker row) — choosing a ship loads
// immediately, no extra confirm step, matching the single-click Unload/Depart
// actions elsewhere in this UI. Errors surface as a toast since a menu item
// click has no natural inline error slot.

// Yard containers split by destination: outbound still needs to travel from
// here; arrived means terminalPort == destPort (BR-008/BR-009 territory —
// the domain has no separate "delivered" status, this is a client-side view
// of the same in-terminal containers). Only outbound containers are offered
// for loading — loading an arrived container is always rejected by BR-008.
const outboundContainers = computed(() => store.yardContainers.filter((c) => c.destPort !== store.port))
const arrivedContainers = computed(() => store.yardContainers.filter((c) => c.destPort === store.port))

const loadBusyID = ref('')
const shipMenu = ref()
const menuContainerID = ref('')

const shipMenuItems = computed(() =>
  store.dockedShips.map((s) => ({ label: s.shipID, command: () => submitLoad(menuContainerID.value, s.shipID) })),
)

function openShipMenu(event, containerID) {
  menuContainerID.value = containerID
  shipMenu.value.toggle(event)
}

async function submitLoad(containerID, shipID) {
  loadBusyID.value = containerID
  try {
    await loadContainer({ context: store.context, containerID, shipID })
    toast.add({ severity: 'success', summary: t('toast.containerLoaded'), detail: `${containerID} → ${shipID}`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: t('toast.loadFailed'), detail: err.message, life: 4000 })
  } finally {
    loadBusyID.value = ''
  }
}
</script>

<template>
  <section class="lab-panel">
    <h3>{{ t('terminal.title', { port: store.port || '—' }) }}</h3>

    <div v-if="!store.port" class="lab-muted no-port">
      {{ t('terminal.selectPort') }}
    </div>
    <div v-else class="ops">
      <Button :label="t('terminal.registerContainer')" icon="pi pi-plus" size="small" outlined @click="openRegister" />
    </div>

    <h4>{{ t('terminal.outbound') }}</h4>
    <DataTable :value="outboundContainers" size="small" data-key="containerID" resizableColumns columnResizeMode="expand">
      <template #empty>
        <span class="lab-muted">{{ t('terminal.outboundEmpty') }}</span>
      </template>
      <Column field="containerID" :header="t('table.container')" style="font-family:monospace;font-size:12px" />
      <Column field="cargo" :header="t('table.cargo')" />
      <Column field="originPort" :header="t('table.origin')" style="width:110px" />
      <Column :header="t('table.destination')" style="width:130px">
        <template #body="{ data }">
          <Tag severity="info" :value="data.destPort" />
        </template>
      </Column>
      <Column header="" style="width:100px">
        <template #body="{ data: container }">
          <Button
            :label="t('action.load')"
            size="small"
            :disabled="store.dockedShips.length === 0 || loadBusyID === container.containerID"
            :loading="loadBusyID === container.containerID"
            :title="store.dockedShips.length === 0 ? t('terminal.noDockedShips') : ''"
            @click="openShipMenu($event, container.containerID)"
          />
        </template>
      </Column>
    </DataTable>
    <Menu ref="shipMenu" :model="shipMenuItems" popup />

    <h4>{{ t('terminal.arrived') }}</h4>
    <DataTable :value="arrivedContainers" size="small" data-key="containerID" resizableColumns columnResizeMode="expand">
      <template #empty>
        <span class="lab-muted">{{ t('terminal.arrivedEmpty') }}</span>
      </template>
      <Column field="containerID" :header="t('table.container')" style="font-family:monospace;font-size:12px" />
      <Column field="cargo" :header="t('table.cargo')" />
      <Column field="originPort" :header="t('table.origin')" style="width:110px" />
      <Column :header="t('table.destination')" style="width:130px">
        <template #body="{ data }">
          <Tag severity="success" :value="data.destPort" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="registerVisible" :header="t('terminal.registerContainer')" modal style="width:26rem">
      <div class="dialog-fields">
        <InputGroup>
          <InputGroupAddon>{{ CONTAINER_ID_PREFIX }}</InputGroupAddon>
          <InputText
            v-model="containerSuffix"
            :placeholder="t('container.suffixPlaceholder')"
            size="small"
            inputmode="numeric"
            maxlength="7"
            @input="onSuffixInput"
          />
        </InputGroup>
        <span v-if="containerSuffix && !containerIDValid" class="format-hint">
          {{ t('container.formatHint', { containerId: `${CONTAINER_ID_PREFIX}1234567` }) }}
        </span>
        <InputText v-model.trim="registerForm.cargo" :placeholder="t('container.cargoPlaceholder')" size="small" />
        <Select v-model="registerForm.destPort" :options="destPortOptions" :placeholder="t('container.destinationPort')" editable size="small" />
        <span class="lab-muted">{{ t('container.originTerminal', { port: store.port }) }}</span>
      </div>
      <div v-if="registerError" class="domain-error">{{ registerError }}</div>
      <template #footer>
        <Button :label="t('action.cancel')" text size="small" @click="registerVisible = false" />
        <Button
          :label="t('action.register')"
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
