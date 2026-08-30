<script setup>
/* The shell's footer bar — its own region (`shell/footer/v1`), rendered
   full-bleed under the content by AppShell's #footer slot. Plugins contribute
   status items into it; the shell owns the bar itself (BR-AS09). */
import { computed, inject, onBeforeUnmount, ref, watch } from 'vue'

import { SHELL } from '../shellKey.js'
import { SHELL_API_VERSION } from '../versions.js'
import PluginSlot from './PluginSlot.vue'

const shell = inject(SHELL)
const disconnected = ref(false)
let disconnectTimer = null
watch(() => shell.connection?.connected, (connected) => {
  clearTimeout(disconnectTimer)
  disconnected.value = false
  if (connected === false) disconnectTimer = setTimeout(() => { disconnected.value = true }, 5000)
}, { immediate: true })
onBeforeUnmount(() => clearTimeout(disconnectTimer))
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
      data-testid="registry-unavailable"
    >
      <span class="k">registry</span> unavailable · built-ins only
    </span>
    <!-- BR-AS22: the service answered, and said it could not vouch for what
         it served. Distinct wording from both "unavailable" above and a
         registry that is simply empty — an empty catalog is not a fault. -->
    <span
      v-else-if="shell.registry?.degraded"
      class="degraded"
      data-testid="registry-degraded"
    >
      <span class="k">registry</span> degraded · built-ins only
    </span>
    <PluginSlot
      v-for="item in items"
      :key="item.qualifiedId"
      :contribution="item"
      placeholder="quiet"
    />
    <!-- The two constants an operator needs to read a screenshot: which shell
         contract is running, and which registry it was told to trust. The
         revision, never the endpoint (BR-AS04). -->
    <span class="tail">
      <span
        v-if="disconnected"
        class="degraded"
        data-testid="shell-disconnected"
        role="status"
      >
        connection offline · registry may be out of date ·
      </span>
      <span class="k">shell api</span> {{ SHELL_API_VERSION }} ·
      <span class="k">registry</span> rev {{ shell.registry?.revision ?? 'n/a' }}
    </span>
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
.tail { margin-left: auto; }
.degraded { color: var(--warn); }
</style>
