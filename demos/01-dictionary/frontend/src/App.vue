<script setup>
import Button from 'primevue/button'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import JetStreamPanel from './components/JetStreamPanel.vue'
import KvInspector from './components/KvInspector.vue'
import NavSidebar from './components/NavSidebar.vue'
import OverviewPanel from './components/OverviewPanel.vue'
import PostgresTablesPanel from './components/PostgresTablesPanel.vue'
import ShapeCPanel from './components/ShapeCPanel.vue'
import ShapePanel from './components/ShapePanel.vue'
import TelemetryStrip from './components/TelemetryStrip.vue'
import IconKv from './components/icons/IconKv.vue'
import IconOverview from './components/icons/IconOverview.vue'
import IconShapes from './components/icons/IconShapes.vue'
import IconStreams from './components/icons/IconStreams.vue'
import IconTables from './components/icons/IconTables.vue'
import { CONTEXTS, useDictionaryStore } from './stores/dictionary'
import { isDark, toggleTheme } from '@unifi-theme/preset.js'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'
import { useUiCopy } from '@refdata/useUiCopy.js'
import { i18n } from './i18n.js'

const store = useDictionaryStore()
const { selectedLocale, locales, connect: connectRefdata, disconnect: disconnectRefdata } = useRefdataLabels()
const { usingFallback, partialFallback, connect: connectUiCopy, disconnect: disconnectUiCopy } = useUiCopy()
const { t } = useI18n()
const dark = ref(isDark())

// ── View selection (grouped activity bar) — one view rendered at a time, no
// router. The JetStream group holds the three NATS surfaces (streams, KV, the
// shape read models); Postgres holds the canonical tables. Extend by pushing
// onto a section's items.
const activeView = ref('overview')
const sections = [
  { items: [{ key: 'overview', label: 'Overview', icon: IconOverview }] },
  {
    eyebrow: 'JetStream',
    items: [
      { key: 'streams', label: 'Streams', icon: IconStreams },
      { key: 'kv', label: 'KV Buckets', icon: IconKv },
      { key: 'shapes', label: 'CQRS Shapes', icon: IconShapes, badge: 3 },
    ],
  },
  {
    eyebrow: 'Postgres',
    items: [{ key: 'tables', label: 'Tables', icon: IconTables }],
  },
]

const SUBTITLES = {
  overview: 'pipeline health · dispatch a test command',
  streams: 'raw NATS messages · live tail and full replay',
  kv: 'every registered bucket · contents and live changes',
  shapes: 'three CQRS read-model shapes, side by side',
  tables: 'canonical Postgres tables by schema',
}
const subtitle = computed(() => SUBTITLES[activeView.value] ?? '')

function handleToggleTheme() {
  toggleTheme()
  dark.value = isDark()
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
      <div class="brand">
        <h1>CQRS Inspector</h1>
        <span class="lab-muted brand-sub">{{ subtitle }}</span>
      </div>
      <div class="topbar-right">
        <Tag :severity="store.connected ? 'success' : 'danger'" :value="store.connected ? 'watching' : 'disconnected'" />
        <label class="lab-muted" for="context">Fleet</label>
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
          placeholder="—"
        />
        <Tag
          v-if="usingFallback || partialFallback"
          severity="warning"
          :value="usingFallback ? 'UI text: bundled (refdata unreachable)' : 'UI text: partially bundled'"
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

    <div class="shell">
      <NavSidebar v-model="activeView" :sections="sections" aria-label="Inspector views" />

      <div class="shell-content">
        <!-- Overview — pipeline health + dispatch -->
        <section v-if="activeView === 'overview'" class="group" data-testid="overview-view">
          <OverviewPanel />
        </section>

        <!-- Streams — raw NATS messages (live tail + full replay). Fills the
             remaining vertical space rather than capping at a page-size table. -->
        <section v-else-if="activeView === 'streams'" class="group group--flush" data-testid="streams-view">
          <div class="lab-panel streams-panel">
            <JetStreamPanel />
          </div>
        </section>

        <!-- KV Buckets — every registered bucket, its contents + live update feed.
             Manages its own internal scroll regions, so the section is flush. -->
        <section v-else-if="activeView === 'kv'" class="group group--flush" data-testid="kv-view">
          <div class="lab-panel streams-panel">
            <KvInspector />
          </div>
        </section>

        <!-- CQRS Shapes — A (KV read model) | B (KV cache + Postgres) | C (replay) -->
        <section v-else-if="activeView === 'shapes'" class="group" data-testid="shapes-view">
          <div class="panels">
            <ShapePanel shape="A" title="Shape A — KV as read model">
              Ship events are projected straight into <code>dict-a-{{ store.context }}</code>. Reads go to
              KV only; the KV revision is the version. No Postgres involved.
            </ShapePanel>
            <ShapePanel shape="B" title="Shape B — KV cache in front of Postgres">
              Events update the canonical Postgres projection, then refresh
              <code>dict-b-{{ store.context }}</code>. Evict a ship, then read it to watch the miss →
              Postgres → backfill path.
            </ShapePanel>
          </div>
          <ShapeCPanel />
        </section>

        <!-- Postgres — canonical tables by schema -->
        <section v-else class="group" data-testid="tables-view">
          <PostgresTablesPanel />
        </section>
      </div>
    </div>

    <TelemetryStrip />
  </div>
</template>

<style scoped>
.layout {
  max-width: 1440px;
  margin: 0 auto;
  height: 100vh;
  padding: 0.75rem 0.75rem 0;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.625rem;
  flex-wrap: wrap;
}
.brand {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  min-width: 0;
}
.topbar h1 {
  margin: 0;
  font-size: 15px;
  line-height: 24px;
  letter-spacing: 0.02em;
}
.brand-sub {
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.topbar-right {
  display: flex;
  align-items: center;
  gap: 0.625rem;
}
.shell {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: stretch;
  gap: 0.625rem;
}
.shell-content {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}
.group {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
  overflow-y: auto;
}
/* Streams manages its own internal scroll region (the message table), so the
   section itself must not also scroll. */
.group--flush {
  overflow: hidden;
}
.streams-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.panels {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.625rem;
}
@media (max-width: 900px) {
  .shell {
    flex-direction: column;
  }
  .panels {
    grid-template-columns: 1fr;
  }
}
</style>

<style>
/* Unscoped: establishes the percentage-height chain html → body → #app so
   .layout's height:100vh has a basis, letting the Streams view's table fill
   the viewport instead of capping at a fixed row count. */
html,
body,
#app {
  height: 100%;
}
</style>
