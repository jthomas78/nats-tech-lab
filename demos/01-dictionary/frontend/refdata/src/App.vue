<script setup>
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { onMounted, onUnmounted, ref, watch } from 'vue'

import IconReferenceData from './components/icons/IconReferenceData.vue'
import IconShippers from './components/icons/IconShippers.vue'
import IconTransporters from './components/icons/IconTransporters.vue'
import ReferenceDataPanel from './components/ReferenceDataPanel.vue'
import TradingPartnersPanel from './components/TradingPartnersPanel.vue'
import { useRefdataAdminConnection } from './nats/useRefdataAdminConnection.js'
import { useDictionaryStore } from './stores/dictionary'
import { useTenantStore } from './stores/tenant'
import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

const store = useDictionaryStore()
const tenantStore = useTenantStore()
const connection = useRefdataAdminConnection()

// Presentation-only nav-shell state (which top-level destination is showing),
// same "mode flag, no router" model as admin's own App.vue — not store state,
// since nothing outside this shell needs to read it.
const topNav = ref('reference-data')
const navSections = [
  {
    group: 'Operations',
    sections: [
      { items: [{ key: 'reference-data', label: 'Reference Data', icon: IconReferenceData }] },
      {
        // Migrated from frontend/admin's Platform group (Phase 36.2) — same
        // eyebrow grouping, relocated rather than redesigned.
        eyebrow: 'Trading Partners',
        items: [
          { key: 'shippers', label: 'Shippers', icon: IconShippers },
          { key: 'transporters', label: 'Transporters', icon: IconTransporters },
        ],
      },
    ],
  },
]

const isTradingPartnersView = (v) => v === 'shippers' || v === 'transporters'

// Trading Partners' tenant store is refreshed lazily, the first time the
// user opens either Shippers or Transporters — nothing else in this app has
// a tenant concept, so there is no reason to fetch the account list or open
// the second NATS connection (useTenantConnection.js) up front.
watch(topNav, (view) => {
  if (isTradingPartnersView(view) && !tenantStore.tenant) tenantStore.refresh()
})

// Phase 32: the NATS connection's own lifecycle (mount once, never
// reconnect on a context switch) is owned here, mirroring frontend/admin's
// App.vue — store.connect()/disconnect() is the data-refresh cycle, called
// again on every context switch, and must not tear down the transport each
// time. connect() is awaited so store.connect() below (which subscribes and
// makes its first api.* calls) doesn't race the WebSocket handshake.
onMounted(async () => {
  await connection.connect().catch(() => {})
  store.connect()
})
onUnmounted(() => {
  store.disconnect()
  connection.disconnect()
})
</script>

<template>
  <Toast position="bottom-right" />
  <AppShell>
    <template #brand>
      <span class="dot">O</span>
      <span>Tech Lab Operator</span>
    </template>
    <template #breadcrumb>
      <span
        v-if="isTradingPartnersView(topNav)"
        class="lab-muted"
      >Operations / <strong>Trading Partners</strong></span>
      <span
        v-else
        class="lab-muted"
      >Operations / <strong>Reference Data</strong></span>
    </template>
    <template #topbar-right>
      <template v-if="isTradingPartnersView(topNav)">
        <!-- Trading Partners' own tenant + fleet-context selectors — distinct
             from Reference Data's platform-wide Context Select below, since
             trading-partner-service is tenant-account-scoped (Phase 36.2). -->
        <label
          class="lab-muted"
          for="tp-tenant"
        >Tenant</label>
        <Select
          id="tp-tenant"
          :model-value="tenantStore.tenant"
          :options="tenantStore.available"
          :disabled="tenantStore.switching"
          size="small"
          @update:model-value="tenantStore.setTenant($event)"
        />
        <Tag
          v-if="tenantStore.switching"
          severity="warning"
          value="switching…"
        />
        <label
          class="lab-muted"
          for="tp-context"
        >Fleet</label>
        <Select
          id="tp-context"
          :model-value="tenantStore.context"
          :options="tenantStore.availableContexts"
          option-label="name"
          option-value="context"
          size="small"
          :disabled="!tenantStore.tenant"
          @update:model-value="tenantStore.setContext($event)"
        />
      </template>
      <template v-else>
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
          option-label="name"
          option-value="context"
          size="small"
          @update:model-value="val => { store.context = val; store.connect() }"
        />
      </template>
    </template>
    <template #sidebar>
      <NavList
        v-model="topNav"
        :sections="navSections"
        aria-label="Operations"
      />
    </template>

    <ReferenceDataPanel v-if="topNav === 'reference-data'" />
    <TradingPartnersPanel
      v-else-if="isTradingPartnersView(topNav)"
      :key="topNav"
      :partner-type="topNav === 'shippers' ? 'SHIPPER' : 'TRANSPORTER'"
    />
  </AppShell>
</template>
