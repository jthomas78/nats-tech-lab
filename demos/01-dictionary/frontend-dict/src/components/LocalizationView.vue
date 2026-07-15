<script setup>
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import RadioButton from 'primevue/radiobutton'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { onMounted, ref, watch } from 'vue'

import { getCompleteness } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()
const toast = useToast()

// ── Locale registration — rare, context-level admin ──────────────────────────

const registerVisible = ref(false)
const newLocale = ref('')
const newIsDefault = ref(false)

function openRegister() {
  newLocale.value = ''
  newIsDefault.value = false
  registerVisible.value = true
}

async function submitAddLocale() {
  if (!newLocale.value.trim()) return
  try {
    await store.addLocaleToContext(newLocale.value.trim(), newIsDefault.value)
    registerVisible.value = false
    toast.add({ severity: 'success', summary: 'Locale registered', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not register locale', detail: err.message, life: 4000 })
  }
}

// ── Default locale — exactly-one semantics (BR-D03's fallback target) ────────
// Radio, not checkbox: picking another locale *moves* the default; the
// current default can't be un-set, only replaced.

async function makeDefault(locale) {
  if (locale === store.defaultLocale) return
  try {
    await store.setDefaultLocale(locale)
    toast.add({ severity: 'success', summary: `${locale} is now the default locale`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not set default locale', detail: err.message, life: 4000 })
  }
}

// ── Completeness matrix — every type x every locale, one glance ──────────────
// Replaces the old one-locale-at-a-time Select: a translator's actual daily
// question is "what's left across everything", not "how's this one locale".

const matrix = ref({}) // typeKey -> locale -> { total, localized }
const loading = ref(false)

async function loadMatrix() {
  if (store.types.length === 0 || store.locales.length === 0) {
    matrix.value = {}
    return
  }
  loading.value = true
  try {
    const next = {}
    await Promise.all(
      store.types.map(async (t) => {
        next[t.typeKey] = {}
        await Promise.all(
          store.locales.map(async (locale) => {
            const res = await getCompleteness(store.context, t.typeKey, locale)
            next[t.typeKey][locale] = { total: res?.total ?? 0, localized: res?.localized ?? 0 }
          }),
        )
      }),
    )
    matrix.value = next
  } finally {
    loading.value = false
  }
}

watch(() => [store.types.length, store.locales.length], loadMatrix)
onMounted(loadMatrix)

function cellFor(typeKey, locale) {
  return matrix.value[typeKey]?.[locale] ?? { total: 0, localized: 0 }
}
function ratioLabel(cell) {
  return `${cell.localized}/${cell.total}`
}
function ratioSeverity(cell) {
  if (cell.total === 0) return 'secondary'
  if (cell.localized === cell.total) return 'success'
  if (cell.localized === 0) return 'danger'
  return 'warning'
}
</script>

<template>
  <div class="localization-view">
    <div class="lab-panel">
      <div class="panel-head">
        <h3>Locales</h3>
        <Button
          icon="pi pi-plus"
          label="Add"
          size="small"
          @click="openRegister"
        />
      </div>
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
        <Column header="Default">
          <template #body="{ data }">
            <RadioButton
              :model-value="store.defaultLocale"
              :value="data.locale"
              name="default-locale"
              size="small"
              :input-id="`default-${data.locale}`"
              :aria-label="`Make ${data.locale} the default locale`"
              @update:model-value="makeDefault(data.locale)"
            />
          </template>
        </Column>
      </DataTable>

      <Dialog
        v-model:visible="registerVisible"
        header="Register locale"
        modal
        style="width: 22rem"
      >
        <div class="field">
          <label
            class="lab-muted"
            for="new-locale"
          >Locale</label>
          <InputText
            id="new-locale"
            v-model="newLocale"
            style="width: 100%"
            placeholder="e.g. de-DE"
          />
        </div>
        <label class="lab-muted default-check">
          <Checkbox
            v-model="newIsDefault"
            binary
            size="small"
          />
          Make this the default locale
        </label>
        <template #footer>
          <Button
            label="Cancel"
            text
            size="small"
            @click="registerVisible = false"
          />
          <Button
            label="Register"
            size="small"
            :disabled="!newLocale.trim()"
            @click="submitAddLocale"
          />
        </template>
      </Dialog>
    </div>

    <div class="lab-panel">
      <h3>Localization Completeness</h3>
      <p class="lab-muted matrix-hint">
        Every dictionary type against every registered locale — no per-locale picker.
      </p>
      <DataTable
        :value="store.types"
        size="small"
        data-key="typeKey"
        :loading="loading"
      >
        <template #empty>
          No dictionary types registered yet.
        </template>
        <Column
          field="name"
          header="Type"
        />
        <Column
          v-for="locale in store.locales"
          :key="locale"
          :header="locale"
        >
          <template #body="{ data }">
            <Tag
              :severity="ratioSeverity(cellFor(data.typeKey, locale))"
              :value="ratioLabel(cellFor(data.typeKey, locale))"
            />
          </template>
        </Column>
      </DataTable>
    </div>
  </div>
</template>

<style scoped>
.localization-view {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}
.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.5rem;
}
.panel-head h3 {
  margin: 0;
}
.field {
  margin-bottom: 0.75rem;
}
.field label {
  display: block;
  margin-bottom: 0.25rem;
  font-size: 11px;
}
.default-check {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 12px;
}
.matrix-hint {
  margin: 0 0 0.5rem;
  font-size: 12px;
}
</style>
