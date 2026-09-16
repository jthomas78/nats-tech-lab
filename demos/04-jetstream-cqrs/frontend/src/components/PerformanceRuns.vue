<script setup>
// Lesson 02 · Performance, run live (04.9.7, decisions D2, D4, D5, D7, D9, D12).
//
// The tab's question is the plainest one in the demo: does adding workers make
// it faster, and what does the speed cost? Until now the answer was four rows
// recorded on 2026-09-15 under a heading that said "1 vs 4" — a name that
// described two of its own four rows.
//
// D9 — one knob per tab. Starvation varies MaxAckPending at eight workers;
// this varies the workers at one cap. Both moving at once would measure
// neither.

import { computed, watch } from 'vue'

import RunControl from './RunControl.vue'
import { runPool, seedPool } from '../pool/api.js'
import { useRunSet } from '../pool/useRunSet.js'
import {
  PERFORMANCE_MAX_PENDING,
  PERFORMANCE_SECONDS,
  PERFORMANCE_WORKERS,
  poolRunCmd,
} from '../view/lessons.js'

const props = defineProps({
  events: { type: Number, default: 10_000 },
  locked: { type: Boolean, default: false },
  ackWaitMs: { type: Number, default: 30_000 },
  // D3 — the bar reads the bucket the page already watches.
  workers: { type: Object, default: () => new Map() },
})

const plans = PERFORMANCE_WORKERS.map((workers) => ({
  workers,
  maxPending: PERFORMANCE_MAX_PENDING,
  drain: true,
}))

const set = useRunSet({
  label: 'Performance',
  events: props.events,
  ackWaitMs: props.ackWaitMs,
  plans,
  seed: (size) => seedPool(size),
  run: (cfg) => runPool(cfg),
})

watch(
  () => props.workers,
  (map) => set.observe([...(map?.values?.() ?? [])], Date.now()),
  { deep: true },
)

defineExpose({ observe: set.observe })

const commands = plans.map((p) => poolRunCmd({ workers: p.workers, maxPending: p.maxPending }))

// The scale is the slowest run SO FAR, so the shape appears as the runs land
// instead of jumping when the last one arrives.
const slowest = computed(() =>
  Math.max(0, ...set.rows.value.map((r) => r.result?.seconds ?? 0)),
)

// The control row. One worker cannot race itself, so it is the only run that
// is certain to fold the whole log — every speed-up is measured against it.
const control = computed(() => set.rows.value[0]?.result ?? null)

const rows = computed(() =>
  set.rows.value.map((r) => ({
    workers: r.plan.workers,
    done: r.result !== null,
    result: r.result,
    barPct: r.result && slowest.value > 0 ? (r.result.seconds / slowest.value) * 100 : 0,
    // Blank, not 1.0x, until the control has run. A speed-up with nothing to
    // compare against is a number the reader cannot check.
    speedUp:
      r.result && control.value && r.result.seconds > 0
        ? control.value.seconds / r.result.seconds
        : null,
    lossPct: r.result && r.result.events > 0 ? (r.result.dropped / r.result.events) * 100 : 0,
  })),
)
</script>

<template>
  <div
    class="card measured"
    data-testid="performance-runs"
  >
    <p class="eyebrow">
      What workers buy you — four runs, made when you press
    </p>

    <table class="rt">
      <thead>
        <tr>
          <th>workers</th>
          <th>time</th>
          <th>rate</th>
          <th>speed-up</th>
          <th>folded</th>
          <th>dropped</th>
          <th>loss</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="r in rows"
          :key="r.workers"
          :data-testid="`performance-workers-${r.workers}`"
          :class="{ empty: !r.done }"
        >
          <td><code>{{ r.workers }}</code></td>
          <template v-if="r.result">
            <td>
              <span class="track wide"><span
                class="fill"
                :style="{ width: `${r.barPct}%` }"
              /></span>
              {{ r.result.seconds.toFixed(1) }}s
            </td>
            <td>{{ r.result.rate.toLocaleString('en-GB') }}/s</td>
            <td>{{ r.speedUp === null ? '—' : `${r.speedUp.toFixed(1)}x` }}</td>
            <td>{{ r.result.acked.toLocaleString('en-GB') }}</td>
            <td :class="r.result.dropped ? 'lost' : 'ok'">
              {{ r.result.dropped.toLocaleString('en-GB') }}
            </td>
            <td :class="r.result.dropped ? 'lost' : 'ok'">
              {{ r.lossPct.toFixed(1) }}%
            </td>
          </template>
          <!-- D5. A row that has not run says so, and draws no bar. -->
          <td
            v-else
            class="nope"
            colspan="6"
          >
            not run yet
          </td>
        </tr>
      </tbody>
    </table>

    <p
      v-if="set.broken.value"
      class="note lost"
      data-testid="performance-broken"
    >
      {{ set.broken.value.error }} — {{ set.broken.value.message }}
    </p>

    <RunControl
      label="Run all four worker counts"
      :runs="plans.length"
      :seconds="PERFORMANCE_SECONDS"
      :commands="commands"
      :locked="props.locked"
      :running="set.running.value"
      :percent="set.progress.percent.value"
      :note="set.progress.note.value"
      :stalled="set.progress.stalled.value"
      @run="set.press"
    />

    <p class="note hard">
      <b>The one-worker row is the control.</b> One worker cannot race itself,
      so it is the only run guaranteed to fold every event. Every speed-up in
      the table is measured against it, and so is every loss.
    </p>
    <p class="note hard">
      Workers are cheap and the speed-up is real, but it flattens: the log has
      to be read off one consumer either way. Read the dropped column at the
      same time — the speed is only free while it stays at zero.
    </p>

    <p class="note">
      Each run is a <code>-drain</code>: it folds the log from sequence 1 and
      stops at zero pending, so all four read the same log from the start and
      the times can be compared. <code>MaxAckPending</code> is held at
      <code>{{ PERFORMANCE_MAX_PENDING }}</code> for all four — the Starvation
      tab is the one that moves it. The log is re-seeded between runs, because
      draining it empties it.
    </p>
  </div>
</template>

<style scoped>
tr.empty {
  opacity: 0.55;
}

.nope {
  color: var(--p-text-muted-color);
  font-style: italic;
}
</style>
