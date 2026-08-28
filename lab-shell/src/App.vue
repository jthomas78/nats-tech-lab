<script setup>
/* The shell frame. It renders the topbar, the nav built from contributions,
   the route-scoped controls, the footer bar, and whatever the router resolved
   — and it knows nothing about any feature. The one import that names a plugin
   is main.js's built-in adapter; nothing here does (BR-AS09). */
import AppShell from '@ui-shell/AppShell.vue'
import { computed, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { createNavigationPending } from './shell/routing/navigationPending.js'
import { SHELL } from './shell/shellKey.js'
import ShellFooter from './shell/ui/ShellFooter.vue'
import PluginSlot from './shell/ui/PluginSlot.vue'
import SkeletonRows from './shell/ui/SkeletonRows.vue'

const shell = inject(SHELL)
const route = useRoute()
const router = useRouter()

const navigation = computed(() => shell.contributions.navigation)
const current = computed(() => route.meta?.title ?? '')
/* Route-scoped: a control declared for '/example' is in the topbar under that
   route and its children, and nowhere else (BR-AS07). */
const controls = computed(() => shell.contributions.shellControlsFor(route.path))

/* A deep link into a remote that has never been loaded spends a network fetch
   before the view exists; this is what fills that gap (task 1b-6). */
const navigating = createNavigationPending(router)

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
      <span class="lab-muted">{{ current }}</span>
    </template>
    <template #topbar-right>
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
        </router-link>
      </nav>
    </template>

    <template #footer>
      <ShellFooter />
    </template>

    <SkeletonRows
      v-if="navigating"
      label="Loading feature"
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
.nav-item {
  text-decoration: none;
}
</style>
