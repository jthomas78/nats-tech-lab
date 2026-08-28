<script setup>
/* The reserved-region placeholder an extension point shows while its
   contributor's chunk is in flight (BR-AS08). */
import './loading.css'

defineProps({
  label: { type: String, required: true },
  height: { type: Number, default: 120 },
  /* Phase offset, so neighbouring pending regions do not pulse in lockstep. */
  index: { type: Number, default: 0 },
})
</script>

<template>
  <div
    class="skel-ext"
    :class="[`delay-${index % 3}`]"
    :style="{ height: `${height}px` }"
    role="status"
    :aria-label="`Loading ${label}`"
  >
    <span class="skel-ext-label">{{ label }} · awaiting chunk</span>
  </div>
</template>
