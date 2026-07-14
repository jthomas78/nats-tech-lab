<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import { useToast } from 'primevue/usetoast'
import { computed, ref, watch } from 'vue'

import { createReference, listItemLocalizations, listItemReferences, setLocalization } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const props = defineProps({
  visible: { type: Boolean, required: true },
  code: { type: String, default: '' },
})
const emit = defineEmits(['update:visible'])

const store = useDictionaryStore()
const toast = useToast()

const localizations = ref([])
const references = ref([])
const loading = ref(false)

async function load() {
  if (!props.code) return
  loading.value = true
  try {
    const [locRes, refRes] = await Promise.all([
      listItemLocalizations(store.context, store.selectedType, props.code),
      listItemReferences(store.context, store.selectedType, props.code),
    ])
    localizations.value = locRes?.localizations ?? []
    references.value = refRes?.references ?? []
  } finally {
    loading.value = false
  }
}

watch(() => [props.visible, props.code], ([visible]) => {
  if (visible) load()
})

// ── Localization form ────────────────────────────────────────────────────────

const locLocale = ref('')
const locLabel = ref('')
const locDescription = ref('')

async function submitLocalization() {
  if (!locLocale.value.trim() || !locLabel.value.trim()) return
  try {
    await setLocalization({
      typeKey: store.selectedType,
      code: props.code,
      context: store.context,
      locale: locLocale.value.trim(),
      label: locLabel.value.trim(),
      description: locDescription.value.trim(),
    })
    locLocale.value = ''
    locLabel.value = ''
    locDescription.value = ''
    await load()
    toast.add({ severity: 'success', summary: 'Localization saved', life: 2500 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not save localization', detail: err.message, life: 4000 })
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
      fromCode: props.code,
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

function close() {
  emit('update:visible', false)
}
</script>

<template>
  <Dialog
    :visible="visible"
    :header="`Edit ${code}`"
    modal
    style="width: 34rem"
    @update:visible="(v) => emit('update:visible', v)"
  >
    <Tabs value="localizations">
      <TabList>
        <Tab value="localizations">
          Localizations
        </Tab>
        <Tab value="references">
          References
        </Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="localizations">
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
            <Column
              field="label"
              header="Label"
            />
            <Column
              field="description"
              header="Description"
            />
            <Column
              field="source"
              header="Source"
            />
          </DataTable>
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
              size="small"
              aria-label="Add localization"
              :disabled="!locLocale.trim() || !locLabel.trim()"
              @click="submitLocalization"
            />
          </div>
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

    <template #footer>
      <Button
        label="Close"
        size="small"
        @click="close"
      />
    </template>
  </Dialog>
</template>

<style scoped>
.add-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  margin-top: 0.75rem;
  flex-wrap: wrap;
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
</style>
