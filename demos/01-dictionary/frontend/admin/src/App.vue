<script setup>
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import AccountActivityPanel from './components/AccountActivityPanel.vue'
import AccountsView from './components/AccountsView.vue'
import ConnectionsPanel from './components/ConnectionsPanel.vue'
import JetStreamPanel from './components/JetStreamPanel.vue'
import KvInspector from './components/KvInspector.vue'
import LogPanel from './components/LogPanel.vue'
import OverviewPanel from './components/OverviewPanel.vue'
import PostgresTablesPanel from './components/PostgresTablesPanel.vue'
import RpcPanel from './components/RpcPanel.vue'
import ServicesPanel from './components/ServicesPanel.vue'
import SettingsPanel from './components/SettingsPanel.vue'
import ShapeCPanel from './components/ShapeCPanel.vue'
import ShapePanel from './components/ShapePanel.vue'
import TelemetryStrip from './components/TelemetryStrip.vue'
import TradingPartnersPanel from './components/TradingPartnersPanel.vue'
import IconAccounts from './components/icons/IconAccounts.vue'
import IconActivity from './components/icons/IconActivity.vue'
import IconConnections from './components/icons/IconConnections.vue'
import IconKv from './components/icons/IconKv.vue'
import IconLog from './components/icons/IconLog.vue'
import IconOverview from './components/icons/IconOverview.vue'
import IconRpc from './components/icons/IconRpc.vue'
import IconServices from './components/icons/IconServices.vue'
import IconSettings from './components/icons/IconSettings.vue'
import IconShapes from './components/icons/IconShapes.vue'
import IconShippers from './components/icons/IconShippers.vue'
import IconStreams from './components/icons/IconStreams.vue'
import IconTables from './components/icons/IconTables.vue'
import IconTransporters from './components/icons/IconTransporters.vue'
import { useDictionaryStore } from './stores/dictionary'
import { useTenantStore } from './stores/tenant'
import { useUiStore } from './stores/ui'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'
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

// Phase 23: the topbar connection indicator below is driven by this
// PLATFORM-account connection specifically, not the tenant one — PLATFORM
// has no tenant/BU lifecycle (auth/token.go's MintAdminToken doc comment),
// so "connected" stops being a side effect of which tenant/BU happens to be
// selected, which is what it was under the old /api/watch/{context}
// EventSource (empty context → connection error → "disconnected", even
// though NATS itself was fine).
const platformConnection = usePlatformConnection()

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
      // Accounts is a tenant (NATS account) roster — a platform-membership
      // question, so it sits here rather than under SYSTEM's NATS group where
      // it used to live purely because NATS accounts are its mechanism.
      { items: [{ key: 'accounts', label: 'Accounts', icon: IconAccounts }] },
      {
        // Phase 26 — own nav category (linebooker_registration_ui_placement.md):
        // organisation-owned master data that *consumes* refdata lookups
        // (VehicleType), not a vocabulary itself, so it belongs here, not in
        // frontend/refdata. Split per role rather than one combined list
        // because shipper- and transporter-specific fields diverge from here
        // on (fleet assets and GOODS_IN_TRANSIT are already transporter-only).
        eyebrow: 'Trading partners',
        items: [
          { key: 'shippers', label: 'Shippers', icon: IconShippers },
          { key: 'transporters', label: 'Transporters', icon: IconTransporters },
        ],
      },
      { items: [{ key: 'settings', label: 'Settings', icon: IconSettings }] },
    ],
  },
  {
    group: 'System',
    sections: [
      {
        eyebrow: 'NATS',
        items: [
          { key: 'connections', label: 'Connections', icon: IconConnections },
          { key: 'services', label: 'Services', icon: IconServices },
          { key: 'account-activity', label: 'Account Activity', icon: IconActivity },
          { key: 'log', label: 'Log', icon: IconLog },
          { key: 'rpc', label: 'Request/Reply', icon: IconRpc },
          { key: 'streams', label: 'Streams', icon: IconStreams },
          { key: 'kv', label: 'KV Buckets', icon: IconKv },
          { key: 'shapes', label: 'CQRS Shapes', icon: IconShapes, badge: 3 },
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
  shapes: 'three CQRS read-model shapes, side by side',
  rpc: 'rpc.* + api.* request/reply traffic · rpc.* replays last 10 min, api.* live only',
  connections: 'nats connections · all accounts',
  services: 'nats micro services · $SRV.* discovery',
  'account-activity': 'per-account traffic + slow-consumer health · /accstatz',
  log: 'nats server log · level + text filter, no rotation',
  tables: 'canonical Postgres tables by schema',
  settings: 'platform-global system configuration',
  shippers: 'shipper registration · KYC documents',
  transporters: 'transporter registration · KYC documents · fleet assets',
}
// accounts has two tabs (AccountsView.vue) with distinct enough subject
// matter — provisioning vs. the export/import graph — that one subtitle for
// both would either describe neither or run long, so it's the one entry
// SUBTITLES can't answer directly.
const ACCOUNTS_SUBTITLES = {
  provisioning: 'dynamic tenant provisioning · decentralized JWTs',
  topology: 'declared export/import edges between accounts · read from resolver JWTs',
}
const subtitle = computed(() =>
  activeView.value === 'accounts' ? ACCOUNTS_SUBTITLES[uiStore.accountsTab] : SUBTITLES[activeView.value] ?? '',
)

onMounted(async () => {
  // Platform connection first (no tenant dependency, drives the connection
  // indicator on its own) — fire-and-forget, its own connected/lastError
  // refs track outcome.
  platformConnection.connect().catch(() => {})
  // tenantStore.refresh() also establishes the tenant NATS connection
  // (Phase 23) and is awaited so loadContexts()/store.connect() below run
  // against a settled connection attempt, not mid-authentication.
  await tenantStore.refresh()
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

    <!-- Request/Reply — obs.rpc.* + obs.api.* request/reply traffic (Phase 12.10; api.* added Phase 16).
         No lab-panel wrapper here (unlike the other group--flush views below) — RpcPanel's own
         Traces/Messages Tabs sit flush on the page, same as AccountsView's tabs, with the card
         treatment applied only to each tab's content (see RpcPanel.vue's .rpc-card). -->
    <section v-else-if="activeView === 'rpc'" class="group group--flush" data-testid="rpc-view">
      <RpcPanel />
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

    <!-- Account Activity — per-account traffic + slow-consumer health from
         /accstatz (Phase 27). Manages its own internal scroll regions, so the
         section is flush, same as Connections/Services above. -->
    <section v-else-if="activeView === 'account-activity'" class="group group--flush" data-testid="account-activity-view">
      <div class="lab-panel streams-panel">
        <AccountActivityPanel />
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

    <!-- Trading Partners — Shipper/Transporter registration (Phase 26). One
         panel per role rather than one combined list, keyed so switching
         roles remounts it instead of leaving the previous role's rows and
         open dialogs behind. -->
    <section
      v-else-if="activeView === 'shippers' || activeView === 'transporters'"
      class="group"
      :data-testid="`${activeView}-view`"
    >
      <TradingPartnersPanel
        :key="activeView"
        :partner-type="activeView === 'shippers' ? 'SHIPPER' : 'TRANSPORTER'"
      />
    </section>

    <!-- Accounts — provisioning (Phase 14c) + the declared export/import
         topology graph, as tabs of one view (see AccountsView.vue) -->
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
