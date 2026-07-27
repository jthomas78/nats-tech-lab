<script setup>
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import RadioButton from 'primevue/radiobutton'
import RadioButtonGroup from 'primevue/radiobuttongroup'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, onMounted, ref, watch } from 'vue'

import { getCompleteness } from '../api'
import { localeLabel, orderLocales } from '../localization'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()

// BR-D32: the default locale is shown first in every locale list, and marked
// as the default where it's rendered as text.
const orderedLocales = computed(() => orderLocales(store.locales, store.defaultLocale))
const localeText = (locale) => localeLabel(locale, store.defaultLocale)
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
      <RadioButtonGroup
        :model-value="store.defaultLocale"
        @update:model-value="makeDefault"
      >
        <DataTable
          :value="orderedLocales.map((l) => ({ locale: l }))"
          size="small"
          data-key="locale"
          resizable-columns
          column-resize-mode="fit"
        >
          <template #empty>
            No locales registered for this context yet.
          </template>
          <Column header="Locale">
            <template #body="{ data }">
              {{ localeText(data.locale) }}
            </template>
          </Column>
          <Column header="Default">
            <template #body="{ data }">
              <RadioButton
                :value="data.locale"
                size="small"
                :input-id="`default-${data.locale}`"
                :aria-label="`Make ${data.locale} the default locale`"
              />
            </template>
          </Column>
        </DataTable>
      </RadioButtonGroup>

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
      <div class="table-scroll">
      <DataTable
        :value="store.types"
        size="small"
        data-key="typeKey"
        :loading="loading"
        resizable-columns
        column-resize-mode="fit"
      >
        <template #empty>
          No dictionary types registered yet.
        </template>
        <Column
          field="name"
          header="Type"
        />
        <Column
          v-for="locale in orderedLocales"
          :key="locale"
          :header="localeText(locale)"
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
/* Same right-edge scroll hint as the Translations tab — this table is fine
   today at 3 locales but grows one column per registered locale. */
.table-scroll {
  position: relative;
}
@media (max-width: 700px) {
  .table-scroll::after {
    content: '';
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 1.25rem;
    pointer-events: none;
    background: linear-gradient(to right, transparent, var(--lab-panel-bg, #1a1e23));
  }
}
/* RadioButtonGroup's root defaults to display:inline-flex — it only exists
   here to share one d_value across all rows' radios (see PrimeVue's
   BaseEditableHolder), not to lay anything out, so drop it from the box tree. */
:deep(.p-radiobutton-group) {
  display: contents;
}
</style>
