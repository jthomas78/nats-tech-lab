<script setup>
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Toast from 'primevue/toast'
import { useToast } from 'primevue/usetoast'
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import FleetPanel from './components/FleetPanel.vue'
import IconFleet from './components/icons/IconFleet.vue'
import IconPort from './components/icons/IconPort.vue'
import IconPricing from './components/icons/IconPricing.vue'
import PricingPanel from './components/PricingPanel.vue'
import ShipsAtPortPanel from './components/ShipsAtPortPanel.vue'
import TerminalPanel from './components/TerminalPanel.vue'
import { usePortStore } from './stores/port'
import { usePricingStore } from './stores/pricing'
import { useTenantStore } from './stores/tenant'
import { useRefdataLabels } from '@refdata/useRefdataLabels.js'
import { useL10nCopy } from '@refdata/useL10nCopy.js'
import { useNatsConnection } from './nats/useNatsConnection'
import { i18n } from './i18n.js'
import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

const store = usePortStore()
const pricingStore = usePricingStore()
const tenantStore = useTenantStore()
const {
  selectedLocale,
  localeOptions,
  connect: connectRefdata,
  disconnect: disconnectRefdata,
} = useRefdataLabels()
const { usingFallback, partialFallback, switching, connect: connectL10nCopy, disconnect: disconnectL10nCopy } = useL10nCopy()
const { connected: natsConnected, lastError, disconnect: disconnectNats } = useNatsConnection()

// "Watching" requires BOTH the NATS connection to be live and the port store
// to have subscribed — they are independent flags and only the former reacts
// to the connection dropping. usePortStore().connected is cleared solely by
// its own disconnect(), which nothing calls when NATS evicts us (a suspended
// tenant, ARCHITECTURE-ACCOUNTS.md § 2t-a), so reading it alone left the
// topbar showing a green "watching" beside the red "connection error" — the
// app claiming to be live and broken at the same time.
const watching = computed(() => natsConnected.value && store.connected)
const { t } = useI18n()
const toast = useToast()

// ── View selection (activity bar) — mutually exclusive, one view rendered
// at a time. A plain ref is enough for two views; add more by pushing onto
// `views` rather than introducing routing. Wrapped in a single ungrouped
// section for NavList (shared/ui-shell) — no eyebrow grouping needed for
// just two flat views.
const activeView = ref('fleet')
const sections = computed(() => [
  {
    items: [
      { key: 'fleet', label: t('nav.fleetManagement'), icon: IconFleet },
      { key: 'port', label: t('nav.portManagement'), icon: IconPort },
      { key: 'pricing', label: t('nav.pricing'), icon: IconPricing },
    ],
  },
])
const subtitles = computed(() => ({
  fleet: t('app.subtitleFleet'),
  port: t('app.subtitlePort'),
  pricing: t('app.subtitlePricing'),
}))
const subtitle = computed(() => subtitles.value[activeView.value])

// Fleet view's port column links directly into Port Management scoped to
// that port, instead of leaving the user to re-select it from the dropdown.
function handleNavigatePort(port) {
  store.setPort(port)
  activeView.value = 'port'
}

// The topbar's business-unit Select scopes every tab, including Pricing —
// mirror the switch into pricingStore the same way store.setContext already
// scopes Fleet/Port.
function handleContextChange(context) {
  store.setContext(context)
  pricingStore.setContext(context)
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

onMounted(async () => {
  connectRefdata()
  connectL10nCopy(i18n)
  try {
    // Authenticates the browser's single NATS WebSocket connection (Phase
    // 15c/15d) before the port store's api.*/notify.* bootstrap can run —
    // unlike the pre-Phase-15 SSE stores, store.connect() now depends on a
    // live NATS connection rather than being independently openable.
    await tenantStore.init()
    await store.connect()
    pricingStore.setContext(store.context)
  } catch (err) {
    console.error('NATS connect failed', err)
    toast.add({ severity: 'error', summary: t('toast.connectFailed'), detail: err.message, life: 6000 })
  }
})
onUnmounted(() => {
  store.disconnect()
  pricingStore.disconnect()
  disconnectNats()
  disconnectRefdata()
  disconnectL10nCopy()
})
</script>

<template>
  <Toast position="bottom-right" />
  <AppShell>
    <template #brand>
      <span class="dot">S</span>
      <span>{{ t('app.title') }}</span>
    </template>
    <template #breadcrumb>
      <span class="lab-muted">{{ subtitle }}</span>
    </template>
    <template #topbar-right>
      <Tag data-testid="connection-status" :severity="watching ? 'success' : 'danger'" :value="watching ? t('connection.watching') : t('connection.disconnected')" />
      <!-- Phase 13b tenant selector — a different NATS account, not a fleet
           filter; must stay visually + functionally distinct from Fleet below. -->
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
      <!-- Surfaces useNatsConnection's lastError (e.g. a tenant suspended
           mid-session gets force-evicted, then refused a fresh JWT by
           connectInfo's 403) — previously set silently with nothing
           rendering it, so panels just stopped updating with no
           explanation. Clears itself: connect() resets lastError to '' the
           moment a connection succeeds again. -->
      <Tag v-if="lastError" data-testid="connection-error" severity="danger" :value="t('connection.error')" :title="lastError" />
      <label class="lab-muted" for="context">{{ t('context.business-unit') }}</label>
      <!-- Disabled + `<default>` when the tenant has no real BU registered
           (availableContexts empty — BR-AC28's auto-created default is
           excluded from this list by its isDefault flag, Phase 22b) —
           nothing to choose between, and the default shouldn't render as if
           it were a normal selectable option. options carry {context, name}
           so the label shown is the human-readable name, not the slug. -->
      <Select
        id="context"
        :model-value="store.context"
        :options="store.availableContexts"
        option-label="name"
        option-value="context"
        :disabled="!store.availableContexts.length"
        size="small"
        @update:model-value="handleContextChange"
      >
        <template #value="{ value }">
          {{ store.availableContexts.length ? (store.availableContexts.find((c) => c.context === value)?.name ?? value) : t('context.default') }}
        </template>
      </Select>
      <label class="lab-muted" for="locale">{{ t('nav.language') }}</label>
      <Select
        id="locale"
        v-model="selectedLocale"
        :options="localeOptions"
        option-label="label"
        option-value="value"
        :loading="switching"
        size="small"
        :placeholder="t('select.none')"
      >
        <template #value="{ value, placeholder }">
          {{ value || placeholder }}
        </template>
      </Select>
      <Tag
        v-if="usingFallback || partialFallback"
        severity="warning"
        :value="usingFallback ? t('fallback.unreachable') : t('fallback.partial')"
      />
    </template>
    <template #sidebar>
      <NavList v-model="activeView" :sections="sections" :aria-label="t('nav.viewSelector')" />
    </template>

    <!-- Fleet-wide ship list (all / docked / in-transit); port-independent,
         fleet-scoped only -->
    <section v-if="activeView === 'fleet'" class="group" data-testid="fleet-view">
      <FleetPanel @navigate-port="handleNavigatePort" />
    </section>

    <!-- Port Management — everything scoped to the selected port. The port
         selector lives here (not the topbar) because it only scopes this
         group; the fleet view above is port-independent. -->
    <section v-else-if="activeView === 'port'" class="group" data-testid="port-view">
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

    <!-- Pricing — read-only landing panel over the same topbar-selected
         business-unit context (Phase 25g); no port scoping, unlike Port
         Management above. -->
    <section v-else class="group" data-testid="pricing-view">
      <PricingPanel />
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
  </AppShell>
</template>

<style scoped>
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
