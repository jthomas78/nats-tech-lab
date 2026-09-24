<script setup>
// The standalone entry. The rail selects a lesson; each lesson owns its local
// controls. Everything below the chrome is LessonPanels.vue, which the
// embedded entry (plugin/OdometerRoute.vue) renders as well — one build serves
// both, and the panels never learn which one is showing them.
import Tag from 'primevue/tag'

import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

import LessonPanels from './components/LessonPanels.vue'
import { useDemoState } from './view/useDemoState.js'

const state = useDemoState()
</script>

<template>
  <AppShell>
    <template #brand>
      <span class="dot">4</span>
      <span>JetStream</span>
    </template>

    <template #breadcrumb>
      <span>Demo 04</span>
      <span class="sep">/</span>
      <span>{{ state.crumb.lesson }}</span>
      <span class="sep">/</span>
      <b>{{ state.crumb.title }}</b>
    </template>

    <template #topbar-right>
      <Tag
        data-testid="connection-status"
        :severity="state.connection.severity"
        :value="state.connection.label"
      />
    </template>

    <template #sidebar>
      <NavList
        v-model="state.view"
        :sections="state.sections"
        aria-label="JetStream"
      />
    </template>

    <LessonPanels :state="state" />
  </AppShell>
</template>
