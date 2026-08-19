<script setup>
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import { computed, ref, watch } from 'vue'

import { categoryLabel, DOMAIN_CATEGORIES } from '../categories'
import { useDictionaryStore } from '../stores/dictionary'
import { typeIcon } from '../typeIcons'
import CacheStatusWidget from './CacheStatusWidget.vue'
import CategoryTypeList from './CategoryTypeList.vue'
import ItemGrid from './ItemGrid.vue'
import LocalizationView from './LocalizationView.vue'
import VersioningPanel from './VersioningPanel.vue'

const store = useDictionaryStore()

// The four top-level tabs replace what used to be separate sidebar
// destinations (TypeNavigator's reference-data list, its Domain group, and
// its Tools group) — see Phase 36.1. Starts on 'items' to match
// store.connect()'s own default landing spot (selectType on the first type).
const activeTab = ref('items')

// Reference-data types (category 'standards') — the flat switcher that used
// to live in TypeNavigator's sidebar, now scoped into this tab's own content.
const referenceTypes = computed(() =>
  store.types
    .filter((t) => (t.category || 'standards') === 'standards')
    .sort((a, b) => a.typeKey.localeCompare(b.typeKey)),
)

function selectReferenceType(typeKey) {
  if (typeKey !== store.selectedType) store.selectType(typeKey)
}

// Domain categories actually populated with types, in categories.js's fixed
// order — dynamic rather than a hardcoded Enums/Strings pair so a future
// `config`-category type (BR-D09) shows up as a third subtab automatically.
const domainCategories = computed(() =>
  DOMAIN_CATEGORIES.filter((cat) => store.types.some((t) => (t.category || 'standards') === cat)),
)

// store.selectedType is shared between this tab and the Domain tab
// (selectCategoryType sets it too, for CategoryTypeList's own master-detail).
// Switching tabs must re-sync it, or the tab you land on shows whatever type
// was last touched in the *other* tab instead of one of its own.
watch(activeTab, (tab) => {
  if (tab === 'domain-category' && !domainCategories.value.includes(store.selectedCategory)) {
    const fallback = domainCategories.value[0]
    if (fallback) store.showCategoryView(fallback)
  } else if (tab === 'items' && !referenceTypes.value.some((t) => t.typeKey === store.selectedType)) {
    const fallback = referenceTypes.value[0]
    if (fallback) store.selectType(fallback.typeKey)
  }
})
</script>

<template>
  <div class="reference-data-panel fill-height">
    <div class="page-head">
      <p class="eyebrow-static">
        Operations
      </p>
      <h1>Reference Data</h1>
      <p>
        Dictionary types, domain values, localization coverage, and corpus versioning for this business unit.
      </p>
    </div>

    <Tabs
      v-model:value="activeTab"
      class="panel-tabs rd-tabs"
    >
      <TabList>
        <Tab value="items">
          Reference Data
        </Tab>
        <Tab value="domain-category">
          Domain
        </Tab>
        <Tab value="localization">
          Localization
        </Tab>
        <Tab value="versioning">
          Versioning
        </Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="items">
          <div class="rd-card">
            <div class="reference-data-row">
              <nav
                class="type-switcher"
                aria-label="Reference data types"
              >
                <div class="nav-group">
                  <div class="eyebrow">
                    Reference Data
                  </div>
                  <div
                    v-for="t in referenceTypes"
                    :key="t.typeKey"
                    class="nav-item"
                    :class="{ active: t.typeKey === store.selectedType }"
                    @click="selectReferenceType(t.typeKey)"
                  >
                    <i :class="['pi', typeIcon(t.typeKey, t.category)]" />
                    <span class="label-fade">{{ t.name }}</span>
                    <span class="nav-badge">{{ store.typeCounts[t.typeKey] ?? 0 }}</span>
                  </div>
                  <p
                    v-if="referenceTypes.length === 0"
                    class="lab-muted label-fade"
                  >
                    No reference-data types registered yet.
                  </p>
                </div>
              </nav>
              <KeepAlive>
                <ItemGrid v-if="activeTab === 'items'" />
              </KeepAlive>
            </div>
            <CacheStatusWidget />
          </div>
        </TabPanel>

        <TabPanel value="domain-category">
          <div class="rd-domain-body">
            <Tabs
              :value="store.selectedCategory"
              @update:value="store.showCategoryView"
            >
              <TabList>
                <Tab
                  v-for="cat in domainCategories"
                  :key="cat"
                  :value="cat"
                >
                  {{ categoryLabel(cat) }}
                </Tab>
              </TabList>
            </Tabs>
            <p
              v-if="domainCategories.length === 0"
              class="lab-muted"
            >
              No domain data registered yet.
            </p>
            <KeepAlive>
              <CategoryTypeList v-if="activeTab === 'domain-category' && domainCategories.length > 0" />
            </KeepAlive>
          </div>
        </TabPanel>

        <TabPanel value="localization">
          <KeepAlive>
            <LocalizationView v-if="activeTab === 'localization'" />
          </KeepAlive>
        </TabPanel>

        <TabPanel value="versioning">
          <KeepAlive>
            <VersioningPanel v-if="activeTab === 'versioning'" />
          </KeepAlive>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>

<style scoped>
.reference-data-panel {
  gap: 1rem;
}
/* Matches shared/unifi-theme/app-shell-reference.html's `.page-head` /
   `.eyebrow-static` exactly (LAYOUT.md's documented "Main content" shape) —
   first real usage of that convention in this repo. */
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.page-head h1 {
  margin: 0;
  font-size: 20px;
  line-height: 27px;
  font-weight: 700;
  letter-spacing: -0.01em;
  text-wrap: balance;
}
.page-head .eyebrow-static {
  margin: 0 0 4px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
}
.page-head p:not(.eyebrow-static) {
  margin: 4px 0 0;
  color: var(--p-text-muted-color);
  font-size: 13px;
  max-width: 60ch;
}

/* Same flex-fill contract RpcPanel.vue uses for its own panel-tabs — see the
   "panel top tabs" rule in shared/unifi-theme/LAYOUT.md. */
.rd-tabs.p-tabs {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.rd-tabs :deep(.p-tablist) {
  flex: none;
}
.rd-tabs :deep(.p-tabpanels) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.rd-tabs :deep(.p-tabpanel) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.rd-card {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  flex: 1;
  min-height: 0;
}
.reference-data-row {
  display: flex;
  gap: 0.75rem;
  flex: 1;
  min-height: 0;
}
/* ItemGrid/CategoryTypeList carry their own `fill-height` class, which
   normally does its work via the global `.main-inner > .fill-height` rule —
   only true while they're App.vue's direct content. Nested inside this
   panel's own tabs instead, that selector no longer matches, so the same
   flex-fill contract is recreated locally for their new parent. */
.reference-data-row > .fill-height {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.type-switcher {
  flex: 0 0 13rem;
  overflow-y: auto;
}

.rd-domain-body {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  flex: 1;
  min-height: 0;
}
.rd-domain-body > .fill-height {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
</style>
