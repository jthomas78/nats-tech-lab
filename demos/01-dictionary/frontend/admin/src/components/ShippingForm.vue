<script setup>
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref } from 'vue'

import { arrivePort, departPort, loadContainer, registerContainer, unloadContainer } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const OPERATIONS = [
  { label: 'Arrive at port', value: 'arrive' },
  { label: 'Depart from port', value: 'depart' },
  { label: 'Register container', value: 'register' },
  { label: 'Load container', value: 'load' },
  { label: 'Unload container', value: 'unload' },
]

const store = useDictionaryStore()

// Backed by the Postgres ports registry (BR-017/BR-018), seeded via
// getPorts() on connect — see stores/dictionary.js. Typing an unregistered
// port here (the Select is editable) is rejected server-side with a clear
// domain error, shown below.
const portOptions = computed(() => [...store.seenPorts].sort())
const toast = useToast()
const busy = ref(false)
const domainError = ref('')

const form = reactive({
  op: 'arrive',
  shipID: '',
  shipName: '',
  port: '',
  containerID: '',
  cargo: '',
  originPort: '',
  destPort: '',
})

const needsShip = computed(() => form.op !== 'register')
const needsPort = computed(() => form.op === 'arrive' || form.op === 'depart')
const needsShipName = computed(() => form.op === 'arrive')
const needsContainer = computed(() => form.op === 'register' || form.op === 'load' || form.op === 'unload')
const needsRoute = computed(() => form.op === 'register')

const isValid = computed(() => {
  if (needsShip.value && !form.shipID) return false
  if (needsPort.value && !form.port) return false
  if (needsContainer.value && !form.containerID) return false
  if (needsRoute.value && (!form.cargo || !form.originPort || !form.destPort)) return false
  return true
})

function clearError() { domainError.value = '' }

async function submit() {
  domainError.value = ''
  busy.value = true
  try {
    const context = store.context
    let result
    switch (form.op) {
      case 'arrive':
        result = await arrivePort({
          context, shipID: form.shipID.trim(),
          shipName: form.shipName.trim() || form.shipID, port: form.port,
        })
        break
      case 'depart':
        result = await departPort({ context, shipID: form.shipID.trim(), port: form.port })
        break
      case 'register':
        result = await registerContainer({
          context, containerID: form.containerID.trim(),
          cargo: form.cargo.trim(), originPort: form.originPort, destPort: form.destPort,
        })
        break
      case 'load':
        result = await loadContainer({
          context, containerID: form.containerID.trim(), shipID: form.shipID.trim(),
        })
        break
      case 'unload':
        result = await unloadContainer({
          context, containerID: form.containerID.trim(), shipID: form.shipID.trim(),
        })
        break
    }
    const ship = result?.ship
    const container = result?.container
    let detail = 'Event published, projections updating'
    if (ship) {
      detail = `${ship.shipName} — ${ship.currentPort || 'at sea'}`
    } else if (container) {
      detail = container.status === 'on-ship'
        ? `${container.containerID} — on ${container.onShipID}`
        : `${container.containerID} — in ${container.terminalPort} terminal`
    }
    toast.add({ severity: 'success', summary: 'Command accepted', detail, life: 3000 })
  } catch (err) {
    // Phase 33.8: these commands moved from REST to NATS api.* (api.js), so
    // err.message is always the bare domain-error string (browserrpc's
    // errorResponse), never an HTTP-status-prefixed one — show it inline,
    // same as frontend/refdata's TradingPartnersPanel.vue's NATS-backed submitRegister().
    domainError.value = err.message
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
        v-if="needsShip"
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

      <InputText
        v-if="needsContainer"
        v-model.trim="form.containerID"
        placeholder="container ID, e.g. TCKU1234567"
        size="small"
        style="width:200px"
        @input="clearError"
      />

      <template v-if="needsRoute">
        <InputText v-model.trim="form.cargo" placeholder="cargo, e.g. Electronics" size="small" @input="clearError" />
        <Select v-model="form.originPort" :options="portOptions" placeholder="origin port" editable size="small" @change="clearError" />
        <Select v-model="form.destPort" :options="portOptions" placeholder="destination port" editable size="small" @change="clearError" />
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
      Commands publish <code>emea.events.acme.{aggregate}.{id}.{event}</code> subjects —
      two aggregates, one stream. Domain rules (BR-001…BR-018) are enforced before publishing;
      invalid transitions return an error above. Fleet: <code>{{ store.context }}</code>
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
