<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { useI18n } from 'vue-i18n'
import { computed, reactive, ref, watch } from 'vue'

import { arrivePort, departPort, unloadContainer } from '../api'
import { usePortStore } from '../stores/port'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'

const store = usePortStore()
const toast = useToast()
const { statusLabel } = useRefdataLabels()
const { t } = useI18n()

const expandedShips = ref({})

// ── Ship arrives ───────────────────────────────────────────────────────────────
// One dropdown, not a form: registering a brand-new ship now happens from the
// Fleet panel's "+" dialog (first arrival there). This picker only brings an
// *existing* ship here, so it only ever needs to identify which one — no name
// field. Only ships at sea are offered: a ship docked elsewhere must depart
// first (BR-002, ErrMustDepart), so listing it here would be a guaranteed-fail
// option, the same trap the outbound-only Load dropdown avoids elsewhere.

const shipsAtSeaOptions = computed(() =>
  store.allShips
    .filter((s) => s.currentPort === '')
    .map((s) => ({ label: `${s.shipID} — ${s.shipName}`, value: s.shipID })),
)

const arriveShipID = ref('')
const arriveBusy = ref(false)
const arriveError = ref('')

async function submitArrive() {
  arriveError.value = ''
  arriveBusy.value = true
  try {
    await arrivePort({ context: store.context, shipID: arriveShipID.value, port: store.port })
    toast.add({ severity: 'success', summary: t('toast.shipArrived'), detail: arriveShipID.value, life: 2500 })
    arriveShipID.value = ''
  } catch (err) {
    arriveError.value = err.message
  } finally {
    arriveBusy.value = false
  }
}

// ── Ship departs ───────────────────────────────────────────────────────────────
// Depart is inline on each docked-ship row (not a separate ship picker) — the
// row already identifies the ship, and every ship in this table is docked at
// the selected port, so the action is always valid here. Errors surface as a
// toast since a row action has no natural inline error slot.

const departBusyID = ref('')

async function submitDepart(shipID) {
  departBusyID.value = shipID
  try {
    await departPort({ context: store.context, shipID, port: store.port })
    toast.add({ severity: 'success', summary: t('toast.shipDeparted'), detail: shipID, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: t('toast.departFailed'), detail: err.message, life: 4000 })
  } finally {
    departBusyID.value = ''
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
    toast.add({ severity: 'success', summary: t('toast.containerUnloaded'), detail: containerID, life: 2500 })
  } catch (err) {
    unloadErrorByShip[shipID] = err.message
  } finally {
    unloadBusyID.value = ''
  }
}

// Clear the arrive error and stale per-ship unload errors when the selected
// port changes. (Depart and unload are inline row actions with no stale
// Select value to reset — the earlier depart-picker bug is gone with the
// picker. The arrive Select's options are fleet-wide, not port-scoped, so its
// value itself never goes stale on a port switch — only its error does.)
watch(
  () => store.port,
  () => {
    arriveError.value = ''
    for (const key of Object.keys(unloadErrorByShip)) delete unloadErrorByShip[key]
  },
)
</script>

<template>
  <section class="lab-panel">
    <h3>{{ t('shipsAtPort.title', { port: store.port || '—' }) }}</h3>

    <div v-if="!store.port" class="lab-muted no-port">
      {{ t('shipsAtPort.selectPort') }}
    </div>
    <div v-else class="ops">
      <div class="op-row">
        <Select
          v-model="arriveShipID"
          :options="shipsAtSeaOptions"
          option-label="label"
          option-value="value"
          :placeholder="t('shipsAtPort.atSea')"
          size="small"
          style="width:220px"
        />
        <Button
          :label="t('action.arrive')"
          size="small"
          :disabled="arriveBusy || !arriveShipID"
          :loading="arriveBusy"
          :title="shipsAtSeaOptions.length === 0 ? t('shipsAtPort.noShipsAtSea') : ''"
          @click="submitArrive"
        />
      </div>
      <div v-if="arriveError" class="domain-error">{{ arriveError }}</div>
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
        <span class="lab-muted">{{ t('shipsAtPort.empty') }}</span>
      </template>
      <Column expander style="width:2.5rem" />
      <Column field="shipID" :header="t('table.shipId')" style="font-family:monospace;font-size:12px" />
      <Column field="shipName" :header="t('table.name')" />
      <Column :header="t('status.label')" style="width:100px">
        <template #body="{ data }">
          <Tag severity="success" :value="statusLabel(data.status)" />
        </template>
      </Column>
      <Column :header="t('table.manifest')" style="width:100px">
        <template #body="{ data }">
          <span class="lab-muted manifest-count">{{ t('container.count', store.manifestFor(data.shipID).length) }}</span>
        </template>
      </Column>
      <Column header="" style="width:110px">
        <template #body="{ data }">
          <Button
            :label="t('action.depart')"
            size="small"
            :disabled="departBusyID === data.shipID"
            :loading="departBusyID === data.shipID"
            @click="submitDepart(data.shipID)"
          />
        </template>
      </Column>

      <template #expansion="{ data }">
        <DataTable :value="store.manifestFor(data.shipID)" size="small" data-key="containerID" class="manifest-table">
          <template #empty>
            <span class="lab-muted">{{ t('manifest.empty') }}</span>
          </template>
          <Column field="containerID" :header="t('table.container')" style="font-family:monospace;font-size:12px" />
          <Column field="cargo" :header="t('table.cargo')" />
          <Column field="originPort" :header="t('table.origin')" style="width:110px" />
          <Column :header="t('table.destination')" style="width:130px">
            <template #body="{ data: container }">
              <Tag :severity="container.destPort === store.port ? 'success' : 'info'" :value="container.destPort" />
            </template>
          </Column>
          <Column header="" style="width:100px">
            <template #body="{ data: container }">
              <Button
                :label="t('action.unload')"
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
