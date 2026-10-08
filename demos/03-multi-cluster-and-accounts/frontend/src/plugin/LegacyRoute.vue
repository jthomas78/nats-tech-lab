<script setup>
// An old link: /demo-03/playground or /demo-03/overview. It forwards, in
// place, to the matching tab of the Meta-Leader page, keeping the query and
// hash. It draws nothing and starts nothing — no poll, no command — so an old
// bookmark costs the rig nothing. /demo-03/playground is also the default
// route, so Home and the bare /demo-03 open the Live tab through here.
import { onMounted } from 'vue'

import { goToTab, LEGACY_TABS, useShellRouter } from './tabs.js'

const props = defineProps({
  routeId: { type: String, default: 'playground' },
})

const router = useShellRouter()

onMounted(() => {
  if (router) goToTab(router, LEGACY_TABS[props.routeId] ?? 'overview', { replace: true })
})
</script>

<template>
  <span
    hidden
    data-testid="legacy-route"
  />
</template>
