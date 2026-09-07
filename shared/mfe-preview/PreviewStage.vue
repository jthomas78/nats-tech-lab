<script setup>
/*
  One contribution, drawn in a frame that says where it would live.

  Two jobs, both borrowed from the shell's PluginSlot:

  - **The frame is not decoration.** A topbar control rendered in the middle of
    a page looks fine and is wrong; shown in a 44px strip it is immediately
    obvious when it is too tall or the wrong weight. The stage is the closest
    thing the preview has to the shell's opinion about placement.
  - **A render error stops here.** The contribution throwing must not take the
    preview page down with it, for the same reason it must not take the shell
    down: you are usually on this page BECAUSE something is throwing.
*/
import { onErrorCaptured, ref, watch } from 'vue'

const props = defineProps({
  component: { type: [Object, Function], required: true },
  componentProps: { type: Object, default: () => ({}) },
  stage: { type: String, default: 'page' },
})

const failure = ref(null)

watch(() => props.component, () => { failure.value = null })

onErrorCaptured((error) => {
  console.error('[mfe-preview] contribution threw while rendering', error)
  failure.value = String(error?.message ?? error)
  return false
})
</script>

<template>
  <div
    class="mfe-preview-stage"
    :class="`is-${stage}`"
  >
    <span class="mfe-preview-stage-label">{{ stage }}</span>
    <div
      v-if="failure"
      class="mfe-preview-banner is-bad"
    >
      <strong>This contribution threw while rendering.</strong>
      {{ failure }}
    </div>
    <div
      v-else
      class="mfe-preview-stage-inner"
    >
      <component
        :is="component"
        v-bind="componentProps"
      />
    </div>
  </div>
</template>
