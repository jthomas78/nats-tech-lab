<script setup>
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import AccountsView from './components/AccountsView.vue'
import ConnectionsPanel from './components/ConnectionsPanel.vue'
import JetStreamPanel from './components/JetStreamPanel.vue'
import KvInspector from './components/KvInspector.vue'
import LogPanel from './components/LogPanel.vue'
import OverviewPanel from './components/OverviewPanel.vue'
import PostgresTablesPanel from './components/PostgresTablesPanel.vue'
import MessagesPanel from './components/MessagesPanel.vue'
import RpcPanel from './components/RpcPanel.vue'
import ServicesPanel from './components/ServicesPanel.vue'
import SettingsPanel from './components/SettingsPanel.vue'
import TelemetryStrip from './components/TelemetryStrip.vue'
import IconAccounts from './components/icons/IconAccounts.vue'
import IconConnections from './components/icons/IconConnections.vue'
import IconKv from './components/icons/IconKv.vue'
import IconLog from './components/icons/IconLog.vue'
import IconOverview from './components/icons/IconOverview.vue'
import IconActivity from './components/icons/IconActivity.vue'
import IconRpc from './components/icons/IconRpc.vue'
import IconServices from './components/icons/IconServices.vue'
import IconSettings from './components/icons/IconSettings.vue'
import IconStreams from './components/icons/IconStreams.vue'
import IconTables from './components/icons/IconTables.vue'
import { useDictionaryStore } from './stores/dictionary'
import { useTenantStore } from './stores/tenant'
import { useUiStore } from './stores/ui'
import { setRefdataTransport, useRefdataLabels } from '@refdata/useRefdataLabels.js'
import { useL10nCopy } from '@refdata/useL10nCopy.js'
import { i18n } from './i18n.js'
import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'
import { usePlatformConnection } from './nats/usePlatformConnection.js'

const store = useDictionaryStore()
const tenantStore = useTenantStore()
const uiStore = useUiStore()
const {
  selectedLocale,
  localeOptions,
  connect: connectRefdata,
  disconnect: disconnectRefdata,
} = useRefdataLabels()
const { usingFallback, partialFallback, connect: connectL10nCopy, disconnect: disconnectL10nCopy } = useL10nCopy()
const { t } = useI18n()

// The Admin UI has one browser NATS connection, authenticated into PLATFORM.
// It watches the centrally projected observability buckets and lends the
// shared refdata composables a narrowly allowlisted read transport for UI
// labels/copy. shared/ can't import @nats-io/nats-core itself, so this app
// injects the connection's request/subscribe pair.
const platformConnection = usePlatformConnection()
setRefdataTransport({
  request: platformConnection.request,
  subscribe: platformConnection.subscribe,
})

// ── View selection (grouped activity bar) — one view rendered at a time, no
// router. Two top-level groups, PLATFORM before SYSTEM, split by what the
// view is *of* rather than which backend serves it: PLATFORM configures and
// inspects the business layer (who is on the platform and how it is set up),
// SYSTEM is low-level infrastructure diagnostics (NATS internals, canonical
// Postgres tables). Overview stays ungrouped above both — it reads across
// the whole stack, so it belongs to neither.
//
// Extend by pushing onto a section's items; add a nav level with a nested
// `eyebrow` section rather than a third level, which NavList.vue doesn't
// render (see shared/unifi-theme/LAYOUT.md).
const activeView = ref('overview')
const sections = [
  { items: [{ key: 'overview', label: 'Overview', icon: IconOverview }] },
  {
    group: 'Platform',
    sections: [
      { items: [{ key: 'settings', label: 'Settings', icon: IconSettings }] },
    ],
  },
  {
    group: 'System',
    sections: [
      // Accounts moved here from PLATFORM (Phase 45) once its Overview tab
      // absorbed the standalone Account Activity panel below — Accounts is
      // now the one home for both the business roster and NATS-account
      // health, so a PLATFORM item displaying SYSTEM content stopped making
      // sense. First entry, above the NATS eyebrow group it partly reports on.
      { items: [{ key: 'accounts', label: 'Accounts', icon: IconAccounts }] },
      {
        eyebrow: 'NATS',
        items: [
          { key: 'services', label: 'Services', icon: IconServices },
          { key: 'connections', label: 'Connections', icon: IconConnections },
          { key: 'pubsub', label: 'Pub/Sub', icon: IconActivity },
          { key: 'rpc', label: 'Request/Reply', icon: IconRpc },
          { key: 'streams', label: 'Streams', icon: IconStreams },
          { key: 'kv', label: 'KV Buckets', icon: IconKv },
          { key: 'log', label: 'Logs', icon: IconLog },
        ],
      },
      {
        eyebrow: 'Postgres',
        items: [{ key: 'tables', label: 'Tables', icon: IconTables }],
      },
    ],
  },
]

const SUBTITLES = {
  overview: 'pipeline health · dispatch a test command',
  streams: 'raw NATS messages · live tail and full replay',
  kv: 'every registered bucket · contents and live changes',
  rpc: 'rpc.* + api.* request/reply traffic · rpc.* replays last 10 min, api.* live only',
  pubsub: 'evt.* + notify.* publish traffic across every tenant · best-effort, last 15 min',
  connections: 'nats connections · all accounts',
  services: 'nats micro services · $SRV.* discovery',
  log: 'nats server log · level + text filter, no rotation',
  tables: 'canonical Postgres tables by schema',
  settings: 'platform-global system configuration',
}
// accounts has three tabs (AccountsView.vue) with distinct enough subject
// matter — fleet health, provisioning, and the export/import graph — that
// one subtitle for all three would either describe none or run long, so
// it's the one entry SUBTITLES can't answer directly.
const ACCOUNTS_SUBTITLES = {
  overview: 'per-account traffic + slow-consumer health · /accstatz',
  provisioning: 'dynamic tenant provisioning · decentralized JWTs',
  sharing: 'declared export/import edges between accounts · read from resolver JWTs',
}
const subtitle = computed(() =>
  activeView.value === 'accounts' ? ACCOUNTS_SUBTITLES[uiStore.accountsTab] : SUBTITLES[activeView.value] ?? '',
)

onMounted(async () => {
  // The refdata reads below need the PLATFORM request transport, while the
  // legacy overview snapshot still needs the backend's active account label.
  // Neither opens a tenant-account browser connection.
  await Promise.all([
    platformConnection.connect().catch(() => {}),
    tenantStore.refresh(),
  ])
  await store.loadContexts()
  store.connect()
  connectRefdata()
  connectL10nCopy(i18n)
})
onUnmounted(() => {
  store.disconnect()
  disconnectRefdata()
  disconnectL10nCopy()
  platformConnection.disconnect()
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
      <Tag
        :severity="platformConnection.connected.value ? 'success' : 'danger'"
        :value="platformConnection.connected.value ? 'watching' : 'disconnected'"
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

    <!-- Request/Reply — obs.rpc.* + obs.api.* request/reply traffic (Phase 12.10; api.* added Phase 16).
         No lab-panel wrapper here (unlike the other group--flush views below) — RpcPanel's own
         Traces/Messages Tabs sit flush on the page, same as AccountsView's tabs, with the card
         treatment applied only to each tab's content (see RpcPanel.vue's .rpc-card). -->
    <section v-else-if="activeView === 'rpc'" class="group group--flush" data-testid="rpc-view">
      <RpcPanel />
    </section>

    <!-- Messages — cross-tenant evt.*/notify.* publish traffic (Phase 43c, BR-048).
         Its own entry rather than a fourth RpcPanel tab: different bucket, different
         stream, and a tenant column none of the Request/Reply tabs can populate.
         MessagesPanel is its own .lab-panel, so the section stays flush. -->
    <section v-else-if="activeView === 'pubsub'" class="group group--flush" data-testid="pubsub-view">
      <MessagesPanel />
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

    <!-- Log — tails NATS's own log_file, level + text filter, no rotation -->
    <section v-else-if="activeView === 'log'" class="group" data-testid="log-view">
      <LogPanel />
    </section>

    <!-- Postgres — canonical tables by schema -->
    <section v-else-if="activeView === 'tables'" class="group" data-testid="tables-view">
      <PostgresTablesPanel />
    </section>

    <!-- System — platform-global configuration (BR-AC20) -->
    <section v-else-if="activeView === 'settings'" class="group" data-testid="settings-view">
      <SettingsPanel />
    </section>

    <!-- Accounts — fleet activity overview (Phase 45, absorbing the old
         standalone Account Activity panel), provisioning (Phase 14c), and
         the declared export/import sharing graph, as tabs of one view (see
         AccountsView.vue) -->
    <section v-else class="group" data-testid="accounts-view">
      <AccountsView />
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
</style>
