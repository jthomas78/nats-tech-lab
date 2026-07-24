<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref, watch } from 'vue'

import { draftTranslation, listItemLocalizations, listItems, setLocalization } from '../api'
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
// Cells with an unsaved AI-drafted candidate, keyed by cellKey (BR-D07) —
// never written to Postgres until the steward saves or discards each one.
const drafts = reactive({})
// Whether the cell currently being edited started from an AI draft, so
// submitCell knows to record source: 'ai' rather than the 'manual' default.
const editIsDraft = ref(false)
const draftingLocale = ref(null) // locale column currently bulk-drafting, or null

function startEditCell(row, locale) {
  const key = cellKey(row.code, locale)
  editingCell.value = key
  editIsDraft.value = Boolean(drafts[key])
  editValue.value = drafts[key]?.label ?? cellText(row, locale) ?? ''
}

function cancelEditCell() {
  editingCell.value = null
}

async function submitCell(row, locale) {
  const value = editValue.value.trim()
  const key = cellKey(row.code, locale)
  if (!value || editingCell.value !== key) {
    editingCell.value = null
    return
  }
  const wasDraft = editIsDraft.value
  try {
    await setLocalization({
      typeKey: props.typeKey,
      code: row.code,
      context: store.context,
      locale,
      label: value,
      description: drafts[key]?.description ?? row.cells[locale]?.description ?? '',
      source: wasDraft ? 'ai' : 'manual',
    })
    row.cells[locale] = { translation: value, description: drafts[key]?.description ?? row.cells[locale]?.description ?? '' }
    delete drafts[key]
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not save translation', detail: err.message, life: 4000 })
  } finally {
    editingCell.value = null
    editIsDraft.value = false
  }
}

// Saves a drafted candidate verbatim, without opening the inline edit field
// first — the fast path for "this AI draft looks fine as-is".
async function acceptDraft(row, locale) {
  const key = cellKey(row.code, locale)
  const draft = drafts[key]
  if (!draft) return
  try {
    await setLocalization({
      typeKey: props.typeKey,
      code: row.code,
      context: store.context,
      locale,
      label: draft.label,
      description: draft.description || '',
      source: 'ai',
    })
    row.cells[locale] = { translation: draft.label, description: draft.description || '' }
    delete drafts[key]
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not save translation', detail: err.message, life: 4000 })
  }
}

function discardDraft(row, locale) {
  delete drafts[cellKey(row.code, locale)]
}

// Bulk "Draft missing (AI)" for one locale column — sequential, never
// concurrent (BR-D24): each row awaits its own draft call before the next
// starts. A single row's failure surfaces via toast but does not stop the
// rest (BR-D07's per-locale failure guardrail applies per-row here).
async function draftMissingForLocale(locale) {
  draftingLocale.value = locale
  let failures = 0
  try {
    for (const row of rows.value) {
      if (cellText(row, locale)) continue // already has a saved or default value
      try {
        const res = await draftTranslation(props.typeKey, row.code, store.context, [locale])
        const draft = res?.drafts?.[0]
        if (draft && !draft.error) {
          drafts[cellKey(row.code, locale)] = { label: draft.label, description: draft.description }
        } else {
          failures++
        }
      } catch {
        failures++
      }
    }
  } finally {
    draftingLocale.value = null
    if (failures > 0) {
      toast.add({
        severity: 'warn',
        summary: 'Some AI drafts failed',
        detail: `${failures} value(s) in "${locale}" could not be drafted.`,
        life: 4000,
      })
    }
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
      resizable-columns
      column-resize-mode="fit"
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
        sortable
        style="width: 18rem"
      >
        <template #header>
          <span class="locale-header">
            {{ locale }}
            <Button
              icon="pi pi-sparkles"
              text
              size="small"
              aria-label="Draft missing (AI)"
              title="Draft missing (AI)"
              :loading="draftingLocale === locale"
              :disabled="draftingLocale !== null"
              @click.stop="draftMissingForLocale(locale)"
            />
          </span>
        </template>
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
          <div
            v-else-if="drafts[cellKey(data.code, locale)]"
            class="matrix-cell-draft"
          >
            <span
              class="matrix-cell"
              @click="startEditCell(data, locale)"
            >{{ drafts[cellKey(data.code, locale)].label }}</span>
            <Tag
              severity="info"
              value="AI"
              class="ai-draft-tag"
            />
            <Button
              icon="pi pi-check"
              text
              size="small"
              aria-label="Accept draft"
              title="Accept draft"
              @click="acceptDraft(data, locale)"
            />
            <Button
              icon="pi pi-times"
              text
              size="small"
              aria-label="Discard draft"
              title="Discard draft"
              @click="discardDraft(data, locale)"
            />
          </div>
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
.locale-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.25rem;
  width: 100%;
}
.matrix-cell-draft {
  display: flex;
  align-items: center;
  gap: 0.35rem;
}
.matrix-cell-draft .matrix-cell {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ai-draft-tag {
  flex: 0 0 auto;
}
.empty {
  padding: 0.5rem 0.25rem;
  font-size: 12px;
}
</style>
