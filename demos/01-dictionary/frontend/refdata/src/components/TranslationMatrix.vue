<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import InputText from 'primevue/inputtext'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { listItemLocalizations, listItems, setLocalization } from '../api'
import { codeFor, labelFor } from '../itemFields'
import { useDictionaryStore } from '../stores/dictionary'

// Bulk translation editor for one type: enum values as rows, registered
// locales as columns. Distinct from the types×locales completeness matrix
// (Phase 11.7's LocalizationView) — that shows ratios across every type; this
// edits individual values' translations within a single type.
const props = defineProps({
  typeKey: { type: String, required: true },
})

const store = useDictionaryStore()
const toast = useToast()

const loading = ref(false)
const rows = ref([])
const filter = ref('')

async function load() {
  if (!props.typeKey) {
    rows.value = []
    return
  }
  loading.value = true
  try {
    const itemsRes = await listItems(store.context, props.typeKey, { all: true })
    const items = itemsRes?.items ?? []
    rows.value = await Promise.all(
      items.map(async (item) => {
        const code = codeFor(item)
        const locRes = await listItemLocalizations(store.context, props.typeKey, code)
        const cells = {}
        for (const loc of locRes?.localizations ?? []) {
          cells[loc.locale] = { translation: loc.label, description: loc.description || '' }
        }
        return { code, label: labelFor(item), cells }
      }),
    )
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not load translation matrix', detail: err.message, life: 4000 })
  } finally {
    loading.value = false
  }
}

watch(() => props.typeKey, load, { immediate: true })

const filteredRows = computed(() => {
  const q = filter.value.trim().toLowerCase()
  if (!q) return rows.value
  return rows.value.filter((row) => row.code.toLowerCase().includes(q) || row.label.toLowerCase().includes(q))
})

function cellKey(code, locale) {
  return `${code}:${locale}`
}

// The default locale falls back to the value's default label when no
// explicit localization override has been recorded yet — same fallback rule
// as the per-item Translations tab.
function cellText(row, locale) {
  const cell = row.cells[locale]
  if (cell) return cell.translation
  return locale === store.defaultLocale ? row.label : ''
}

const editingCell = ref(null)
const editValue = ref('')

function startEditCell(row, locale) {
  editingCell.value = cellKey(row.code, locale)
  editValue.value = cellText(row, locale) || ''
}

function cancelEditCell() {
  editingCell.value = null
}

async function submitCell(row, locale) {
  const value = editValue.value.trim()
  if (!value || editingCell.value !== cellKey(row.code, locale)) {
    editingCell.value = null
    return
  }
  try {
    await setLocalization({
      typeKey: props.typeKey,
      code: row.code,
      context: store.context,
      locale,
      label: value,
      description: row.cells[locale]?.description || '',
    })
    row.cells[locale] = { translation: value, description: row.cells[locale]?.description || '' }
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not save translation', detail: err.message, life: 4000 })
  } finally {
    editingCell.value = null
  }
}
</script>

<template>
  <div class="matrix">
    <InputText
      v-model="filter"
      placeholder="Filter by key or label…"
      size="small"
      class="matrix-filter"
    />
    <DataTable
      :value="filteredRows"
      :loading="loading"
      size="small"
      data-key="code"
      scrollable
      scroll-direction="horizontal"
      class="matrix-table"
    >
      <template #empty>
        No values in this type yet.
      </template>
      <Column
        field="code"
        header="Enum value"
        sortable
        style="font-family: monospace; width: 16rem"
        frozen
      >
        <template #body="{ data }">
          <span
            class="enum-key"
            :title="data.code"
          >{{ data.code }}</span>
        </template>
      </Column>
      <Column
        v-for="locale in store.locales"
        :key="locale"
        :field="(row) => cellText(row, locale)"
        :header="locale"
        sortable
        style="width: 18rem"
      >
        <template #body="{ data }">
          <InputText
            v-if="editingCell === cellKey(data.code, locale)"
            v-model="editValue"
            size="small"
            style="width: 100%"
            @keyup.enter="submitCell(data, locale)"
            @keyup.escape="cancelEditCell"
            @blur="submitCell(data, locale)"
          />
          <span
            v-else
            class="matrix-cell"
            :class="{ 'lab-muted': !cellText(data, locale) }"
            @click="startEditCell(data, locale)"
          >{{ cellText(data, locale) || '—' }}</span>
        </template>
      </Column>
    </DataTable>
    <p
      v-if="store.locales.length === 0"
      class="lab-muted empty"
    >
      No locales registered for this context yet.
    </p>
  </div>
</template>

<style scoped>
.matrix-filter {
  width: 100%;
  max-width: 20rem;
  margin-bottom: 0.5rem;
}
/* Caps the table to the frozen "Enum value" column plus exactly 3 locale
   columns (each a fixed 12rem below) — a 4th+ locale scrolls horizontally
   instead of growing the table wider than the panel. table-layout:fixed makes
   those widths authoritative — otherwise the browser's default content-driven
   sizing (auto) shrinks columns below their declared width to fit long
   translated strings, which both breaks the "3 columns" math and clips text
   instead of wrapping it. */
.matrix-table {
  max-width: calc(16rem + 3 * 18rem);
}
.matrix-table :deep(.p-datatable-table) {
  table-layout: fixed;
}
.enum-key {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.matrix-cell {
  display: block;
  cursor: pointer;
  padding: 0.15rem 0;
}
.empty {
  padding: 0.5rem 0.25rem;
  font-size: 12px;
}
</style>
