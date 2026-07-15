<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
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
} from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()
const toast = useToast()

const detail = ref(null)
const localizations = ref([])
const references = ref([])
const loading = ref(false)

const code = computed(() => store.selectedCode)
const attrs = computed(() => detail.value?.item?.attrs ?? detail.value?.attrs ?? {})
const attrRows = computed(() => Object.entries(attrs.value).map(([key, value]) => ({ key, value })))
const status = computed(() => detail.value?.item?.status ?? detail.value?.status ?? '')
const label = computed(() => detail.value?.label ?? attrs.value.name ?? '')

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

// ── Localization form ────────────────────────────────────────────────────────

const locLocale = ref('')
const locLabel = ref('')
const locDescription = ref('')

function localeExists(locale) {
  return localizations.value.some((l) => l.locale === locale.trim())
}

async function submitLocalization() {
  if (!locLocale.value.trim() || !locLabel.value.trim() || localeExists(locLocale.value)) return
  try {
    await setLocalization({
      typeKey: store.selectedType,
      code: code.value,
      context: store.context,
      locale: locLocale.value.trim(),
      label: locLabel.value.trim(),
      description: locDescription.value.trim(),
    })
    locLocale.value = ''
    locLabel.value = ''
    locDescription.value = ''
    await Promise.all([store.refreshItems(), load()])
    toast.add({ severity: 'success', summary: 'Localization added', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not add localization', detail: err.message, life: 4000 })
  }
}

// ── Inline update (existing locale) ──────────────────────────────────────────

const editingLocale = ref(null)
const editLabel = ref('')
const editDescription = ref('')

function startEdit(row) {
  editingLocale.value = row.locale
  editLabel.value = row.label || ''
  editDescription.value = row.description || ''
}

function cancelEdit() {
  editingLocale.value = null
}

async function submitUpdate(row) {
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
    toast.add({ severity: 'success', summary: 'Localization updated', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not update localization', detail: err.message, life: 4000 })
  }
}

// ── Reference form ───────────────────────────────────────────────────────────

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
            icon="pi pi-eye-slash"
            text
            size="small"
            aria-label="Deprecate"
            :disabled="status === 'deprecated'"
            @click="onDeprecate"
          />
          <Button
            icon="pi pi-eye"
            text
            size="small"
            aria-label="Reactivate"
            :disabled="status !== 'deprecated'"
            @click="onReactivate"
          />
          <Button
            icon="pi pi-trash"
            text
            severity="danger"
            size="small"
            aria-label="Delete"
            @click="onDelete"
          />
        </div>
      </div>

      <Tabs value="attrs">
        <TabList>
          <Tab value="attrs">
            Attrs
          </Tab>
          <Tab value="localizations">
            Localizations
          </Tab>
          <Tab value="references">
            References
          </Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="attrs">
            <DataTable
              :value="attrRows"
              size="small"
              data-key="key"
            >
              <template #empty>
                No attributes on this item.
              </template>
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
          </TabPanel>
          <TabPanel value="localizations">
            <div class="add-row">
              <InputText
                v-model="locLocale"
                placeholder="locale (e.g. de-DE)"
                size="small"
                style="flex: 0 0 7rem"
              />
              <InputText
                v-model="locLabel"
                placeholder="label"
                size="small"
                style="flex: 1 1 7rem"
              />
              <InputText
                v-model="locDescription"
                placeholder="description (optional)"
                size="small"
                style="flex: 2 1 8rem"
              />
              <Button
                icon="pi pi-plus"
                label="Add"
                size="small"
                aria-label="Add localization"
                :disabled="!locLocale.trim() || !locLabel.trim() || localeExists(locLocale)"
                :title="localeExists(locLocale) ? 'That locale already exists — use Update on its row instead.' : null"
                @click="submitLocalization"
              />
            </div>
            <DataTable
              :value="localizations"
              size="small"
              data-key="locale"
            >
              <template #empty>
                No localizations yet.
              </template>
              <Column
                field="locale"
                header="Locale"
              />
              <Column header="Label">
                <template #body="{ data }">
                  <InputText
                    v-if="editingLocale === data.locale"
                    v-model="editLabel"
                    size="small"
                    style="width: 100%"
                  />
                  <span v-else>{{ data.label }}</span>
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
                  <span v-else>{{ data.description }}</span>
                </template>
              </Column>
              <Column
                field="source"
                header="Source"
              />
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
                      @click="submitUpdate(data)"
                    />
                    <Button
                      icon="pi pi-times"
                      text
                      size="small"
                      aria-label="Cancel"
                      @click="cancelEdit"
                    />
                  </div>
                  <Button
                    v-else
                    icon="pi pi-pencil"
                    text
                    size="small"
                    aria-label="Update"
                    @click="startEdit(data)"
                  />
                </template>
              </Column>
            </DataTable>
          </TabPanel>
          <TabPanel value="references">
            <DataTable
              :value="references"
              size="small"
              data-key="relation"
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
  color: var(--lab-muted, #888);
}
.detail-actions {
  display: flex;
  gap: 2px;
  flex: 0 0 auto;
}
.add-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  margin-bottom: 0.75rem;
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
</style>
