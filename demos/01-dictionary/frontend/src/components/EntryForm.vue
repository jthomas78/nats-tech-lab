<script setup>
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import { useToast } from 'primevue/usetoast'
import { reactive, ref } from 'vue'

import { createEntry, updateEntry } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const ENTITY_TYPES = ['currency', 'country', 'status', 'locale']

const store = useDictionaryStore()
const toast = useToast()
const busy = ref(false)

const form = reactive({
  entityType: ENTITY_TYPES[0],
  id: '',
  label: '',
})

async function submit(action, verb) {
  busy.value = true
  try {
    await action({ context: store.context, ...form })
    toast.add({
      severity: 'success',
      summary: `${verb} accepted`,
      detail: `${form.entityType}.${form.id} → event published, watch both panels`,
      life: 2500,
    })
  } catch (err) {
    toast.add({ severity: 'error', summary: `${verb} failed`, detail: err.message, life: 4000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="lab-panel entry-form" @submit.prevent>
    <h3>Create / update entry</h3>
    <div class="fields">
      <Select v-model="form.entityType" :options="ENTITY_TYPES" size="small" />
      <InputText v-model.trim="form.id" placeholder="id, e.g. GBP" size="small" />
      <InputText v-model.trim="form.label" placeholder="label, e.g. Pound Sterling" size="small" />
      <Button
        label="Create"
        size="small"
        :disabled="busy || !form.id || !form.label"
        @click="submit(createEntry, 'Create')"
      />
      <Button
        label="Update"
        size="small"
        severity="secondary"
        :disabled="busy || !form.id || !form.label"
        @click="submit(updateEntry, 'Update')"
      />
    </div>
    <p class="lab-muted hint">
      Commands publish <code>DICTIONARY.entry.created / .updated</code> events; both projections
      react independently. Context <code>{{ store.context }}</code> is taken from the selector
      above.
    </p>
  </form>
</template>

<style scoped>
.fields {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.hint {
  margin: 0.75rem 0 0;
  font-size: 0.85rem;
}
</style>
