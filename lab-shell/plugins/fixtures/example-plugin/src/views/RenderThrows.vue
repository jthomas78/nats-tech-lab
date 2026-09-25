<script setup>
/* Failure mode (c) of BR-AS04: the remote loads, activate() succeeds, and then
   one contribution throws while rendering. Isolation is at CONTRIBUTION
   granularity, so this must cost the plugin only this panel — its route, its
   footer item and its other extension stay up. */
throw new Error('example-plugin/render-throws: deliberate failure during render')
</script>

<template>
  <div>unreachable</div>
</template>
