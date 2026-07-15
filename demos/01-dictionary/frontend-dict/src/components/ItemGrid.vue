<script setup>
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { registerItem } from '../api'
import { categoryLabel } from '../categories'
import { useDictionaryStore } from '../stores/dictionary'
import ItemDetailPanel from './ItemDetailPanel.vue'

const store = useDictionaryStore()
const toast = useToast()

const localeOptions = computed(() => ['', ...store.locales])

watch(() => store.showDeprecated, () => store.refreshItems())
watch(() => store.selectedLocale, () => store.refreshItems())

const selectedTypeMeta = computed(() => store.types.find((t) => t.typeKey === store.selectedType))
const category = computed(() => selectedTypeMeta.value?.category || 'standards')

// Density varies by category (Phase 11.9): reference data is the only
// category where sets get big, so it gets a filter; small domain sets
// (enums / UI strings / configuration) just show in full.
const FILTER_THRESHOLD = 15
const filterable = computed(
  () => category.value === 'standards' || store.items.length > FILTER_THRESHOLD,
)
const filter = ref('')
watch(() => store.selectedType, () => { filter.value = '' })

const filteredItems = computed(() => {
  const q = filter.value.trim().toLowerCase()
  if (!q) return store.items
  return store.items.filter(
    (i) => codeFor(i)?.toLowerCase().includes(q) || labelFor(i)?.toLowerCase().includes(q),
  )
})

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
    store.selectItem(newCode.value.trim())
    toast.add({ severity: 'success', summary: 'Item registered', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not register item', detail: err.message, life: 4000 })
  }
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
    <!-- Header split: type identity left, view controls right. -->
    <div class="grid-head">
      <div class="type-identity">
        <h3>{{ store.selectedType || 'Select a type' }}</h3>
        <Tag
          v-if="store.selectedType"
          class="category-chip"
          severity="secondary"
          :value="categoryLabel(category)"
        />
        <span
          v-if="store.selectedType"
          class="lab-muted item-count"
        >{{ store.items.length }} items</span>
      </div>
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

    <!-- Master-detail: item list | item detail (one spatial model for every category). -->
    <div class="master-detail">
      <div class="item-list-pane">
        <InputText
          v-if="filterable"
          v-model="filter"
          class="item-filter"
          placeholder="Filter by code or label…"
          size="small"
        />
        <ul class="item-list">
          <li
            v-for="item in filteredItems"
            :key="codeFor(item)"
            :class="{ active: codeFor(item) === store.selectedCode }"
            @click="store.selectItem(codeFor(item))"
          >
            <span class="item-code">{{ codeFor(item) }}</span>
            <span class="item-label">{{ labelFor(item) }}</span>
            <Tag
              v-if="statusFor(item) !== 'active'"
              severity="warning"
              :value="statusFor(item)"
            />
          </li>
        </ul>
        <p
          v-if="store.items.length === 0"
          class="lab-muted"
        >
          No items in this type yet.
        </p>
        <p
          v-else-if="filteredItems.length === 0"
          class="lab-muted"
        >
          No items match the filter.
        </p>
      </div>

      <ItemDetailPanel class="detail-pane" />
    </div>

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
  </div>
</template>

<style scoped>
.grid-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
  margin-bottom: 0.5rem;
}
.type-identity {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}
.type-identity h3 {
  text-transform: capitalize;
  margin: 0;
}
.category-chip {
  font-size: 10px;
}
.item-count {
  font-size: 11px;
  white-space: nowrap;
}
.grid-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.master-detail {
  display: grid;
  grid-template-columns: minmax(14rem, 2fr) 3fr;
  gap: 0.75rem;
  align-items: start;
}
@media (max-width: 900px) {
  .master-detail {
    grid-template-columns: 1fr;
  }
}
.item-list-pane {
  min-width: 0;
  border-right: 1px solid var(--lab-disabled-bg);
  padding-right: 0.75rem;
}
@media (max-width: 900px) {
  .item-list-pane {
    border-right: none;
    padding-right: 0;
  }
}
.item-filter {
  width: 100%;
  margin-bottom: 0.4rem;
}
.item-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 32rem;
  overflow-y: auto;
}
.item-list li {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.35rem 0.5rem;
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
}
.item-list li:hover {
  background: var(--lab-disabled-bg);
}
.item-list li.active {
  background: var(--lab-accent);
  color: #fff;
}
.item-list li.active .item-label {
  color: inherit;
}
.item-code {
  font-family: monospace;
  flex: 0 0 auto;
}
.item-label {
  color: var(--lab-muted, #888);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1 1 auto;
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
