<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { deleteItem, deprecateItem, registerItem } from '../api'
import { useDictionaryStore } from '../stores/dictionary'
import ItemEditorDialog from './ItemEditorDialog.vue'

const store = useDictionaryStore()
const toast = useToast()

const localeOptions = computed(() => ['', ...store.locales])

watch(() => store.showDeprecated, () => store.refreshItems())
watch(() => store.selectedLocale, () => store.refreshItems())

// ── Add item ───────────────────────────────────────────────────────────────────

const addVisible = ref(false)
const newCode = ref('')
const newName = ref('')

function openAdd() {
  newCode.value = ''
  newName.value = ''
  addVisible.value = true
}

async function submitAdd() {
  if (!newCode.value.trim()) return
  try {
    await registerItem({
      typeKey: store.selectedType,
      code: newCode.value.trim(),
      context: store.context,
      attrs: newName.value.trim() ? { name: newName.value.trim() } : {},
    })
    addVisible.value = false
    await store.refreshItems()
    await store.refreshTypeCounts()
    toast.add({ severity: 'success', summary: 'Item registered', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not register item', detail: err.message, life: 4000 })
  }
}

// ── Deprecate / delete ─────────────────────────────────────────────────────────

async function onDeprecate(item) {
  try {
    await deprecateItem(store.selectedType, store.context, item.code)
    await store.refreshItems()
    toast.add({ severity: 'success', summary: `${item.code} deprecated`, life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not deprecate', detail: err.message, life: 4000 })
  }
}

async function onDelete(item) {
  try {
    await deleteItem(store.selectedType, store.context, item.code)
    await store.refreshItems()
    await store.refreshTypeCounts()
    toast.add({ severity: 'success', summary: `${item.code} deleted`, life: 2500 })
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

// ── Item editor (localizations + references) ──────────────────────────────────

const editorVisible = ref(false)
const editorCode = ref('')

function openEditor(item) {
  editorCode.value = codeFor(item)
  editorVisible.value = true
}

function labelFor(item) {
  return item.label || item.item?.attrs?.name || item.attrs?.name || item.code || item.item?.code
}
function codeFor(item) {
  return item.code || item.item?.code
}
function statusFor(item) {
  return item.status || item.item?.status
}
</script>

<template>
  <div class="lab-panel item-grid">
    <div class="grid-head">
      <h3>{{ store.selectedType || 'Select a type' }}</h3>
      <div class="grid-controls">
        <label
          class="lab-muted"
          for="locale"
        >Locale</label>
        <Select
          id="locale"
          v-model="store.selectedLocale"
          :options="localeOptions"
          size="small"
          placeholder="(code)"
          style="width: 8rem"
        />
        <label
          class="lab-muted"
          for="show-deprecated"
        >Show deprecated</label>
        <ToggleSwitch
          id="show-deprecated"
          v-model="store.showDeprecated"
        />
        <Button
          icon="pi pi-plus"
          label="Add"
          size="small"
          :disabled="!store.selectedType"
          @click="openAdd"
        />
      </div>
    </div>

    <DataTable
      :value="store.items"
      size="small"
      data-key="code"
    >
      <template #empty>
        No items in this type yet.
      </template>
      <Column header="Code">
        <template #body="{ data }">
          {{ codeFor(data) }}
        </template>
      </Column>
      <Column header="Label">
        <template #body="{ data }">
          {{ labelFor(data) }}
        </template>
      </Column>
      <Column header="Status">
        <template #body="{ data }">
          <Tag
            :severity="statusFor(data) === 'active' ? 'success' : 'warning'"
            :value="statusFor(data)"
          />
        </template>
      </Column>
      <Column header="Actions">
        <template #body="{ data }">
          <div class="row-actions">
            <Button
              icon="pi pi-pencil"
              text
              size="small"
              aria-label="Edit"
              @click="openEditor(data)"
            />
            <Button
              icon="pi pi-eye-slash"
              text
              size="small"
              aria-label="Deprecate"
              :disabled="statusFor(data) === 'deprecated'"
              @click="onDeprecate(data)"
            />
            <Button
              icon="pi pi-trash"
              text
              severity="danger"
              size="small"
              aria-label="Delete"
              @click="onDelete(data)"
            />
          </div>
        </template>
      </Column>
    </DataTable>

    <Dialog
      v-model:visible="addVisible"
      header="Register item"
      modal
      style="width: 24rem"
    >
      <div class="field">
        <label
          class="lab-muted"
          for="new-code"
        >Code</label>
        <InputText
          id="new-code"
          v-model="newCode"
          style="width: 100%"
          placeholder="e.g. XYZ"
        />
      </div>
      <div class="field">
        <label
          class="lab-muted"
          for="new-name"
        >Name (attrs.name)</label>
        <InputText
          id="new-name"
          v-model="newName"
          style="width: 100%"
          placeholder="optional display name"
        />
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          size="small"
          @click="addVisible = false"
        />
        <Button
          label="Register"
          size="small"
          :disabled="!newCode.trim()"
          @click="submitAdd"
        />
      </template>
    </Dialog>

    <ItemEditorDialog
      v-model:visible="editorVisible"
      :code="editorCode"
    />
  </div>
</template>

<style scoped>
.grid-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.5rem;
}
.grid-head h3 {
  text-transform: capitalize;
}
.grid-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.row-actions {
  display: flex;
  gap: 2px;
}
.field {
  margin-bottom: 0.75rem;
}
.field label {
  display: block;
  margin-bottom: 0.25rem;
  font-size: 11px;
}
</style>
