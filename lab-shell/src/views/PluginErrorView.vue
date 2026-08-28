<script setup>
/* Rendered in place of a route whose plugin would not load (BR-AS04). The
   frame, the nav and every other plugin are untouched — only this panel
   reports the failure. */
import { inject } from 'vue'
import { useRoute } from 'vue-router'

import { SHELL } from '../shell/shellKey.js'

const shell = inject(SHELL, null)
const route = useRoute()

const record = () => shell?.statuses.get(route.meta.pluginId) ?? null
</script>

<template>
  <div class="lab-panel">
    <h2>{{ route.meta.title }} could not be loaded</h2>
    <p class="lab-muted">
      The <b>{{ route.meta.pluginId }}</b> plugin failed to load
      <template v-if="record()?.reasonCode"> ({{ record().reasonCode }})</template>.
      Everything else in the shell is unaffected.
    </p>
    <p v-if="record()?.reason" class="lab-muted"><code>{{ record().reason }}</code></p>
  </div>
</template>
