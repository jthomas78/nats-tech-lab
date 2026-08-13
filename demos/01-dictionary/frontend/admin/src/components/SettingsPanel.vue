<script setup>
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { computed, onMounted, reactive, ref } from 'vue'

import { getSystemConfig, updateSystemConfig } from '../api'

const toast = useToast()

// Read-only hard envelope (BR-UA03) reported by the backend — the UI never
// hardcodes 15/30, it constrains its editors from whatever the server sends.
const envelope = reactive({ min: 15, max: 30 })
const updatedAt = ref('')
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')

// Editable working copy + the last-persisted snapshot, so we can show a dirty
// state and offer a reset.
const form = reactive({ value: 15, min: 15, max: 30 })
const saved = reactive({ value: 15, min: 15, max: 30 })

function apply(cfg) {
  form.value = cfg.tokenTtlMinutes
  form.min = cfg.tokenTtlMinMinutes
  form.max = cfg.tokenTtlMaxMinutes
  saved.value = cfg.tokenTtlMinutes
  saved.min = cfg.tokenTtlMinMinutes
  saved.max = cfg.tokenTtlMaxMinutes
  envelope.min = cfg.envelopeMinMinutes
  envelope.max = cfg.envelopeMaxMinutes
  updatedAt.value = cfg.updatedAt || ''
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    apply(await getSystemConfig())
  } catch (e) {
    loadError.value = e.message
  } finally {
    loading.value = false
  }
}

// Client-side mirror of the backend's BR-AC21 rules — for immediate feedback
// only; the server re-validates and is the source of truth.
const validationError = computed(() => {
  if (form.min < envelope.min || form.max > envelope.max) {
    return `Range must stay within the ${envelope.min}–${envelope.max} minute envelope.`
  }
  if (form.min > form.max) {
    return 'Range minimum must not exceed the maximum.'
  }
  if (form.value < form.min || form.value > form.max) {
    return `Value must fall within the configured ${form.min}–${form.max} minute range.`
  }
  return ''
})

const dirty = computed(
  () => form.value !== saved.value || form.min !== saved.min || form.max !== saved.max,
)
const canSave = computed(() => dirty.value && !validationError.value && !saving.value)

async function save() {
  saving.value = true
  try {
    const res = await updateSystemConfig({
      tokenTtlMinutes: form.value,
      tokenTtlMinMinutes: form.min,
      tokenTtlMaxMinutes: form.max,
    })
    apply(res)
    toast.add({ severity: 'success', summary: 'Settings saved', detail: `Token TTL is now ${res.tokenTtlMinutes} min`, life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to save settings', detail: e.message, life: 5000 })
  } finally {
    saving.value = false
  }
}

function reset() {
  form.value = saved.value
  form.min = saved.min
  form.max = saved.max
}

onMounted(load)
</script>

<template>
  <div class="lab-panel settings-panel">
    <header class="settings-head">
      <h2>Browser &amp; Admin JWT expiry</h2>
      <p class="lab-muted">
        The TTL stamped on the short-lived NATS credentials the browser apps use to
        connect over WebSocket (BR-AC20). A tab open past the TTL is force-closed by
        NATS and silently reconnects with a fresh credential — this value sets that
        cadence and the credential blast radius.
      </p>
    </header>

    <Message v-if="loadError" severity="error" :closable="false">{{ loadError }}</Message>

    <div v-else-if="!loading" class="settings-body">
      <div class="field-grid">
        <label for="ttl-value">
          Token TTL <span class="lab-muted">(minutes)</span>
          <InputNumber
            id="ttl-value"
            v-model="form.value"
            :min="form.min"
            :max="form.max"
            :use-grouping="false"
            show-buttons
          />
        </label>

        <label for="ttl-min">
          Allowed range — min
          <InputNumber
            id="ttl-min"
            v-model="form.min"
            :min="envelope.min"
            :max="envelope.max"
            :use-grouping="false"
            show-buttons
          />
        </label>

        <label for="ttl-max">
          Allowed range — max
          <InputNumber
            id="ttl-max"
            v-model="form.max"
            :min="envelope.min"
            :max="envelope.max"
            :use-grouping="false"
            show-buttons
          />
        </label>
      </div>

      <p class="envelope-note lab-muted">
        Hard limit: {{ envelope.min }}–{{ envelope.max }} minutes (BR-UA03). The
        range can only narrow within this envelope; widening it is a code change.
      </p>

      <Message v-if="validationError" severity="warn" :closable="false">{{ validationError }}</Message>

      <div class="actions">
        <Button label="Save" size="small" :disabled="!canSave" :loading="saving" @click="save" />
        <Button label="Reset" size="small" severity="secondary" text :disabled="!dirty || saving" @click="reset" />
        <span v-if="updatedAt" class="lab-muted updated-at">Last updated {{ updatedAt }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-panel {
  max-width: 640px;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.settings-head h2 {
  margin: 0 0 0.35rem;
}
.settings-head p {
  margin: 0;
  max-width: 62ch;
}
.settings-body {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}
.field-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.75rem;
}
.field-grid label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-weight: 600;
}
.field-grid label span {
  font-weight: 400;
}
.envelope-note {
  margin: 0;
}
.actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.updated-at {
  margin-left: auto;
}
@media (max-width: 640px) {
  .field-grid {
    grid-template-columns: 1fr;
  }
}
</style>
