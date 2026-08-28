<script setup>
/*
  A host renders one of its extension points with this (BR-AS07).

  The host supplies the point id and the context; the region does the rest —
  which contributions were placed (decided at index time, in registry order,
  within capacity), and one slot each. A host never sees plugin ids, and a
  contributor never chooses where it lands.

  Context is frozen here, on every render, rather than trusted to be frozen by
  the caller: a contributor mutating the host's state through the object it was
  handed is BR-AS02 violated through a legal API.
*/
import { computed, inject } from 'vue'

import { readonlyContext } from '../extensions/extensionPoints.js'
import { SHELL } from '../shellKey.js'
import PluginSlot from './PluginSlot.vue'

const props = defineProps({
  point: { type: String, required: true },
  context: { type: Object, default: () => ({}) },
  placeholder: { type: String, default: 'extension' },
})

const shell = inject(SHELL)
const contributions = computed(() => shell.contributions.extensionsFor(props.point))
const frozen = computed(() => readonlyContext(props.context))
</script>

<template>
  <div
    class="extension-region"
    :data-extension-point="point"
  >
    <PluginSlot
      v-for="(contribution, i) in contributions"
      :key="contribution.qualifiedId"
      :contribution="contribution"
      :context="frozen"
      :placeholder="placeholder"
      :index="i"
    />
  </div>
</template>

<style scoped>
.extension-region {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
