<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import InputText from 'primevue/inputtext'
import Menu from 'primevue/menu'
import Select from 'primevue/select'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import {
  createReference,
  deleteItem,
  deprecateItem,
  listItemLocalizations,
  listItemReferences,
  reactivateItem,
  setLocalization,
  updateItem,
} from '../api'
import { attrsFor, labelFor, statusFor } from '../itemFields'
import { buildTranslationRows, filterTranslationRows } from '../localization'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()
const toast = useToast()

const detail = ref(null)
const localizations = ref([])
const references = ref([])
const loading = ref(false)

// Controlled so the header Edit button can jump to the Details tab (where the
// edit fields live) regardless of which tab the admin was last on.
const activeTab = ref('details')

const code = computed(() => store.selectedCode)
// The default label/description live in attrs (attrs.name / attrs.description)
// — distinct from `label`, which is the *locale-resolved* display value (may
// already be a translation if a non-default locale is selected). General-tab
// edits must read/write attrs directly: mixing up the resolved label with
// attrs.name would silently overwrite the default with whatever locale is
// currently selected.
const attrs = computed(() => attrsFor(detail.value ?? {}))
const otherAttrRows = computed(() =>
  Object.entries(attrs.value)
    .filter(([key]) => key !== 'name' && key !== 'description')
    .map(([key, value]) => ({ key, value })),
)
const status = computed(() => statusFor(detail.value ?? {}))
const label = computed(() => detail.value?.label ?? labelFor(detail.value ?? {}))

async function load() {
  if (!code.value || !store.selectedType) {
    detail.value = null
    localizations.value = []
    references.value = []
    return
  }
  loading.value = true
  try {
    const [detRes, locRes, refRes] = await Promise.all([
      store.fetchItemDetail(code.value),
      listItemLocalizations(store.context, store.selectedType, code.value),
      listItemReferences(store.context, store.selectedType, code.value),
    ])
    detail.value = detRes
    localizations.value = locRes?.localizations ?? []
    references.value = refRes?.references ?? []
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not load item', detail: err.message, life: 4000 })
  } finally {
    loading.value = false
  }
}

watch(
  () => [store.selectedCode, store.selectedType, store.selectedLocale],
  () => load(),
  { immediate: true },
)

// ── Item lifecycle actions (moved from the old ItemGrid row actions) ─────────

async function onDeprecate() {
  try {
    await deprecateItem(store.selectedType, store.context, code.value)
    await Promise.all([store.refreshItems(), load()])
    toast.add({ severity: 'success', summary: `${code.value} deprecated`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not deprecate', detail: err.message, life: 4000 })
  }
}

async function onReactivate() {
  try {
    await reactivateItem(store.selectedType, store.context, code.value)
    await Promise.all([store.refreshItems(), load()])
    toast.add({ severity: 'success', summary: `${code.value} reactivated`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not reactivate', detail: err.message, life: 4000 })
  }
}

async function onDelete() {
  const deleted = code.value
  try {
    await deleteItem(store.selectedType, store.context, deleted)
    await store.refreshItems()
    await store.refreshTypeCounts()
    toast.add({ severity: 'success', summary: `${deleted} deleted`, life: 2500 })
  } catch (err) {
    // BR-D02: a referenced item can't be hard-deleted — surface the fix.
    const referenced = err.message?.includes('referenced')
    toast.add({
      severity: 'error',
      summary: referenced ? 'Item is referenced' : 'Could not delete',
      detail: referenced ? 'This item is referenced by another item — deprecate it instead.' : err.message,
      life: 5000,
    })
  }
}

// ── Header actions: Edit (primary) + overflow menu (secondary/lifecycle) ─────
// Icon-only header buttons were easy to miss; Edit is now a labelled primary
// action and the lifecycle/destructive actions live in one overflow menu.

const actionMenu = ref(null)

function toggleActionMenu(event) {
  actionMenu.value.toggle(event)
}

const actionMenuItems = computed(() => {
  const active = status.value === 'active'
  return [
    active
      ? { label: 'Deprecate', icon: 'pi pi-eye-slash', command: onDeprecate }
      : { label: 'Reactivate', icon: 'pi pi-eye', command: onReactivate },
    { separator: true },
    { label: 'Delete', icon: 'pi pi-trash', class: 'danger-item', command: onDelete },
  ]
})

// Edit always edits the record's Details fields, so jump there first — clearer
// than silently arming an editor on a tab the admin can't see.
function onHeaderEdit() {
  activeTab.value = 'details'
  startEditGeneral()
}

// ── General tab: default label / description (BR-D18 full attrs replace) ────

const editingGeneral = ref(false)
const generalLabel = ref('')
const generalDescription = ref('')

function startEditGeneral() {
  generalLabel.value = attrs.value.name || ''
  generalDescription.value = attrs.value.description || ''
  editingGeneral.value = true
}

function cancelEditGeneral() {
  editingGeneral.value = false
}

async function submitGeneral() {
  if (!generalLabel.value.trim()) return
  try {
    const nextAttrs = { ...attrs.value, name: generalLabel.value.trim() }
    if (generalDescription.value.trim()) nextAttrs.description = generalDescription.value.trim()
    else delete nextAttrs.description
    await updateItem(store.selectedType, store.context, code.value, nextAttrs)
    editingGeneral.value = false
    await Promise.all([store.refreshItems(), load()])
    toast.add({ severity: 'success', summary: 'Item updated', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not update item', detail: err.message, life: 4000 })
  }
}

// ── Translations tab ──────────────────────────────────────────────────────────
// Every registered locale gets a row (Default / Complete / Missing), not just
// the locales that already have an explicit localization — that's the whole
// point of surfacing "Missing" as a first-class status.

const translationQuery = ref('')
const missingOnly = ref(false)

const translationRows = computed(() =>
  buildTranslationRows({
    locales: store.locales,
    defaultLocale: store.defaultLocale,
    localizations: localizations.value,
    defaultLabel: attrs.value.name,
  }),
)
const filteredTranslationRows = computed(() =>
  filterTranslationRows(translationRows.value, {
    query: translationQuery.value,
    missingOnly: missingOnly.value,
  }),
)

const editingLocale = ref(null)
const editLabel = ref('')
const editDescription = ref('')

function startEditTranslation(row) {
  editingLocale.value = row.locale
  editLabel.value = row.translation || ''
  editDescription.value = row.description || ''
}

function cancelEditTranslation() {
  editingLocale.value = null
}

// setLocalization upserts — the same call creates a translation for a
// currently-"Missing" locale or updates an existing one.
async function submitTranslation(row) {
  if (!editLabel.value.trim()) return
  try {
    await setLocalization({
      typeKey: store.selectedType,
      code: code.value,
      context: store.context,
      locale: row.locale,
      label: editLabel.value.trim(),
      description: editDescription.value.trim(),
    })
    editingLocale.value = null
    await Promise.all([store.refreshItems(), load()])
    toast.add({ severity: 'success', summary: 'Translation saved', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not save translation', detail: err.message, life: 4000 })
  }
}

const newLocale = ref('')
const newLocaleTaken = computed(() => store.locales.includes(newLocale.value.trim()))

async function submitAddLocale() {
  const locale = newLocale.value.trim()
  if (!locale || newLocaleTaken.value) return
  try {
    await store.addLocaleToContext(locale, false)
    newLocale.value = ''
    toast.add({ severity: 'success', summary: `Locale ${locale} added`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not add locale', detail: err.message, life: 4000 })
  }
}

function translationSeverity(rowStatus) {
  if (rowStatus === 'default') return 'info'
  if (rowStatus === 'complete') return 'success'
  return 'warning'
}

// ── Usage tab (formerly References) ──────────────────────────────────────────

const refRelation = ref('')
const refTargetType = ref('')
const refTargetCode = ref('')

const targetTypeOptions = computed(() => store.types.map((t) => t.typeKey))

async function submitReference() {
  if (!refRelation.value.trim() || !refTargetType.value || !refTargetCode.value.trim()) return
  try {
    await createReference({
      context: store.context,
      fromTypeKey: store.selectedType,
      fromCode: code.value,
      relation: refRelation.value.trim(),
      declaredTargetType: refTargetType.value,
      toTypeKey: refTargetType.value,
      toCode: refTargetCode.value.trim(),
    })
    refRelation.value = ''
    refTargetType.value = ''
    refTargetCode.value = ''
    await load()
    toast.add({ severity: 'success', summary: 'Reference created', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not create reference', detail: err.message, life: 4000 })
  }
}
</script>

<template>
  <div class="detail-panel">
    <p
      v-if="!code"
      class="lab-muted empty-hint"
    >
      Select an item to see its details.
    </p>
    <template v-else>
      <div class="detail-head">
        <div class="detail-identity">
          <h4>
            {{ code }}<span
              v-if="label"
              class="detail-label"
            > — {{ label }}</span>
          </h4>
          <Tag
            v-if="status"
            :severity="status === 'active' ? 'success' : 'warning'"
            :value="status"
          />
        </div>
        <div class="detail-actions">
          <Button
            label="Edit"
            icon="pi pi-pencil"
            size="small"
            :disabled="editingGeneral"
            @click="onHeaderEdit"
          />
          <Button
            icon="pi pi-ellipsis-v"
            text
            size="small"
            aria-label="More actions"
            aria-haspopup="true"
            @click="toggleActionMenu"
          />
          <Menu
            ref="actionMenu"
            :model="actionMenuItems"
            popup
          />
        </div>
      </div>

      <Tabs v-model:value="activeTab">
        <TabList>
          <Tab value="details">
            Details
          </Tab>
          <Tab value="translations">
            Translations
          </Tab>
          <Tab value="usage">
            Usage
          </Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="details">
            <div class="general-fields">
              <div class="general-row">
                <label class="lab-muted">Key</label>
                <span class="general-key">{{ code }}</span>
              </div>
              <div class="general-row">
                <label class="lab-muted">Default label</label>
                <InputText
                  v-if="editingGeneral"
                  v-model="generalLabel"
                  size="small"
                />
                <span v-else>{{ attrs.name || '—' }}</span>
              </div>
              <div class="general-row">
                <label class="lab-muted">Description</label>
                <InputText
                  v-if="editingGeneral"
                  v-model="generalDescription"
                  size="small"
                  placeholder="optional"
                />
                <span
                  v-else
                  class="lab-muted"
                >{{ attrs.description || '—' }}</span>
              </div>
              <div
                v-if="editingGeneral"
                class="general-actions"
              >
                <Button
                  label="Save"
                  icon="pi pi-check"
                  size="small"
                  :disabled="!generalLabel.trim()"
                  @click="submitGeneral"
                />
                <Button
                  label="Cancel"
                  text
                  size="small"
                  @click="cancelEditGeneral"
                />
              </div>
            </div>
            <template v-if="otherAttrRows.length > 0">
              <h5 class="lab-muted section-label">
                Other attributes
              </h5>
              <DataTable
                :value="otherAttrRows"
                size="small"
                data-key="key"
                resizable-columns
                column-resize-mode="fit"
              >
                <Column
                  field="key"
                  header="Key"
                />
                <Column header="Value">
                  <template #body="{ data }">
                    {{ typeof data.value === 'object' ? JSON.stringify(data.value) : data.value }}
                  </template>
                </Column>
              </DataTable>
            </template>
          </TabPanel>
          <TabPanel value="translations">
            <div class="translations-toolbar">
              <InputText
                v-model="translationQuery"
                placeholder="Search locales…"
                size="small"
                style="flex: 1 1 10rem"
              />
              <div class="missing-toggle">
                <label class="lab-muted">Missing only</label>
                <ToggleSwitch v-model="missingOnly" />
              </div>
            </div>
            <div class="table-scroll">
            <DataTable
              :value="filteredTranslationRows"
              size="small"
              data-key="locale"
              resizable-columns
              column-resize-mode="fit"
            >
              <template #empty>
                No locales registered yet.
              </template>
              <Column
                field="locale"
                header="Locale"
                style="width: 6rem; font-family: monospace"
              />
              <Column
                field="displayName"
                header="Display name"
              />
              <Column header="Translation">
                <template #body="{ data }">
                  <InputText
                    v-if="editingLocale === data.locale"
                    v-model="editLabel"
                    size="small"
                    style="width: 100%"
                  />
                  <span v-else>{{ data.translation || '—' }}</span>
                </template>
              </Column>
              <Column header="Description">
                <template #body="{ data }">
                  <InputText
                    v-if="editingLocale === data.locale"
                    v-model="editDescription"
                    size="small"
                    style="width: 100%"
                  />
                  <span
                    v-else
                    class="lab-muted"
                  >{{ data.description || '—' }}</span>
                </template>
              </Column>
              <Column header="Status">
                <template #body="{ data }">
                  <Tag
                    :severity="translationSeverity(data.status)"
                    :value="data.status"
                  />
                </template>
              </Column>
              <Column header="Actions">
                <template #body="{ data }">
                  <div
                    v-if="editingLocale === data.locale"
                    class="row-actions"
                  >
                    <Button
                      icon="pi pi-check"
                      text
                      size="small"
                      aria-label="Save"
                      :disabled="!editLabel.trim()"
                      @click="submitTranslation(data)"
                    />
                    <Button
                      icon="pi pi-times"
                      text
                      size="small"
                      aria-label="Cancel"
                      @click="cancelEditTranslation"
                    />
                  </div>
                  <Button
                    v-else
                    icon="pi pi-pencil"
                    text
                    size="small"
                    aria-label="Edit translation"
                    @click="startEditTranslation(data)"
                  />
                </template>
              </Column>
            </DataTable>
            </div>
            <div class="add-row">
              <InputText
                v-model="newLocale"
                placeholder="new locale (e.g. de-DE)"
                size="small"
                style="flex: 0 0 10rem"
              />
              <Button
                icon="pi pi-plus"
                label="Add locale"
                size="small"
                :disabled="!newLocale.trim() || newLocaleTaken"
                :title="newLocaleTaken ? 'That locale is already registered.' : null"
                @click="submitAddLocale"
              />
            </div>
          </TabPanel>
          <TabPanel value="usage">
            <DataTable
              :value="references"
              size="small"
              data-key="relation"
              resizable-columns
              column-resize-mode="fit"
            >
              <template #empty>
                No outbound references yet.
              </template>
              <Column
                field="relation"
                header="Relation"
              />
              <Column
                field="toTypeKey"
                header="Target type"
              />
              <Column
                field="toCode"
                header="Target code"
              />
            </DataTable>
            <div class="add-row">
              <InputText
                v-model="refRelation"
                placeholder="relation (e.g. defaultCurrency)"
                size="small"
                style="flex: 2 1 8rem"
              />
              <Select
                v-model="refTargetType"
                :options="targetTypeOptions"
                placeholder="target type"
                size="small"
                style="flex: 1 1 7rem"
              />
              <InputText
                v-model="refTargetCode"
                placeholder="target code"
                size="small"
                style="flex: 1 1 6rem"
              />
              <Button
                icon="pi pi-plus"
                size="small"
                aria-label="Add reference"
                :disabled="!refRelation.trim() || !refTargetType || !refTargetCode.trim()"
                @click="submitReference"
              />
            </div>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </template>
  </div>
</template>

<style scoped>
.detail-panel {
  min-width: 0;
}
.empty-hint {
  padding: 1rem 0.25rem;
}
.detail-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.25rem;
}
.detail-identity {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}
.detail-identity h4 {
  margin: 0;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.detail-label {
  font-weight: 400;
  color: var(--p-text-muted-color);
}
.detail-actions {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex: 0 0 auto;
}
/* Destructive action in the overflow menu reads as danger without shouting. */
:deep(.danger-item) .p-menu-item-link .p-menu-item-icon,
:deep(.danger-item) .p-menu-item-link .p-menu-item-label {
  color: var(--p-red-400, #e5484d);
}
.general-fields {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.general-row {
  display: grid;
  grid-template-columns: 7rem 1fr;
  align-items: center;
  gap: 0.5rem;
}
.general-row label {
  font-size: 11px;
}
.general-key {
  font-family: monospace;
  font-size: 13px;
}
.general-actions {
  display: flex;
  gap: 0.5rem;
}
.section-label {
  margin: 0.75rem 0 0.35rem;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.translations-toolbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
  flex-wrap: wrap;
}
.missing-toggle {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.missing-toggle label {
  font-size: 11px;
}
.add-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  margin-top: 0.75rem;
  flex-wrap: wrap;
}
.row-actions {
  display: flex;
  gap: 2px;
}
/* Let flex inputs shrink below their intrinsic content width (default
   min-width:auto is what pushed the + button off the right edge). */
.add-row :deep(.p-inputtext),
.add-row :deep(.p-select) {
  min-width: 0;
}
/* Keep the + button its natural square size — never squashed, never grown. */
.add-row :deep(.p-button) {
  flex: 0 0 auto;
}
/* The panel sits in a half-width column — drop the tab panels' side padding
   so tables use the full width. */
:deep(.p-tabpanels) {
  padding-left: 0;
  padding-right: 0;
}
/* Below ~700px this table's columns no longer fit and PrimeVue's own
   overflow-x:auto container takes over scrolling — that mechanism already
   works, it just isn't visually obvious, so hint it with a right-edge fade
   rather than adding a second (redundant) scroll container. */
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
</style>
