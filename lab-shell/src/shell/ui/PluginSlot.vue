<script setup>
/*
  One contribution, rendered — the only place a plugin's component becomes DOM
  (BR-AS02, AS04, AS08).

  Three things are true here and nowhere else:

  - **Loading starts here, not at boot.** The slot asks the loader for the
    plugin's module the first time it is mounted. Until then the contribution
    exists as metadata only, which is what makes the nav tree complete with
    zero chunks fetched.
  - **The failure stops here.** Both failure surfaces are caught — the chunk
    (load, activate) and the render — and either way this slot shows a card
    while its siblings, the plugin's other contributions and the shell's chrome
    carry on. Isolation is at contribution granularity (BR-AS04).
  - **The error text is shell-owned.** Stage and cause code only: never the
    remote's URL, never the underlying message, which can carry a registry
    endpoint or a token in its text. The detail goes to the console for a
    developer, not to the screen.
*/
import { computed, inject, onErrorCaptured, ref, shallowRef, watch } from 'vue'

import { SHELL } from '../shellKey.js'
import PendingExtension from './PendingExtension.vue'
import SkeletonRows from './SkeletonRows.vue'

const props = defineProps({
  contribution: { type: Object, required: true },
  /* Readonly context from the region's owner. Frozen by the host before it
     gets here; the slot passes it straight through (BR-AS07). */
  context: { type: Object, default: () => Object.freeze({}) },
  /* Which placeholder to reserve the space with while the chunk is in flight:
     a fuzzy region for an extension point, sweeping rows for a route panel,
     nothing at all for a topbar control or a footer item (a skeleton in the
     chrome would be noisier than an absence). */
  placeholder: { type: String, default: 'extension' },
  index: { type: Number, default: 0 },
})

const shell = inject(SHELL)
const component = shallowRef(null)
const failure = ref(null)

/* Already-loaded plugins resolve without a tick, so navigating back to a
   plugin's route does not flash a skeleton for code that is in memory. */
function resolveSync(contribution) {
  const module = shell.loader.peek(contribution.pluginId)
  return module ? (module.components?.[contribution.component] ?? null) : null
}

async function resolve(contribution) {
  failure.value = null
  const ready = resolveSync(contribution)
  if (ready) {
    component.value = ready
    return
  }
  component.value = null
  const plugin = shell.plugins.get(contribution.pluginId)
  if (!plugin) {
    failure.value = { stage: 'load', code: 'plugin-not-registered' }
    return
  }
  let module
  try {
    module = await shell.loader.load(plugin)
  } catch (error) {
    /* The loader already recorded the plugin's status and the reason code; the
       slot only needs to know which stage failed. The message is logged, never
       rendered. */
    console.error(`[shell] ${contribution.qualifiedId} failed to load`, error)
    failure.value = {
      stage: 'load',
      code: shell.statuses.get(contribution.pluginId)?.reasonCode ?? 'load-failed',
    }
    return
  }
  const resolved = module.components?.[contribution.component]
  if (!resolved) {
    failure.value = { stage: 'load', code: 'component-not-exported' }
    return
  }
  component.value = resolved
}

watch(() => props.contribution, resolve, { immediate: true })

/* A contribution throwing during render is contained the same way a chunk
   failure is — the slot swaps to the error card and returns false so the error
   stops climbing toward the shell's own frame. */
onErrorCaptured((error) => {
  console.error(`[shell] ${props.contribution.qualifiedId} threw while rendering`, error)
  component.value = null
  failure.value = { stage: 'render', code: 'contribution-threw' }
  return false
})

const label = computed(() => {
  const plugin = shell.plugins.get(props.contribution.pluginId)
  return plugin?.name ?? props.contribution.pluginId
})
</script>

<template>
  <div
    class="plugin-slot"
    :data-contribution="contribution.qualifiedId"
  >
    <div
      v-if="failure"
      class="slot-error"
      role="alert"
    >
      <span class="slot-error-eyebrow">{{ label }} unavailable</span>
      <p>
        Stage <b>{{ failure.stage }}</b> · cause <b>{{ failure.code }}</b>
      </p>
      <p class="slot-error-note">
        The rest of the application is unaffected. Details are in the browser
        console.
      </p>
    </div>

    <component
      :is="component"
      v-else-if="component"
      :context="context"
    />

    <SkeletonRows
      v-else-if="placeholder === 'panel'"
      :label="`Loading ${label}`"
    />
    <PendingExtension
      v-else-if="placeholder === 'extension'"
      :label="contribution.pluginId"
      :index="index"
    />
    <span
      v-else
      class="slot-quiet"
      role="status"
    >Loading {{ label }}…</span>
  </div>
</template>

<style scoped>
.plugin-slot { display: contents; }
.slot-error {
  border: 1px solid var(--err);
  border-radius: 6px;
  padding: 12px 14px;
  background: var(--lab-panel-bg);
}
.slot-error-eyebrow {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--err);
}
.slot-error p { margin: 6px 0 0; color: var(--p-text-muted-color); }
.slot-error-note { font-size: 11px; color: var(--p-text-disabled-color); }
.slot-quiet { font-size: 11px; color: var(--p-text-disabled-color); }
</style>
