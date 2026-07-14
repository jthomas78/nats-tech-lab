<script setup>
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { getCompleteness } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()
const toast = useToast()

const newLocale = ref('')
const newIsDefault = ref(false)

async function submitAddLocale() {
  if (!newLocale.value.trim()) return
  try {
    await store.addLocaleToContext(newLocale.value.trim(), newIsDefault.value)
    newLocale.value = ''
    newIsDefault.value = false
    toast.add({ severity: 'success', summary: 'Locale registered', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not register locale', detail: err.message, life: 4000 })
  }
}

// ── Per-type completeness for a chosen locale ────────────────────────────────

const completenessLocale = ref('')
const completeness = ref([])
const loadingCompleteness = ref(false)

const localeOptions = computed(() => store.locales)

async function refreshCompleteness() {
  if (!completenessLocale.value) {
    completeness.value = []
    return
  }
  loadingCompleteness.value = true
  try {
    const rows = await Promise.all(
      store.types.map(async (t) => {
        const res = await getCompleteness(store.context, t.typeKey, completenessLocale.value)
        return { typeKey: t.typeKey, total: res?.total ?? 0, localized: res?.localized ?? 0 }
      }),
    )
    completeness.value = rows
  } finally {
    loadingCompleteness.value = false
  }
}

watch(completenessLocale, refreshCompleteness)
watch(() => store.types, refreshCompleteness)

function ratioLabel(row) {
  return `${row.localized}/${row.total}`
}
function ratioSeverity(row) {
  if (row.total === 0) return 'secondary'
  if (row.localized === row.total) return 'success'
  if (row.localized === 0) return 'danger'
  return 'warning'
}
</script>

<template>
  <div class="lab-panel locales-panel">
    <h3>Locales</h3>

    <DataTable
      :value="store.locales.map((l) => ({ locale: l }))"
      size="small"
      data-key="locale"
    >
      <template #empty>
        No locales registered for this context yet.
      </template>
      <Column
        field="locale"
        header="Locale"
      />
    </DataTable>

    <div class="add-row">
      <InputText
        v-model="newLocale"
        placeholder="locale (e.g. de-DE)"
        size="small"
        style="width: 8rem"
      />
      <label class="lab-muted default-check">
        <Checkbox
          v-model="newIsDefault"
          binary
          size="small"
        />
        default
      </label>
      <Button
        icon="pi pi-plus"
        size="small"
        :disabled="!newLocale.trim()"
        @click="submitAddLocale"
      />
    </div>

    <h3 class="completeness-head">
      Localization completeness
    </h3>
    <Select
      v-model="completenessLocale"
      :options="localeOptions"
      placeholder="pick a locale"
      size="small"
      style="width: 10rem; margin-bottom: 0.5rem"
    />
    <DataTable
      :value="completeness"
      size="small"
      data-key="typeKey"
    >
      <template #empty>
        Pick a locale above to see per-type completeness.
      </template>
      <Column
        field="typeKey"
        header="Type"
      />
      <Column header="Completeness">
        <template #body="{ data }">
          <Tag
            :severity="ratioSeverity(data)"
            :value="ratioLabel(data)"
          />
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.add-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  margin: 0.5rem 0 1rem;
}
.default-check {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 12px;
}
.completeness-head {
  margin-top: 0.75rem;
}
</style>
