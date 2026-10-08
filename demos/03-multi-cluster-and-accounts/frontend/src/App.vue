<script setup>
// The standalone entry. One rail item, Meta-Leader, and the same page
// component the embedded entry hands to the shell. There is no router here,
// so the page keeps its tab as local state.
import { ref } from 'vue'

import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

import MetaLeaderRoute from './plugin/MetaLeaderRoute.vue'

const view = ref('meta-leader')
const sections = [
  {
    eyebrow: 'Clusters',
    items: [{ key: 'meta-leader', label: 'Meta-Leader' }],
  },
]
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
      <b>Meta-Leader</b>
    </template>

    <template #sidebar>
      <NavList
        v-model="view"
        :sections="sections"
        aria-label="Clusters"
      />
    </template>

    <MetaLeaderRoute route-id="meta-leader" />
  </AppShell>
</template>
