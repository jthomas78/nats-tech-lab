<script setup>
import Button from 'primevue/button'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { onMounted, onUnmounted, ref } from 'vue'

import CacheStatusWidget from './components/CacheStatusWidget.vue'
import ItemGrid from './components/ItemGrid.vue'
import LocalizationView from './components/LocalizationView.vue'
import TypeNavigator from './components/TypeNavigator.vue'
import { CONTEXTS, useDictionaryStore } from './stores/dictionary'
import { isDark, toggleTheme } from '@unifi-theme/preset.js'

const store = useDictionaryStore()
const dark = ref(isDark())

function handleToggleTheme() {
  toggleTheme()
  dark.value = isDark()
}

onMounted(() => store.connect())
onUnmounted(() => store.disconnect())
</script>

<template>
  <Toast position="bottom-right" />
  <div class="layout">
    <header class="topbar">
      <div>
        <h1>Dictionary</h1>
        <span class="lab-muted">reference data · localization · typed references · Q5 cache</span>
      </div>
      <div class="topbar-right">
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
          :options="CONTEXTS"
          size="small"
          disabled
        />
        <Button
          :icon="dark ? 'pi pi-sun' : 'pi pi-moon'"
          :aria-label="dark ? 'Switch to light mode' : 'Switch to dark mode'"
          text
          rounded
          size="small"
          @click="handleToggleTheme"
        />
      </div>
    </header>

    <div class="main">
      <TypeNavigator />
      <div class="content">
        <LocalizationView v-if="store.activeView === 'localization'" />
        <template v-else>
          <ItemGrid />
          <div class="lower-panels">
            <CacheStatusWidget />
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.layout {
  max-width: 1280px;
  margin: 0 auto;
  padding: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.topbar h1 {
  margin: 0 0 2px;
  font-size: 15px;
  line-height: 24px;
  letter-spacing: 0.02em;
}
.topbar-right {
  display: flex;
  align-items: center;
  gap: 0.625rem;
}
.main {
  display: grid;
  grid-template-columns: 14rem 1fr;
  gap: 0.625rem;
  align-items: start;
}
.content {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}
.lower-panels {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.625rem;
}
@media (max-width: 900px) {
  .main {
    grid-template-columns: 1fr;
  }
}
</style>
