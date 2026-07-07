<script setup>
import Button from 'primevue/button'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { onMounted, onUnmounted, ref } from 'vue'

import EntryForm from './components/EntryForm.vue'
import EventLog from './components/EventLog.vue'
import JetStreamPanel from './components/JetStreamPanel.vue'
import ShapePanel from './components/ShapePanel.vue'
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
        <h1>Dictionary POC</h1>
        <span class="lab-muted">JetStream → projections → KV, two shapes side by side</span>
      </div>
      <div class="topbar-right">
        <Tag :severity="store.connected ? 'success' : 'danger'" :value="store.connected ? 'watching' : 'disconnected'" />
        <label class="lab-muted" for="context">Context</label>
        <Select
          id="context"
          :model-value="store.context"
          :options="CONTEXTS"
          size="small"
          @update:model-value="store.setContext($event)"
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

    <!-- 1. Command input — dispatch side -->
    <EntryForm />

    <!-- 2. JetStream — raw NATS messages -->
    <JetStreamPanel />

    <!-- 3. KV projections — Shape A (KV-only) | Shape B (KV cache + Postgres) -->
    <div class="panels">
      <ShapePanel shape="A" title="Shape A — KV as read model">
        Events are projected straight into <code>dict-a-{{ store.context }}</code>. Reads go to KV
        only; the KV revision is the version. No Postgres involved.
      </ShapePanel>
      <ShapePanel shape="B" title="Shape B — KV cache in front of Postgres">
        Events update the canonical Postgres projection, then refresh
        <code>dict-b-{{ store.context }}</code>. Evict a key, then read it to watch the miss →
        Postgres → backfill path.
      </ShapePanel>
    </div>

    <!-- 4. KV watch stream — filterable -->
    <EventLog />
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
  font-size: 15px; /* UniFi --desktop-font-size-large, line-height: 24px */
  line-height: 24px;
  letter-spacing: 0.02em;
}
.topbar-right {
  display: flex;
  align-items: center;
  gap: 0.625rem;
}
.panels {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.625rem;
}
@media (max-width: 900px) {
  .panels {
    grid-template-columns: 1fr;
  }
}
</style>
