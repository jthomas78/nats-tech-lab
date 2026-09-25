<script setup>
/* A route-scoped `shell-control` into `shell/topbar-controls/v1`. It appears
   only under the routes its registry entry names — a plugin cannot park a
   control in the topbar for the whole session (BR-AS07). */
import { ref } from 'vue'

const live = ref(true)
</script>

<template>
  <button
    type="button"
    class="ex-control"
    @click="live = !live"
  >
    <span
      class="pip"
      :class="{ off: !live }"
    />
    {{ live ? 'Streaming' : 'Paused' }}
  </button>
</template>

<style scoped>
.ex-control {
  display: inline-flex; align-items: center; gap: 6px;
  font: inherit; font-size: 11px; font-weight: 600;
  padding: 3px 9px; border-radius: 100px;
  border: 1px solid var(--lab-panel-border);
  background: none; color: var(--p-text-muted-color);
  cursor: pointer;
}
.pip { width: 6px; height: 6px; border-radius: 50%; background: var(--ok); }
.pip.off { background: var(--p-text-disabled-color); }
</style>
