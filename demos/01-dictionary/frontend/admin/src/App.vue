<script setup>
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import AccountsPanel from './components/AccountsPanel.vue'
import ConnectionsPanel from './components/ConnectionsPanel.vue'
import JetStreamPanel from './components/JetStreamPanel.vue'
import KvInspector from './components/KvInspector.vue'
import OverviewPanel from './components/OverviewPanel.vue'
import PostgresTablesPanel from './components/PostgresTablesPanel.vue'
import RpcPanel from './components/RpcPanel.vue'
import ServicesPanel from './components/ServicesPanel.vue'
import ShapeCPanel from './components/ShapeCPanel.vue'
import ShapePanel from './components/ShapePanel.vue'
import TelemetryStrip from './components/TelemetryStrip.vue'
import IconAccounts from './components/icons/IconAccounts.vue'
import IconConnections from './components/icons/IconConnections.vue'
import IconKv from './components/icons/IconKv.vue'
import IconOverview from './components/icons/IconOverview.vue'
import IconRpc from './components/icons/IconRpc.vue'
import IconServices from './components/icons/IconServices.vue'
import IconShapes from './components/icons/IconShapes.vue'
import IconStreams from './components/icons/IconStreams.vue'
import IconTables from './components/icons/IconTables.vue'
import { useDictionaryStore } from './stores/dictionary'
import { useTenantStore } from './stores/tenant'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'
import { useL10nCopy } from '@refdata/useL10nCopy.js'
import { i18n } from './i18n.js'
import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

const store = useDictionaryStore()
const tenantStore = useTenantStore()
const {
  selectedLocale,
  localeOptions,
  connect: connectRefdata,
  disconnect: disconnectRefdata,
} = useRefdataLabels()
const { usingFallback, partialFallback, connect: connectL10nCopy, disconnect: disconnectL10nCopy } = useL10nCopy()
const { t } = useI18n()

// ── View selection (grouped activity bar) — one view rendered at a time, no
// router. The NATS group holds the four NATS surfaces (request/reply,
// streams, KV, the shape read models); Postgres holds the canonical tables.
// Extend by pushing onto a section's items.
const activeView = ref('overview')
const sections = [
  { items: [{ key: 'overview', label: 'Overview', icon: IconOverview }] },
  {
    eyebrow: 'NATS',
    items: [
      { key: 'connections', label: 'Connections', icon: IconConnections },
      { key: 'services', label: 'Services', icon: IconServices },
      { key: 'rpc', label: 'Request/Reply', icon: IconRpc },
      { key: 'streams', label: 'Streams', icon: IconStreams },
      { key: 'kv', label: 'KV Buckets', icon: IconKv },
      { key: 'shapes', label: 'CQRS Shapes', icon: IconShapes, badge: 3 },
      { key: 'accounts', label: 'Accounts', icon: IconAccounts },
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
  rpc: 'rpc.* + api.* request/reply traffic · rpc.* replays last 10 min, api.* live only',
  connections: 'nats connections · all accounts',
  services: 'nats micro services · $SRV.* discovery',
  tables: 'canonical Postgres tables by schema',
  accounts: 'dynamic tenant provisioning · decentralized JWTs',
}
const subtitle = computed(() => SUBTITLES[activeView.value] ?? '')

onMounted(() => {
  store.connect()
  store.loadContexts()
  connectRefdata()
  connectL10nCopy(i18n)
  tenantStore.refresh()
})
onUnmounted(() => {
  store.disconnect()
  disconnectRefdata()
  disconnectL10nCopy()
})
</script>

<template>
  <Toast position="bottom-right" />
  <AppShell>
    <template #brand>
      <span class="dot">A</span>
      <span>Tech Lab Admin</span>
    </template>
    <template #breadcrumb>
      <span class="lab-muted">{{ subtitle }}</span>
    </template>
    <template #topbar-right>
      <Tag :severity="store.connected ? 'success' : 'danger'" :value="store.connected ? 'watching' : 'disconnected'" />
      <!-- Phase 13b tenant selector — a different NATS account, not a fleet
           filter (CLAUDE.md/plan: must stay visually + functionally distinct
           from the Fleet selector below). "warning" severity is deliberate:
           this is the one control in the topbar that reconnects the backend. -->
      <label class="lab-muted" for="tenant">{{ t('nav.tenant') }}</label>
      <Select
        id="tenant"
        :model-value="tenantStore.tenant"
        :options="tenantStore.available"
        :disabled="tenantStore.switching"
        size="small"
        @update:model-value="tenantStore.setTenant($event)"
      />
      <Tag v-if="tenantStore.switching" severity="warning" :value="t('tenant.switching')" />
      <label class="lab-muted" for="context">Fleet</label>
      <Select
        id="context"
        :model-value="store.context"
        :options="store.availableContexts"
        size="small"
        @update:model-value="store.setContext($event)"
      />
      <label class="lab-muted" for="locale">{{ t('nav.language') }}</label>
      <Select
        id="locale"
        v-model="selectedLocale"
        :options="localeOptions"
        option-label="label"
        option-value="value"
        size="small"
        placeholder="—"
      >
        <template #value="{ value, placeholder }">
          {{ value || placeholder }}
        </template>
      </Select>
      <Tag
        v-if="usingFallback || partialFallback"
        severity="warning"
        :value="usingFallback ? 'UI text: bundled (refdata unreachable)' : 'UI text: partially bundled'"
      />
    </template>
    <template #sidebar>
      <NavList v-model="activeView" :sections="sections" aria-label="Inspector views" />
    </template>

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
          Ship events are projected into <code>dict-a</code> under the
          <code>{{ store.context }}.ship.*</code> key prefix. Reads go to KV
          only; the KV revision is the version. No Postgres involved.
        </ShapePanel>
        <ShapePanel shape="B" title="Shape B — KV cache in front of Postgres">
          Events update the canonical Postgres projection, then refresh
          <code>dict-b</code> under the <code>{{ store.context }}.ship.*</code>
          key prefix. Evict a ship, then read it to watch the miss → Postgres →
          backfill path.
        </ShapePanel>
      </div>
      <ShapeCPanel />
    </section>

    <!-- Request/Reply — obs.rpc.* + obs.api.* request/reply traffic (Phase 12.10; api.* added Phase 16) -->
    <section v-else-if="activeView === 'rpc'" class="group group--flush" data-testid="rpc-view">
      <div class="lab-panel streams-panel">
        <RpcPanel />
      </div>
    </section>

    <!-- Connections — every active NATS connection, server-wide (Phase 17c).
         Manages its own internal scroll regions, so the section is flush. -->
    <section v-else-if="activeView === 'connections'" class="group group--flush" data-testid="connections-view">
      <div class="lab-panel streams-panel">
        <ConnectionsPanel />
      </div>
    </section>

    <!-- Services — micro-registered services via $SRV.* discovery (Phase 17c) -->
    <section v-else-if="activeView === 'services'" class="group group--flush" data-testid="services-view">
      <div class="lab-panel streams-panel">
        <ServicesPanel />
      </div>
    </section>

    <!-- Postgres — canonical tables by schema -->
    <section v-else-if="activeView === 'tables'" class="group" data-testid="tables-view">
      <PostgresTablesPanel />
    </section>

    <!-- Platform — dynamic tenant provisioning (Phase 14c) -->
    <section v-else class="group" data-testid="accounts-view">
      <AccountsPanel />
    </section>

    <template #footer>
      <TelemetryStrip />
    </template>
  </AppShell>
</template>

<style scoped>
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
  .panels {
    grid-template-columns: 1fr;
  }
}
</style>
