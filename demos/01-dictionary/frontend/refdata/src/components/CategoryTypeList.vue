<script setup>
import Badge from 'primevue/badge'
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Menu from 'primevue/menu'
import SelectButton from 'primevue/selectbutton'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { deleteItem, deprecateItem, reactivateItem, registerItem, registerType } from '../api'
import { categoryLabel } from '../categories'
import { attrsFor, codeFor, statusFor } from '../itemFields'
import { useDictionaryStore } from '../stores/dictionary'
import ItemDetailPanel from './ItemDetailPanel.vue'
import TranslationMatrix from './TranslationMatrix.vue'

const store = useDictionaryStore()
const toast = useToast()

const types = computed(() =>
  store.types
    .filter((t) => (t.category || 'standards') === store.selectedCategory)
    .sort((a, b) => a.typeKey.localeCompare(b.typeKey)),
)

// The singular noun for this category, used on the register button/dialog so
// the label reads "Register enum" rather than the plural section title.
const singular = computed(() => {
  const label = categoryLabel(store.selectedCategory)
  return label.endsWith('s') ? label.slice(0, -1) : label
})

const selectedMeta = computed(() => store.types.find((t) => t.typeKey === store.selectedType))

// Values vs bulk Translation Matrix (Phase 11.11). Reset to Values whenever
// the selected type changes so switching enums doesn't strand the admin on a
// half-loaded matrix for the previous type.
const VIEW_OPTIONS = ['Values', 'Translation Matrix']
const viewMode = ref('Values')
watch(() => store.selectedType, () => { viewMode.value = 'Values' })

// The "Default label" column is deliberately attrs.name, not the
// locale-resolved label ItemGrid shows — this table's whole point is
// surfacing the authored default, independent of whatever locale happens to
// be selected elsewhere in the app.
const entries = computed(() =>
  store.items.map((item) => ({
    code: codeFor(item),
    label: attrsFor(item).name || codeFor(item),
    status: statusFor(item),
  })),
)

const filter = ref('')
watch(() => store.selectedType, () => { filter.value = '' })

const filteredEntries = computed(() => {
  const q = filter.value.trim().toLowerCase()
  if (!q) return entries.value
  return entries.value.filter(
    (e) => e.code.toLowerCase().includes(q) || e.label.toLowerCase().includes(q),
  )
})

function select(typeKey) {
  if (typeKey !== store.selectedType) store.selectCategoryType(typeKey)
}

// ── Register a new type in this category ─────────────────────────────────────

const addTypeVisible = ref(false)
const newTypeKey = ref('')
const newTypeName = ref('')
const newTypeDescription = ref('')

function openAddType() {
  newTypeKey.value = ''
  newTypeName.value = ''
  newTypeDescription.value = ''
  addTypeVisible.value = true
}

const typeKeyTaken = computed(() =>
  store.types.some((t) => t.typeKey === newTypeKey.value.trim()),
)

async function submitAddType() {
  const key = newTypeKey.value.trim()
  if (!key || typeKeyTaken.value) return
  try {
    await registerType({
      typeKey: key,
      name: newTypeName.value.trim() || key,
      description: newTypeDescription.value.trim(),
      category: store.selectedCategory,
    })
    addTypeVisible.value = false
    await store.refreshTypes()
    await store.selectCategoryType(key)
    toast.add({ severity: 'success', summary: `${singular.value} registered`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: `Could not register ${singular.value.toLowerCase()}`, detail: err.message, life: 4000 })
  }
}

// ── Edit an existing type's name/description ──────────────────────────────────
// The key is the type's identity and stays read-only here — registerType is
// an upsert keyed by typeKey (POST .../admin/types ON CONFLICT DO UPDATE), so
// the same call that creates a type also renames/re-describes it in place.

const editTypeVisible = ref(false)
const editTypeKey = ref('')
const editTypeName = ref('')
const editTypeDescription = ref('')

function openEditType(t) {
  editTypeKey.value = t.typeKey
  editTypeName.value = t.name
  editTypeDescription.value = t.description || ''
  editTypeVisible.value = true
}

async function submitEditType() {
  if (!editTypeName.value.trim()) return
  try {
    await registerType({
      typeKey: editTypeKey.value,
      name: editTypeName.value.trim(),
      description: editTypeDescription.value.trim(),
      category: store.selectedCategory,
    })
    editTypeVisible.value = false
    await store.refreshTypes()
    toast.add({ severity: 'success', summary: `${singular.value} updated`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: `Could not update ${singular.value.toLowerCase()}`, detail: err.message, life: 4000 })
  }
}

// ── Add a value (item) to the selected type ───────────────────────────────────

const addValueVisible = ref(false)
const newValueCode = ref('')
const newValueLabel = ref('')

function openAddValue() {
  newValueCode.value = ''
  newValueLabel.value = ''
  addValueVisible.value = true
}

const valueCodeTaken = computed(() =>
  store.items.some((i) => codeFor(i) === newValueCode.value.trim()),
)

async function submitAddValue() {
  const code = newValueCode.value.trim()
  if (!code || valueCodeTaken.value) return
  try {
    await registerItem({
      typeKey: store.selectedType,
      code,
      context: store.context,
      attrs: newValueLabel.value.trim() ? { name: newValueLabel.value.trim() } : {},
    })
    addValueVisible.value = false
    await store.refreshItems()
    await store.refreshTypeCounts()
    store.selectItem(code)
    toast.add({ severity: 'success', summary: 'Value registered', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not register value', detail: err.message, life: 4000 })
  }
}

// ── Per-row overflow menu: Edit · Deactivate/Reactivate · Duplicate · Delete ──

const rowMenu = ref()
const menuEntry = ref(null)

function openRowMenu(event, entry) {
  menuEntry.value = entry
  rowMenu.value.toggle(event)
}

const rowMenuItems = computed(() => {
  const entry = menuEntry.value
  if (!entry) return []
  const active = entry.status === 'active'
  return [
    { label: 'Edit', icon: 'pi pi-pencil', command: () => store.selectItem(entry.code) },
    active
      ? { label: 'Deactivate', icon: 'pi pi-eye-slash', command: () => onDeprecate(entry.code) }
      : { label: 'Reactivate', icon: 'pi pi-eye', command: () => onReactivate(entry.code) },
    { label: 'Duplicate', icon: 'pi pi-copy', command: () => openDuplicate(entry) },
    { label: 'Delete', icon: 'pi pi-trash', command: () => onDelete(entry.code) },
  ]
})

async function onDeprecate(code) {
  try {
    await deprecateItem(store.selectedType, store.context, code)
    await store.refreshItems()
    toast.add({ severity: 'success', summary: `${code} deprecated`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not deprecate', detail: err.message, life: 4000 })
  }
}

async function onReactivate(code) {
  try {
    await reactivateItem(store.selectedType, store.context, code)
    await store.refreshItems()
    toast.add({ severity: 'success', summary: `${code} reactivated`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not reactivate', detail: err.message, life: 4000 })
  }
}

async function onDelete(code) {
  try {
    await deleteItem(store.selectedType, store.context, code)
    await store.refreshItems()
    await store.refreshTypeCounts()
    toast.add({ severity: 'success', summary: `${code} deleted`, life: 2500 })
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

// ── Duplicate: clone a value under a new key (attrs copied, translations start empty) ──

const duplicateVisible = ref(false)
const duplicateSource = ref(null)
const duplicateCode = ref('')

function openDuplicate(entry) {
  duplicateSource.value = entry
  duplicateCode.value = ''
  duplicateVisible.value = true
}

const duplicateCodeTaken = computed(() =>
  store.items.some((i) => codeFor(i) === duplicateCode.value.trim()),
)

async function submitDuplicate() {
  const code = duplicateCode.value.trim()
  if (!code || duplicateCodeTaken.value || !duplicateSource.value) return
  try {
    const sourceItem = store.items.find((i) => codeFor(i) === duplicateSource.value.code)
    await registerItem({
      typeKey: store.selectedType,
      code,
      context: store.context,
      attrs: { ...attrsFor(sourceItem) },
    })
    duplicateVisible.value = false
    await store.refreshItems()
    await store.refreshTypeCounts()
    store.selectItem(code)
    toast.add({ severity: 'success', summary: `${code} created from ${duplicateSource.value.code}`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not duplicate value', detail: err.message, life: 4000 })
  }
}
</script>

<template>
  <div class="lab-panel category-view fill-height">
    <div class="category-head">
      <h3>{{ categoryLabel(store.selectedCategory) }}</h3>
      <span class="lab-muted type-count">{{ types.length }} {{ types.length === 1 ? 'type' : 'types' }}</span>
    </div>

    <div class="master-detail">
      <!-- Left: the scrollable list of types in this category. -->
      <div class="type-pane">
        <Button
          class="register-btn"
          icon="pi pi-plus"
          :label="`New ${singular.toLowerCase()}`"
          size="small"
          @click="openAddType"
        />
        <DataTable
          :value="types"
          size="small"
          data-key="typeKey"
          selection-mode="single"
          :selection="types.find((t) => t.typeKey === store.selectedType) ?? null"
          sort-field="typeKey"
          :sort-order="1"
          scrollable
          scroll-height="flex"
          resizable-columns
          column-resize-mode="fit"
          class="type-table"
          @row-click="select($event.data.typeKey)"
        >
          <template #empty>
            No {{ singular.toLowerCase() }}s registered yet. Use New {{ singular.toLowerCase() }} to add one.
          </template>
          <Column
            field="typeKey"
            header="Key"
            sortable
            style="font-family: monospace; max-width: 8rem"
          >
            <template #body="{ data }">
              <span
                class="value-key"
                :title="data.typeKey"
              >{{ data.typeKey }}</span>
            </template>
          </Column>
          <Column
            field="name"
            header="Name"
            sortable
          >
            <template #body="{ data }">
              <span
                class="value-label"
                :title="data.name"
              >{{ data.name }}</span>
            </template>
          </Column>
          <Column
            header=""
            style="width: 4.5rem"
          >
            <template #body="{ data }">
              <span class="row-meta">
                <Badge
                  :value="store.typeCounts[data.typeKey] ?? 0"
                  severity="secondary"
                />
                <Button
                  icon="pi pi-pencil"
                  text
                  size="small"
                  :aria-label="`Edit ${data.name}`"
                  class="edit-type-btn"
                  @click.stop="openEditType(data)"
                />
              </span>
            </template>
          </Column>
        </DataTable>
      </div>

      <!-- Right: values (table or bulk matrix) + selected-value detail. -->
      <div class="content-pane">
        <template v-if="store.selectedType">
          <div class="values-head">
            <h4>{{ selectedMeta?.name || store.selectedType }}</h4>
            <span class="lab-muted">{{ entries.length }} {{ entries.length === 1 ? 'value' : 'values' }}</span>
            <SelectButton
              v-model="viewMode"
              :options="VIEW_OPTIONS"
              :allow-empty="false"
              class="view-toggle"
            />
          </div>

          <template v-if="viewMode === 'Values'">
            <div class="values-toolbar">
              <Button
                icon="pi pi-plus"
                label="Add value"
                size="small"
                @click="openAddValue"
              />
              <InputText
                v-model="filter"
                placeholder="Search…"
                size="small"
                class="values-search"
              />
            </div>
            <div class="values-detail-split">
              <DataTable
                :value="filteredEntries"
                size="small"
                data-key="code"
                selection-mode="single"
                :selection="filteredEntries.find((e) => e.code === store.selectedCode) ?? null"
                sort-field="code"
                :sort-order="1"
                scrollable
                scroll-height="flex"
                resizable-columns
                column-resize-mode="fit"
                class="values-table"
                @row-click="store.selectItem($event.data.code)"
              >
                <template #empty>
                  No values in this {{ singular.toLowerCase() }} yet.
                </template>
                <Column
                  field="code"
                  header="Key"
                  sortable
                  style="font-family: monospace; width: 14rem"
                >
                  <template #body="{ data }">
                    <span
                      class="value-key"
                      :title="data.code"
                    >{{ data.code }}</span>
                  </template>
                </Column>
                <Column
                  field="label"
                  header="Default label"
                  sortable
                >
                  <template #body="{ data }">
                    <span
                      class="value-label"
                      :title="data.label"
                    >{{ data.label }}</span>
                  </template>
                </Column>
                <Column
                  field="status"
                  header="Status"
                  sortable
                  style="width: 6rem"
                >
                  <template #body="{ data }">
                    <Tag
                      :severity="data.status === 'active' ? 'success' : 'warning'"
                      :value="data.status"
                    />
                  </template>
                </Column>
                <Column
                  header=""
                  style="width: 2.5rem"
                >
                  <template #body="{ data }">
                    <Button
                      icon="pi pi-ellipsis-v"
                      text
                      size="small"
                      aria-label="Value actions"
                      @click.stop="openRowMenu($event, data)"
                    />
                  </template>
                </Column>
              </DataTable>

              <ItemDetailPanel class="detail-pane" />
            </div>
          </template>

          <TranslationMatrix
            v-else
            :type-key="store.selectedType"
          />
        </template>
        <p
          v-else
          class="lab-muted empty"
        >
          Select a {{ singular.toLowerCase() }} to see its values.
        </p>
      </div>
    </div>

    <Menu
      ref="rowMenu"
      :model="rowMenuItems"
      popup
    />

    <Dialog
      v-model:visible="addTypeVisible"
      :header="`New ${singular.toLowerCase()}`"
      modal
      style="width: 26rem"
    >
      <div class="field">
        <label
          class="lab-muted"
          for="new-type-key"
        >Key</label>
        <InputText
          id="new-type-key"
          v-model="newTypeKey"
          style="width: 100%"
          placeholder="e.g. container-status"
        />
        <small
          v-if="typeKeyTaken"
          class="key-taken"
        >That key is already registered.</small>
      </div>
      <div class="field">
        <label
          class="lab-muted"
          for="new-type-name"
        >Name</label>
        <InputText
          id="new-type-name"
          v-model="newTypeName"
          style="width: 100%"
          placeholder="Display name (defaults to the key)"
        />
      </div>
      <div class="field">
        <label
          class="lab-muted"
          for="new-type-desc"
        >Description</label>
        <InputText
          id="new-type-desc"
          v-model="newTypeDescription"
          style="width: 100%"
          placeholder="optional"
        />
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          size="small"
          @click="addTypeVisible = false"
        />
        <Button
          :label="`New ${singular.toLowerCase()}`"
          size="small"
          :disabled="!newTypeKey.trim() || typeKeyTaken"
          @click="submitAddType"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="editTypeVisible"
      :header="`Edit ${singular.toLowerCase()}`"
      modal
      style="width: 26rem"
    >
      <div class="field">
        <label class="lab-muted">Key</label>
        <InputText
          :model-value="editTypeKey"
          style="width: 100%"
          disabled
        />
      </div>
      <div class="field">
        <label
          class="lab-muted"
          for="edit-type-name"
        >Name</label>
        <InputText
          id="edit-type-name"
          v-model="editTypeName"
          style="width: 100%"
        />
      </div>
      <div class="field">
        <label
          class="lab-muted"
          for="edit-type-desc"
        >Description</label>
        <InputText
          id="edit-type-desc"
          v-model="editTypeDescription"
          style="width: 100%"
          placeholder="optional"
        />
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          size="small"
          @click="editTypeVisible = false"
        />
        <Button
          label="Save"
          size="small"
          :disabled="!editTypeName.trim()"
          @click="submitEditType"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="addValueVisible"
      header="Add value"
      modal
      style="width: 24rem"
    >
      <div class="field">
        <label
          class="lab-muted"
          for="new-value-code"
        >Key</label>
        <InputText
          id="new-value-code"
          v-model="newValueCode"
          style="width: 100%"
          placeholder="e.g. at-anchor"
        />
        <small
          v-if="valueCodeTaken"
          class="key-taken"
        >That key is already registered.</small>
      </div>
      <div class="field">
        <label
          class="lab-muted"
          for="new-value-label"
        >Default label</label>
        <InputText
          id="new-value-label"
          v-model="newValueLabel"
          style="width: 100%"
          placeholder="e.g. At Anchor"
        />
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          size="small"
          @click="addValueVisible = false"
        />
        <Button
          label="Add value"
          size="small"
          :disabled="!newValueCode.trim() || valueCodeTaken"
          @click="submitAddValue"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="duplicateVisible"
      header="Duplicate value"
      modal
      style="width: 24rem"
    >
      <p
        v-if="duplicateSource"
        class="lab-muted"
      >
        Copying <strong>{{ duplicateSource.code }}</strong> ({{ duplicateSource.label }}). Translations
        are not copied — the new value starts untranslated.
      </p>
      <div class="field">
        <label
          class="lab-muted"
          for="dup-code"
        >New key</label>
        <InputText
          id="dup-code"
          v-model="duplicateCode"
          style="width: 100%"
          placeholder="e.g. at-anchor-copy"
        />
        <small
          v-if="duplicateCodeTaken"
          class="key-taken"
        >That key is already registered.</small>
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          size="small"
          @click="duplicateVisible = false"
        />
        <Button
          label="Duplicate"
          size="small"
          :disabled="!duplicateCode.trim() || duplicateCodeTaken"
          @click="submitDuplicate"
        />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.category-head {
  display: flex;
  align-items: baseline;
  gap: 0.6rem;
  margin-bottom: 0.75rem;
}
.category-head h3 {
  margin: 0;
}
.type-count {
  font-size: 11px;
}
.master-detail {
  display: grid;
  grid-template-columns: minmax(10rem, 1fr) 4fr;
  gap: 0.75rem;
  flex: 1;
  min-height: 0;
}
.category-view {
  min-height: 0;
}
@media (max-width: 900px) {
  .master-detail {
    grid-template-columns: 1fr;
  }
}

/* ── Type list (scrollable) ── */
.type-pane {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--lab-disabled-bg);
  padding-right: 0.75rem;
}
.type-table {
  flex: 1;
  min-height: 0;
}
@media (max-width: 900px) {
  .type-pane {
    border-right: none;
    padding-right: 0;
  }
}
.register-btn {
  width: 100%;
  margin-bottom: 0.5rem;
}
.type-table :deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
.row-meta {
  display: flex;
  align-items: center;
  gap: 0.15rem;
  flex: 0 0 auto;
}
.edit-type-btn {
  color: var(--p-text-muted-color);
}
.type-table :deep(.p-datatable-tbody > tr.p-datatable-row-selected) .edit-type-btn {
  color: inherit;
}

/* ── Content pane: values head, toolbar, values+detail split or matrix ── */
.content-pane {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.values-head {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  margin-bottom: 0.6rem;
  flex-wrap: wrap;
}
.values-head h4 {
  margin: 0;
  font-size: 14px;
  text-transform: capitalize;
}
.view-toggle {
  margin-left: auto;
}
.view-toggle :deep(.p-togglebutton) {
  font-size: 11px;
  padding: 0.3rem 0.6rem;
}
.values-toolbar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.values-search {
  flex: 0 1 14rem;
}
.values-detail-split {
  display: grid;
  grid-template-columns: minmax(15rem, 1fr) 1fr;
  gap: 0.75rem;
  flex: 1;
  min-height: 0;
}
.values-table {
  min-height: 0;
}
.values-table :deep(.p-datatable-table),
.values-table :deep(.p-datatable-table-container table) {
  table-layout: fixed;
  width: 100%;
}
.detail-pane {
  overflow-y: auto;
}
@media (max-width: 1200px) {
  .values-detail-split {
    grid-template-columns: 1fr;
  }
}
.values-table :deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
.value-key,
.value-label {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty {
  padding: 0.5rem 0.25rem;
  font-size: 12px;
}
.field {
  margin-bottom: 0.75rem;
}
.field label {
  display: block;
  margin-bottom: 0.25rem;
  font-size: 11px;
}
.key-taken {
  display: block;
  margin-top: 0.25rem;
  font-size: 11px;
  color: var(--p-red-500, #e5484d);
}
</style>
