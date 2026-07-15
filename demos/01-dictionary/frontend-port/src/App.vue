<script setup>
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { useToast } from 'primevue/usetoast'
import { useI18n } from 'vue-i18n'
import { onMounted, onUnmounted, ref } from 'vue'

import FleetPanel from './components/FleetPanel.vue'
import ShipsAtPortPanel from './components/ShipsAtPortPanel.vue'
import TerminalPanel from './components/TerminalPanel.vue'
import { CONTEXTS, usePortStore } from './stores/port'
import { isDark, toggleTheme } from '@unifi-theme/preset.js'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'
import { useUiCopy } from '@refdata/useUiCopy.js'
import { i18n } from './i18n.js'

const store = usePortStore()
const { selectedLocale, locales, connect: connectRefdata, disconnect: disconnectRefdata } = useRefdataLabels()
const { usingFallback, partialFallback, connect: connectUiCopy, disconnect: disconnectUiCopy } = useUiCopy()
const { t } = useI18n()
const dark = ref(isDark())
const toast = useToast()

function handleToggleTheme() {
  toggleTheme()
  dark.value = isDark()
}

// ── Add a shipping port (popup) ───────────────────────────────────────────────

const newPortVisible = ref(false)
const newPortName = ref('')

function openNewPort() {
  newPortName.value = ''
  newPortVisible.value = true
}

async function submitNewPort() {
  if (!newPortName.value.trim()) return
  try {
    await store.addShippingPort(newPortName.value)
    newPortVisible.value = false
  } catch (err) {
    toast.add({ severity: 'error', summary: t('toast.portAddFailed'), detail: err.message, life: 4000 })
  }
}

onMounted(() => {
  store.connect()
  connectRefdata()
  connectUiCopy(i18n)
})
onUnmounted(() => {
  store.disconnect()
  disconnectRefdata()
  disconnectUiCopy()
})
</script>

<template>
  <Toast position="bottom-right" />
  <div class="layout">
    <header class="topbar">
      <div>
        <h1>{{ t('app.title') }}</h1>
        <span class="lab-muted">{{ t('app.subtitle') }}</span>
      </div>
      <div class="topbar-right">
        <Tag :severity="store.connected ? 'success' : 'danger'" :value="store.connected ? t('connection.watching') : t('connection.disconnected')" />
        <label class="lab-muted" for="context">{{ t('context.fleet') }}</label>
        <Select
          id="context"
          :model-value="store.context"
          :options="CONTEXTS"
          size="small"
          @update:model-value="store.setContext($event)"
        />
        <label class="lab-muted" for="locale">{{ t('nav.language') }}</label>
        <Select
          id="locale"
          v-model="selectedLocale"
          :options="locales"
          size="small"
          :placeholder="t('select.none')"
        />
        <Tag
          v-if="usingFallback || partialFallback"
          severity="warning"
          :value="usingFallback ? t('fallback.unreachable') : t('fallback.partial')"
        />
        <Button
          :icon="dark ? 'pi pi-sun' : 'pi pi-moon'"
          :aria-label="dark ? t('a11y.lightMode') : t('a11y.darkMode')"
          text
          rounded
          size="small"
          @click="handleToggleTheme"
        />
      </div>
    </header>

    <!-- Fleet-wide ship list (all / docked / in-transit); port-independent,
         fleet-scoped only -->
    <section class="group">
      <FleetPanel />
    </section>

    <!-- Port Management — everything scoped to the selected port. The port
         selector lives here (not the topbar) because it only scopes this
         group; the in-transit view above is port-independent. -->
    <section class="group">
      <div class="group-head">
        <h2>{{ t('port.management') }}</h2>
        <div class="group-head-controls">
          <label class="lab-muted" for="port">{{ t('port.label') }}</label>
          <Select
            id="port"
            :model-value="store.port"
            :options="store.knownPorts"
            :placeholder="t('port.select')"
            size="small"
            @update:model-value="store.setPort($event)"
          />
          <Button
            icon="pi pi-plus"
            :aria-label="t('port.add')"
            text
            rounded
            size="small"
            @click="openNewPort"
          />
        </div>
      </div>
      <!-- Terminal yard + docked ships; each panel owns the operations that
           act on it (register/load in the yard, arrive/depart/unload for ships) -->
      <div class="panels">
        <TerminalPanel />
        <ShipsAtPortPanel />
      </div>
    </section>

    <Dialog v-model:visible="newPortVisible" :header="t('port.addDialog')" modal style="width:22rem">
      <InputText
        v-model="newPortName"
        :placeholder="t('port.namePlaceholder')"
        size="small"
        style="width:100%"
        @keyup.enter="submitNewPort"
      />
      <p class="lab-muted dialog-note">
        {{ t('port.addHelp') }}
      </p>
      <template #footer>
        <Button :label="t('action.cancel')" text size="small" @click="newPortVisible = false" />
        <Button :label="t('action.add')" size="small" :disabled="!newPortName.trim()" @click="submitNewPort" />
      </template>
    </Dialog>
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
.dialog-note {
  margin: 0.5rem 0 0;
  font-size: 0.8rem;
}
.group {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}
.group-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.group-head h2 {
  margin: 0;
  font-size: 13px;
  line-height: 20px;
  letter-spacing: 0.02em;
}
.group-head-controls {
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
