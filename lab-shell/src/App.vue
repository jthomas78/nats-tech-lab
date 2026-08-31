<script setup>
/* The shell frame. It renders the topbar, the nav built from contributions,
   the route-scoped controls, the footer bar, and whatever the router resolved
   — and it knows nothing about any feature or plugin identity (BR-AS09). */
import AppShell from '@ui-shell/AppShell.vue'
import { computed, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { attentionTone, summarizeAttention } from './shell/registry/statusRollup.js'
import { breadcrumbTrail } from './shell/routing/breadcrumb.js'
import { createNavigationPending } from './shell/routing/navigationPending.js'
import { SHELL } from './shell/shellKey.js'
import RegistrySignalBanner from './shell/ui/RegistrySignalBanner.vue'
import ShellFooter from './shell/ui/ShellFooter.vue'
import PluginSlot from './shell/ui/PluginSlot.vue'
import SkeletonRows from './shell/ui/SkeletonRows.vue'

const shell = inject(SHELL)
const route = useRoute()
const router = useRouter()

const navigation = computed(() => shell.contributions.navigation)
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
const statusOf = (pluginId) => shell.statuses.get(pluginId)?.status ?? null

/* A deep link into a remote that has never been loaded spends a network fetch
   before the view exists; this is what fills that gap (task 1b-6). */
const { pending: navigating, target: navigatingTo } = createNavigationPending(router)

function isActive(entry) {
  const target = router.resolve({ name: entry.routeQualifiedId })
  return route.path === target.path || route.path.startsWith(`${target.path}/`)
}
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

    <template #sidebar>
      <!-- The shell's own two screens. They carry no feature content: Home is
           a host for `shell/home-main/v1`, Plugins is the inventory. -->
      <nav class="nav-group">
        <div class="eyebrow">
          <span>Shell</span>
        </div>
        <router-link
          class="nav-item"
          :class="{ active: route.path === '/' }"
          to="/"
        >
          <i class="pi pi-home" />
          <span class="label-fade">Home</span>
        </router-link>
        <router-link
          class="nav-item"
          :class="{ active: route.path === '/plugins' }"
          to="/plugins"
        >
          <i class="pi pi-box" />
          <span class="label-fade">Plugins</span>
          <!-- The count is the inventory size, so the badge and the screen it
               leads to can never disagree. -->
          <span
            v-if="attention.count"
            class="nav-dot"
            :class="attention.tone"
          />
          <span
            v-else
            class="nav-badge"
          >{{ inventory.length }}</span>
        </router-link>
      </nav>
      <nav class="nav-group">
        <div class="eyebrow">
          <span>Features</span>
        </div>
        <router-link
          v-for="entry in navigation"
          :key="entry.qualifiedId"
          class="nav-item"
          :class="{ active: isActive(entry) }"
          :to="{ name: entry.routeQualifiedId }"
        >
          <i
            v-if="entry.icon"
            :class="entry.icon"
          />
          <span class="label-fade">{{ entry.label }}</span>
          <!-- The dot marks the entry whose own plugin is in trouble. A
               sibling's failure never dots this one — that isolation is the
               whole claim the shell makes (BR-AS04). -->
          <span
            v-if="attentionTone(statusOf(entry.pluginId))"
            class="nav-dot"
            :class="attentionTone(statusOf(entry.pluginId))"
            :title="statusOf(entry.pluginId)"
          />
        </router-link>
      </nav>
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
.nav-item {
  text-decoration: none;
}
.nav-badge {
  margin-left: auto;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  color: var(--p-text-disabled-color);
  background: var(--lab-disabled-bg);
  border-radius: 100px;
  padding: 1px 6px;
}
.nav-dot {
  margin-left: auto;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.nav-dot.err,
.dot.err {
  background: var(--err);
}
.nav-dot.warn,
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
