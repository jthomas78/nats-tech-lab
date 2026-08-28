<script setup>
/* Rendered in place of a route whose plugin would not load (BR-AS04). The
   frame, the nav and every other plugin are untouched — only this panel
   reports the failure.

   Stage and cause code only. The underlying error text is not shown: a
   federation failure quotes the remote's URL back at you, and BR-AS04 puts
   registry URLs, credentials and tokens on a denylist for anything the user
   can see. The detail goes to the console, where a developer looks for it. */
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
      <template v-if="record()?.reasonCode">
        ({{ record().reasonCode }})
      </template>.
      Everything else in the shell is unaffected.
    </p>
    <p class="lab-muted slot-error-note">
      Details are in the browser console.
    </p>
  </div>
</template>
