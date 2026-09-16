<script setup>
// Lesson 02 · one run, one press (04.9.8, decisions D2, D4, D6, D8, D12).
//
// Live and Redelivery each ask ONE question, so there is nothing to sequence
// and nothing to re-seed between. The set components own the other two tabs;
// this one owns the two that run once.
//
// D6 — Stop is a prop, not a default. Live runs open-ended and is the only
// tab that can be stopped mid-run. Every other run ends by itself, and a Stop
// button on one of those would suggest it might not.

import { computed, ref, watch } from 'vue'

import RedeliveryTimeline from './RedeliveryTimeline.vue'
import RunControl from './RunControl.vue'
import { runPool, stopPool } from '../pool/api.js'
import { useRunProgress } from '../pool/useRunProgress.js'
import { poolRunCmd } from '../view/lessons.js'

const props = defineProps({
  label: { type: String, required: true },
  // The exact config the press sends. It is also what gets printed, so the
  // line under the button cannot disagree with the run above it.
  plan: { type: Object, required: true },
  events: { type: Number, default: 10_000 },
  seconds: { type: Number, default: 0 },
  locked: { type: Boolean, default: false },
  stoppable: { type: Boolean, default: false },
  ackWaitMs: { type: Number, default: 30_000 },
  workers: { type: Object, default: () => new Map() },
})

const progress = useRunProgress()
const result = ref(null)
const broken = ref(null)
const busy = ref(false)

watch(
  () => props.workers,
  (map) => progress.observe([...(map?.values?.() ?? [])], Date.now()),
  { deep: true },
)

const press = async () => {
  if (busy.value) return
  busy.value = true
  broken.value = null
  result.value = null
  progress.start({ label: props.label, runs: 1, events: props.events, ackWaitMs: props.ackWaitMs })
  try {
    const out = await runPool({ ...props.plan })
    if (out.kind === 'ok') result.value = out
    else broken.value = out
  } finally {
    progress.finish()
    busy.value = false
  }
}

// Stop goes through the shim. Dropping the promise on the floor would leave
// the workers folding on a screen that had stopped saying so — the run
// outlives the request on purpose (D11), so only the shim can end it.
const halt = async () => {
  const out = await stopPool()
  if (out.kind !== 'ok') broken.value = out
}

const commands = computed(() => [poolRunCmd(props.plan)])
</script>

<template>
  <div
    class="card"
    data-testid="single-run"
  >
    <RunControl
      :label="props.label"
      :runs="1"
      :workers="props.plan.workers"
      :seconds="props.seconds"
      :commands="commands"
      :locked="props.locked"
      :running="busy"
      :stoppable="props.stoppable"
      :percent="progress.percent.value"
      :note="progress.note.value"
      :stalled="progress.stalled.value"
      @run="press"
      @stop="halt"
    />

    <p
      v-if="result"
      class="note"
      data-testid="single-result"
    >
      Folded <b>{{ result.acked.toLocaleString('en-GB') }}</b> events in
      <b>{{ result.seconds.toFixed(1) }}s</b> at
      {{ result.rate.toLocaleString('en-GB') }}/s, with
      <b :class="result.dropped ? 'lost' : 'ok'">{{ result.dropped.toLocaleString('en-GB') }}</b>
      dropped. {{ result.share.busy }} of {{ result.share.workers }} workers acked.
    </p>

    <!-- The fault the run injected, drawn from the run that injected it
         (04.9.9). Absent unless the run redelivered something: a drawing of a
         redelivery that did not happen is worse than no drawing. -->
    <RedeliveryTimeline
      v-if="result?.redelivery"
      :run="result.redelivery"
    />

    <p
      v-if="broken"
      class="note lost"
      data-testid="single-broken"
    >
      {{ broken.error }} — {{ broken.message }}
    </p>
  </div>
</template>
