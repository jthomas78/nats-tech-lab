<script setup>
// "Dark" is SIGSTOP of this cluster's three processes (rule 4) — never a
// partition. The box shows what the service read from the OS: the phase,
// how many of three it has confirmed, and the time since the change.
// Resume stays enabled while other commands are pending: Resume always wins.
import { computed } from 'vue'

import { nsToS, pendingOf } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
  actions: { type: Object, required: true },
  cluster: { type: String, required: true },
})

const view = computed(() =>
  (props.state.snap?.clusters ?? []).find((c) => c.cluster === props.cluster) ?? { phase: 'mixed', stopped: 0, running: 0 },
)
const since = computed(() => nsToS(view.value.sinceNs))
const reachable = computed(() => props.state.service.reachable === true)
const ready = computed(() => props.state.snap?.rig?.status === 'ready')
const noRig = computed(() => props.state.snap?.rig?.status !== 'ready')

const freezing = computed(() => pendingOf(props.state.snap, 'freeze', props.cluster))
const resuming = computed(() => pendingOf(props.state.snap, 'resume', props.cluster))

const box = computed(() => {
  const v = view.value
  const t = since.value ? `${since.value.toFixed(1)} s` : '—'
  switch (v.phase) {
    case 'going_dark':
      return { cls: 'moving', title: 'Going dark', spin: true, text: `SIGSTOP sent ${t} ago. ${v.stopped} of 3 confirmed stopped.` }
    case 'coming_back':
      return { cls: 'moving', title: 'Coming back', spin: true, text: `SIGCONT sent ${t} ago. ${v.running} of 3 confirmed running.` }
    case 'dark':
      return { cls: 'on', title: 'Dark: on', text: `${v.stopped} of 3 stopped (ps state T). Dark for ${t}.` }
    case 'off':
      return { cls: '', title: 'Dark: off', text: `${v.running} of 3 running.${v.sinceNs ? ` Resumed ${t} ago.` : ''}` }
    default:
      if (noRig.value) return { cls: '', title: 'Dark: off', text: 'No rig processes.' }
      return { cls: 'mixed', title: 'Mixed', text: `${v.running} running, ${v.stopped} stopped. Not asked for by this page.` }
  }
})

const canFreeze = computed(
  () => reachable.value && ready.value && !freezing.value && !['dark', 'going_dark'].includes(view.value.phase) && view.value.running > 0,
)
const canResume = computed(
  () => reachable.value && !resuming.value && (view.value.wanted || view.value.stopped > 0 || view.value.phase === 'going_dark'),
)
</script>

<template>
  <div
    class="darkrow"
    :data-testid="`dark-${cluster}`"
  >
    <div
      class="darkbox"
      :class="box.cls"
    >
      <span class="ds"><span
        v-if="box.spin"
        class="spin"
      />{{ box.title }}</span>
      <span class="dd">{{ box.text }}</span>
    </div>
    <button
      class="btn sm darkb"
      data-testid="freeze"
      :disabled="!canFreeze"
      @click="actions.freeze(cluster)"
    >
      Freeze
    </button>
    <button
      class="btn sm"
      data-testid="resume"
      :disabled="!canResume"
      @click="actions.resume(cluster)"
    >
      Resume
    </button>
  </div>
</template>
