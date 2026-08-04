<script setup>
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { onMounted, onUnmounted } from 'vue'

import CacheStatusWidget from './components/CacheStatusWidget.vue'
import CategoryTypeList from './components/CategoryTypeList.vue'
import ItemGrid from './components/ItemGrid.vue'
import LocalizationView from './components/LocalizationView.vue'
import TypeNavigator from './components/TypeNavigator.vue'
import VersioningPanel from './components/VersioningPanel.vue'
import { useDictionaryStore } from './stores/dictionary'
import AppShell from '@ui-shell/AppShell.vue'

const store = useDictionaryStore()

onMounted(() => store.connect())
onUnmounted(() => store.disconnect())
</script>

<template>
  <Toast position="bottom-right" />
  <AppShell>
    <template #brand>
      <span class="dot">D</span>
      <span>Dictionary</span>
    </template>
    <template #breadcrumb>
      <span class="lab-muted">reference data · localization · typed references · Q5 cache</span>
    </template>
    <template #topbar-right>
      <Tag
        :severity="store.connected ? 'success' : 'danger'"
        :value="store.connected ? 'watching' : 'disconnected'"
      />
      <label
        class="lab-muted"
        for="context"
      >Context</label>
      <Select
        id="context"
        :model-value="store.context"
        :options="store.availableContexts"
        size="small"
        @update:model-value="val => { store.context = val; store.connect() }"
      />
    </template>
    <template #sidebar>
      <TypeNavigator />
    </template>

    <LocalizationView v-if="store.activeView === 'localization'" />
    <VersioningPanel v-else-if="store.activeView === 'versioning'" />
    <CategoryTypeList v-else-if="store.activeView === 'domain-category'" />
    <template v-else>
      <ItemGrid />
      <div class="lower-panels">
        <CacheStatusWidget />
      </div>
    </template>
  </AppShell>
</template>

<style scoped>
.lower-panels {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.625rem;
}
</style>
