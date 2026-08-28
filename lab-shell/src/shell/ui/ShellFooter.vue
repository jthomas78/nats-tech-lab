<script setup>
/* The shell's footer bar — its own region (`shell/footer/v1`), rendered
   full-bleed under the content by AppShell's #footer slot. Plugins contribute
   status items into it; the shell owns the bar itself (BR-AS09). */
import { computed, inject } from 'vue'

import { SHELL } from '../shellKey.js'
import PluginSlot from './PluginSlot.vue'

const shell = inject(SHELL)
const items = computed(() => shell.contributions.shellFooter)
const pluginCount = computed(
  () => [...shell.statuses.values()].filter((r) => r.status !== 'incompatible').length,
)
</script>

<template>
  <div class="footbar">
    <span><span class="k">plugins</span> {{ pluginCount }}</span>
    <span
      v-if="shell.registryError"
      class="degraded"
    >
      <span class="k">registry</span> unavailable · built-ins only
    </span>
    <PluginSlot
      v-for="item in items"
      :key="item.qualifiedId"
      :contribution="item"
      placeholder="quiet"
    />
  </div>
</template>

<style scoped>
.footbar {
  display: flex;
  align-items: center;
  gap: 22px;
  height: 26px;
  padding: 0 16px;
  border-top: 1px solid var(--lab-panel-border);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.k { color: var(--p-text-disabled-color); }
.degraded { color: var(--warn); }
</style>
