<script setup>
import { marked } from 'marked'
import Button from 'primevue/button'
import { computed } from 'vue'

import ExtensionRegion from '../ExtensionRegion.js'
import { findDemo } from '../demos'

/* The catalog owns this region; the shell supplies its renderer at activation.
   Other plugins contribute without the catalog knowing their identities. */
const DETAILS_SIDEBAR = 'demo-catalog/details-sidebar/v1'

const props = defineProps({
  id: { type: String, required: true },
})

const demo = computed(() => findDemo(props.id))
const introHtml = computed(() => (demo.value ? marked.parse(demo.value.intro) : ''))

function launch() {
  window.open(demo.value.launchUrl, '_blank')
}
</script>

<template>
  <div
    v-if="demo"
    class="intro"
  >
    <div class="intro-actions">
      <router-link to="/demos">
        <Button
          label="Back"
          size="small"
          severity="secondary"
          text
          icon="pi pi-arrow-left"
        />
      </router-link>
      <Button
        label="Launch demo"
        size="small"
        icon="pi pi-external-link"
        @click="launch"
      />
    </div>
    <p class="lab-muted run-hint">
      Start it first: <code>docker compose up --build</code> in <code>{{ demo.composeDir }}/</code>
    </p>
    <div class="intro-body">
      <!-- eslint-disable-next-line vue/no-v-html — markdown from our own repo -->
      <article
        class="lab-panel markdown"
        v-html="introHtml"
      />
      <aside class="intro-sidebar">
        <h2>{{ DETAILS_SIDEBAR }}</h2>
        <ExtensionRegion
          :point="DETAILS_SIDEBAR"
          :context="{ demoId: id }"
        />
      </aside>
    </div>
  </div>
  <div
    v-else
    class="lab-panel"
  >
    Unknown demo: {{ id }}
  </div>
</template>

<style scoped>
.intro-body {
  display: flex;
  gap: 18px;
  align-items: flex-start;
}
.intro-body > .markdown {
  flex: 1 1 auto;
  min-width: 0;
}
.intro-sidebar {
  width: 320px;
  flex-shrink: 0;
}
.intro-sidebar h2 {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
  margin: 0 0 10px;
}
.intro-actions {
  display: flex;
  justify-content: space-between;
  margin-bottom: 0.5rem;
}
.run-hint {
  margin: 0 0 1rem;
  font-size: 0.85rem;
}
.markdown :deep(h1) {
  margin-top: 0;
  font-size: 1.3rem;
}
.markdown :deep(h2) {
  font-size: 1.05rem;
  margin-top: 1.5rem;
}
.markdown :deep(code) {
  background: var(--lab-bg);
  padding: 0.1em 0.35em;
  border-radius: 3px;
  font-size: 0.85em;
}
.markdown :deep(pre) {
  background: var(--lab-bg);
  padding: 0.75rem;
  border-radius: 4px;
  overflow-x: auto;
}
.markdown :deep(pre code) {
  padding: 0;
}
</style>
