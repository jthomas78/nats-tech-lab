<script setup>
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref } from 'vue'

import { arrivePort, departPort, loadCargo, unloadCargo } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const OPERATIONS = [
  { label: 'Arrive at port', value: 'arrive' },
  { label: 'Depart from port', value: 'depart' },
  { label: 'Load cargo', value: 'load' },
  { label: 'Unload cargo', value: 'unload' },
]

const EXAMPLE_SHIPS = ['Orient Express', 'Pacific Star', 'Atlantic Pioneer', 'Nordic Voyager']
const BASE_PORTS = ['Hamburg', 'Rotterdam', 'Singapore', 'New York', 'Shanghai', 'Sydney']

const store = useDictionaryStore()

const portOptions = computed(() => {
  const merged = new Set([...BASE_PORTS, ...store.seenPorts])
  return [...merged].sort()
})
const toast = useToast()
const busy = ref(false)
const domainError = ref('')

const form = reactive({
  op: 'arrive',
  shipID: '',
  shipName: '',
  port: '',
  cargoDescription: '',
  cargoUnits: 1,
})

const needsPort = computed(() => form.op === 'arrive' || form.op === 'depart')
const needsCargo = computed(() => form.op === 'load' || form.op === 'unload')
const needsShipName = computed(() => form.op === 'arrive')

const isValid = computed(() => {
  if (!form.shipID) return false
  if (needsPort.value && !form.port) return false
  if (needsCargo.value && !form.cargoDescription) return false
  return true
})

function clearError() { domainError.value = '' }

async function submit() {
  domainError.value = ''
  busy.value = true
  try {
    const base = { context: store.context, shipID: form.shipID.trim() }
    let result
    switch (form.op) {
      case 'arrive':
        result = await arrivePort({ ...base, shipName: form.shipName.trim() || form.shipID, port: form.port })
        break
      case 'depart':
        result = await departPort({ ...base, port: form.port })
        break
      case 'load':
        result = await loadCargo({ ...base, cargo: { description: form.cargoDescription.trim(), units: form.cargoUnits } })
        break
      case 'unload':
        result = await unloadCargo({ ...base, cargo: { description: form.cargoDescription.trim(), units: form.cargoUnits } })
        break
    }
    const ship = result?.ship
    toast.add({
      severity: 'success',
      summary: 'Command accepted',
      detail: ship
        ? `${ship.shipName} — ${ship.currentPort || 'at sea'} — ${ship.cargo?.length ?? 0} cargo item(s)`
        : 'Event published, projections updating',
      life: 3000,
    })
  } catch (err) {
    // 422 = domain rule violation — show inline; other errors → toast
    if (err.message && !err.message.startsWith('5')) {
      domainError.value = err.message
    } else {
      toast.add({ severity: 'error', summary: 'Command failed', detail: err.message, life: 4000 })
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="lab-panel shipping-form" @submit.prevent="submit">
    <h3>Shipping Operations</h3>

    <div class="fields">
      <Select v-model="form.op" :options="OPERATIONS" option-label="label" option-value="value" size="small" @change="clearError" />

      <InputText
        v-model.trim="form.shipID"
        placeholder="ship ID, e.g. orient-express"
        size="small"
        @input="clearError"
      />

      <InputText
        v-if="needsShipName"
        v-model.trim="form.shipName"
        placeholder="ship name (first arrival only)"
        size="small"
      />

      <Select
        v-if="needsPort"
        v-model="form.port"
        :options="portOptions"
        placeholder="port"
        editable
        size="small"
        @change="clearError"
      />

      <template v-if="needsCargo">
        <InputText v-model.trim="form.cargoDescription" placeholder="cargo, e.g. Electronics" size="small" @input="clearError" />
        <InputNumber v-model="form.cargoUnits" :min="1" placeholder="units" size="small" style="width:90px" />
      </template>

      <Button
        type="submit"
        label="Execute"
        size="small"
        :disabled="busy || !isValid"
        :loading="busy"
      />
    </div>

    <div v-if="domainError" class="domain-error">
      {{ domainError }}
    </div>

    <p class="lab-muted hint">
      Commands publish <code>DICTIONARY.ship.*</code> / <code>DICTIONARY.cargo.*</code> events.
      Domain rules are enforced before publishing — invalid transitions return an error above.
      Fleet: <code>{{ store.context }}</code>
    </p>
  </form>
</template>

<style scoped>
.fields {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
  align-items: center;
}
.domain-error {
  margin-top: 0.5rem;
  font-size: 0.85rem;
  color: var(--p-red-400, #f87171);
  background: rgba(248, 113, 113, 0.08);
  border: 1px solid rgba(248, 113, 113, 0.25);
  border-radius: 4px;
  padding: 0.35rem 0.6rem;
}
.hint {
  margin: 0.75rem 0 0;
  font-size: 0.85rem;
}
</style>
