<script setup>
/* The shell frame. It renders the topbar, the nav built from contributions,
   the route-scoped controls, the footer bar, and whatever the router resolved
   — and it knows nothing about any feature or plugin identity (BR-AS09). */
import AppShell from '@ui-shell/AppShell.vue'
import { computed, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { summarizeAttention } from './shell/registry/statusRollup.js'
import { breadcrumbTrail } from './shell/routing/breadcrumb.js'
import { createNavigationPending } from './shell/routing/navigationPending.js'
import { SHELL } from './shell/shellKey.js'
import RegistrySignalBanner from './shell/ui/RegistrySignalBanner.vue'
import ShellFooter from './shell/ui/ShellFooter.vue'
import ShellNav from './shell/ui/ShellNav.vue'
import PluginSlot from './shell/ui/PluginSlot.vue'
import SkeletonRows from './shell/ui/SkeletonRows.vue'
import { isWithdrawnRoute } from './shell/routing/withdrawnRoutes.js'
import PluginWithdrawnView from './views/PluginWithdrawnView.vue'

const shell = inject(SHELL)
const route = useRoute()
const router = useRouter()

const inventory = computed(() => shell.inventory)
const trail = computed(() =>
  breadcrumbTrail(route.meta, (id) => shell.statuses.get(id)?.name ?? null),
)
/* Route-scoped: a control declared for '/example' is in the topbar under that
   route and its children, and nowhere else (BR-AS07). */
const controls = computed(() => shell.contributions.shellControlsFor(route.path))

/* The chrome's one aggregate signal. A plugin that failed is visible from
   every screen without opening the inventory — status only, never a cause
   string, because a cause can quote a remote URL (BR-AS04). */
const attention = computed(() => summarizeAttention(inventory.value))

/* A deep link into a remote that has never been loaded spends a network fetch
   before the view exists; this is what fills that gap (task 1b-6). */
const { pending: navigating, target: navigatingTo } = createNavigationPending(router)

/* The occupant of a route whose plugin was withdrawn under them (BR-AS57).
   Swapped here rather than in the route record: the record resolved its
   component before the withdrawal, and re-resolving it would mean a live
   navigation on a URL nobody asked to leave. */
const withdrawnHere = computed(() => isWithdrawnRoute(shell.contributions, route))
</script>

<template>
  <AppShell>
    <template #brand>
      <router-link
        to="/"
        class="brand"
      >
        <i class="pi pi-sitemap" />
        <span>NATS Tech Lab</span>
      </router-link>
    </template>
    <template #breadcrumb>
      <span class="crumb">
        <span class="lab-muted">{{ trail.owner }}</span>
        <template v-if="trail.leaf">
          <span class="sep">/</span>
          <b>{{ trail.leaf }}</b>
        </template>
      </span>
    </template>
    <template #topbar-right>
      <router-link
        v-if="attention.count"
        class="attention"
        :class="attention.tone"
        to="/plugins"
      >
        <span
          class="dot"
          :class="attention.tone"
        />
        {{ attention.label }}
      </router-link>
      <PluginSlot
        v-for="control in controls"
        :key="control.qualifiedId"
        :contribution="control"
        placeholder="quiet"
      />
    </template>

    <!-- One rail, one definition. The shell's own screens, then every
         plugin's navigation merged into shell-placed bands (BR-AS89). -->
    <template #sidebar>
      <ShellNav />
    </template>

    <template #footer>
      <ShellFooter />
    </template>

    <!-- Above the content, not in the topbar: it is about the whole catalog,
         and it must not compete with a plugin's own route-scoped controls. -->
    <RegistrySignalBanner />

    <SkeletonRows
      v-if="navigating"
      :label="navigatingTo?.contributionId
        ? `Route contribution — ${navigatingTo.contributionId}`
        : 'Loading feature'"
    />
    <PluginWithdrawnView v-else-if="withdrawnHere" />
    <router-view v-else />
  </AppShell>
</template>

<style scoped>
.brand {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--p-text-color);
  text-decoration: none;
  font-size: 1rem;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.brand i {
  color: var(--lab-accent);
}
.crumb {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  white-space: nowrap;
}
.crumb b {
  color: var(--p-text-color);
  font-weight: 600;
}
.crumb .sep {
  color: var(--p-text-disabled-color);
}
.dot.err {
  background: var(--err);
}
.dot.warn {
  background: var(--warn);
}
.attention {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 10px;
  border-radius: 100px;
  border: 1px solid var(--lab-panel-border);
  font-size: 11px;
  font-weight: 600;
  text-decoration: none;
}
.attention.err {
  color: var(--err);
}
.attention.warn {
  color: var(--warn);
}
.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
</style>
