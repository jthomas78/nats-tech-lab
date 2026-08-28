<script setup>
/* The shell frame. It renders the topbar, the nav built from contributions,
   and whatever the router resolved — and it knows nothing about any feature.
   The one import that names a plugin is main.js's built-in adapter; nothing
   here does (BR-AS09). */
import AppShell from '@ui-shell/AppShell.vue'
import { computed, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { SHELL } from './shell/shellKey.js'

const shell = inject(SHELL)
const route = useRoute()
const router = useRouter()

const navigation = computed(() => shell.contributions.navigation)
const current = computed(() => route.meta?.title ?? '')

function isActive(entry) {
  const target = router.resolve({ name: entry.routeQualifiedId })
  return route.path === target.path || route.path.startsWith(`${target.path}/`)
}
</script>

<template>
  <AppShell>
    <template #brand>
      <router-link to="/" class="brand">
        <i class="pi pi-sitemap" />
        <span>NATS Tech Lab</span>
      </router-link>
    </template>
    <template #breadcrumb>
      <span class="lab-muted">{{ current }}</span>
    </template>

    <template #sidebar>
      <nav class="nav-group">
        <router-link
          v-for="entry in navigation"
          :key="entry.qualifiedId"
          class="nav-item"
          :class="{ active: isActive(entry) }"
          :to="{ name: entry.routeQualifiedId }"
        >
          <i v-if="entry.icon" :class="entry.icon" />
          <span class="label-fade">{{ entry.label }}</span>
        </router-link>
      </nav>
    </template>

    <router-view />
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
