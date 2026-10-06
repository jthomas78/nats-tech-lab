<script setup>
// The standalone entry. The rail picks the playground or the overview; both
// are the same route components the embedded entry hands to the shell.
import { computed, ref } from 'vue'

import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

import OverviewRoute from './plugin/OverviewRoute.vue'
import PlaygroundRoute from './plugin/PlaygroundRoute.vue'

const view = ref('playground')
const sections = [
  {
    eyebrow: 'Multi-cluster',
    items: [
      { key: 'playground', label: 'Playground' },
      { key: 'overview', label: 'Overview' },
    ],
  },
]
const title = computed(() => (view.value === 'overview' ? 'Overview' : 'Playground'))
</script>

<template>
  <AppShell>
    <template #brand>
      <span class="dot">3</span>
      <span>Multi-cluster</span>
    </template>

    <template #breadcrumb>
      <span>Demo 03</span>
      <span class="sep">/</span>
      <b>{{ title }}</b>
    </template>

    <template #sidebar>
      <NavList
        v-model="view"
        :sections="sections"
        aria-label="Multi-cluster"
      />
    </template>

    <PlaygroundRoute
      v-if="view === 'playground'"
      route-id="playground"
    />
    <OverviewRoute
      v-else
      route-id="overview"
    />
  </AppShell>
</template>
