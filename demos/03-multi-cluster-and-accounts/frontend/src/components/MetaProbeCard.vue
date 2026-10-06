<script setup>
// Try metadata operation: create, delete and check a small stream. It needs
// a meta leader; a stream write does not. The result is the service's own
// result text for the last probe, split into its steps.
import { computed } from 'vue'

import { commandKinds, hms, pendingOf, secsSince } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
  actions: { type: Object, required: true },
})

const ready = computed(() => props.state.service.reachable === true && props.state.snap?.rig?.status === 'ready')
const pending = computed(() => pendingOf(props.state.snap, 'probe'))

const last = computed(() => {
  const kinds = commandKinds(props.state.history)
  for (let i = props.state.history.length - 1; i >= 0; i--) {
    const e = props.state.history[i]
    if (e.kind === 'result' && kinds[e.cmd]?.kind === 'probe') return e
  }
  return null
})

const steps = computed(() => {
  const e = last.value
  if (!e) return null
  const m = /^(PG_PROBE_\d+) via (\S+): (.*)$/.exec(e.text)
  if (!m) return { via: '', name: '', parts: [{ cls: 'err', text: e.text }], at: e.at }
  const parts = m[3].split('; ').map((p) => ({ cls: / ok$/.test(p) ? 'ok' : 'err', text: p }))
  return { name: m[1], via: m[2], parts, at: e.at }
})
</script>

<template>
  <div
    class="card"
    data-testid="probe-card"
  >
    <h3>
      Metadata operation
      <button
        class="btn sm"
        data-testid="probe"
        :disabled="!ready || !!pending"
        @click="actions.probe()"
      >
        <template v-if="pending">
          <span class="spin" />Trying
        </template>
        <template v-else>
          Try metadata operation
        </template>
      </button>
    </h3>
    <div class="n">
      Create, delete and check a small stream. It needs a meta leader. Compare it with a stream write.
    </div>
    <div class="probe-res">
      <span
        v-if="pending"
        class="pill pend"
      >pending {{ secsSince(state, pending.at)?.toFixed(1) }} s / 30 s{{ pending.via ? ` via ${pending.via}` : '' }}</span>
      <template v-else-if="steps">
        <span
          v-for="p in steps.parts"
          :key="p.text"
          class="pill"
          :class="p.cls"
          style="white-space: normal"
        >{{ p.text }}</span>
        <span class="n">{{ steps.name }}{{ steps.via ? ` via ${steps.via}` : '' }}, {{ hms(steps.at) }}</span>
      </template>
      <span
        v-else
        class="n"
      >Not tried yet.</span>
    </div>
  </div>
</template>
