<script setup>
// Lesson 02 · Starvation, run live (04.9.6, decisions D2, D4, D5, D7, D12).
//
// The tab's claim is a surprising one: MaxAckPending does NOT idle a worker.
// It throttles the consumer, and what it really is, is the loss dial. A
// smaller cap is slower and drops less, because there is less in flight to
// reorder with.
//
// Until now that claim rested on three numbers recorded on 2026-09-16. A
// reader who disbelieved it had nothing to do about it. Now the press makes
// the runs, so disbelieving it is a button.
//
// D12 — four caps, 1 / 3 / 8 / 64, and the list is not shortened.
// D7  — the log is re-seeded between runs, because each run drains it.
// D5  — a row that has not been run is GREY, never filled. No fallback: a
//       fallback is how a screen shows numbers after a run that failed.

import { computed, watch } from 'vue'

import RunControl from './RunControl.vue'
import { runPool, seedPool } from '../pool/api.js'
import { useRunSet } from '../pool/useRunSet.js'
import {
  poolRunCmd,
  STARVATION_CAPS,
  STARVATION_SECONDS,
  STARVATION_WORKERS,
} from '../view/lessons.js'

const props = defineProps({
  // How big the log is, read live from the fixture above the tabs. The set
  // re-seeds to the same size, so every run folds the same number of events.
  events: { type: Number, default: 10_000 },
  // Somebody else holds the shim (D8).
  locked: { type: Boolean, default: false },
  // The consumer's AckWait, so the bar knows what "stopped moving" means.
  ackWaitMs: { type: Number, default: 30_000 },
  // D3 — the progress bar reads the workers bucket the page ALREADY watches.
  // No new transport. The map arrives from PoolPanel, which holds the KV
  // watch; this component only folds it into a percentage.
  workers: { type: Object, default: () => new Map() },
})

const plans = STARVATION_CAPS.map((maxPending) => ({
  workers: STARVATION_WORKERS,
  maxPending,
  drain: true,
}))

const set = useRunSet({
  label: 'Starvation',
  events: props.events,
  ackWaitMs: props.ackWaitMs,
  plans,
  seed: (size) => seedPool(size),
  run: (cfg) => runPool(cfg),
})

// A press without a moving bar is the failure the bar exists to prevent, and
// that is exactly what shipped until it was clicked. The watch is what wires
// D3 up: every heartbeat the bucket delivers is handed to the run set.
watch(
  () => props.workers,
  (map) => set.observe([...(map?.values?.() ?? [])], Date.now()),
  { deep: true },
)

defineExpose({ observe: set.observe })

const commands = plans.map((p) => poolRunCmd({ workers: p.workers, maxPending: p.maxPending }))

// The bar on each row is read against the SLOWEST run of the set so far, so
// the shape appears as the runs land instead of jumping when the last one
// arrives. Nothing is drawn for a row that has not run.
const slowest = computed(() =>
  Math.max(0, ...set.rows.value.map((r) => r.result?.seconds ?? 0)),
)

const rows = computed(() =>
  set.rows.value.map((r) => ({
    cap: r.plan.maxPending,
    done: r.result !== null,
    result: r.result,
    barPct: r.result && slowest.value > 0 ? (r.result.seconds / slowest.value) * 100 : 0,
    lossPct: r.result && r.result.events > 0 ? (r.result.dropped / r.result.events) * 100 : 0,
  })),
)
</script>

<template>
  <div
    class="card measured"
    data-testid="starvation-runs"
  >
    <p class="eyebrow">
      What the cap actually does — four runs, made when you press
    </p>

    <table class="rt">
      <thead>
        <tr>
          <th>MaxAckPending</th>
          <th>time</th>
          <th>rate</th>
          <th>workers that acked</th>
          <th>folded</th>
          <th>dropped</th>
          <th>loss</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="r in rows"
          :key="r.cap"
          :data-testid="`starvation-cap-${r.cap}`"
          :class="{ empty: !r.done }"
        >
          <td><code>{{ r.cap }}</code></td>
          <template v-if="r.result">
            <td>
              <span class="track wide"><span
                class="fill"
                :style="{ width: `${r.barPct}%` }"
              /></span>
              {{ r.result.seconds.toFixed(1) }}s
            </td>
            <td>{{ r.result.rate.toLocaleString('en-GB') }}/s</td>
            <td class="ok">{{ r.result.share.busy }} of {{ r.result.share.workers }}</td>
            <td>{{ r.result.acked.toLocaleString('en-GB') }}</td>
            <td :class="r.result.dropped ? 'lost' : 'ok'">
              {{ r.result.dropped.toLocaleString('en-GB') }}
            </td>
            <td :class="r.result.dropped ? 'lost' : 'ok'">
              {{ r.lossPct.toFixed(1) }}%
            </td>
          </template>
          <!-- D5. An empty row says it has not run. It does NOT say 0s, and
               it does not borrow the last run's number. -->
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
      data-testid="starvation-broken"
    >
      {{ set.broken.value.error }} — {{ set.broken.value.message }}
    </p>

    <RunControl
      label="Run all four caps"
      :runs="plans.length"
      :workers="STARVATION_WORKERS"
      :seconds="STARVATION_SECONDS"
      :commands="commands"
      :locked="props.locked"
      :running="set.running.value"
      :percent="set.progress.percent.value"
      :note="set.progress.note.value"
      :stalled="set.progress.stalled.value"
      @run="set.press"
    />

    <p class="note hard">
      <b>Watch the "workers that acked" column.</b> The cap is shared across
      the consumer, not given to each worker, so a cap of 1 means one message
      in flight for the whole pool. It still does not idle a worker: one acks,
      a slot frees, the next fetch is served. <code>MaxAckPending</code>
      throttles the <b>consumer</b>.
    </p>
    <p class="note hard">
      What the cap really is, is the <b>loss dial</b> — read the time column
      against the dropped column. An event is dropped under BR-OD08 when two
      workers meet on the <b>same vehicle</b> and the later one folds first,
      so the loss depends on how much is in flight at once. A run that drops
      nothing is a real answer: at this log's size and spread the workers did
      not collide.
    </p>
    <p class="note cmp">
      The nats.io worker-pool page says a low cap "starves a large set of
      workers". Both can be true, about different things. At an
      <b>instant</b>, yes — <code>num_waiting</code> on the consumer sits at 4
      or 5 of the 8 while <code>num_ack_pending</code> holds at the cap. Over
      a <b>run</b>, the table above is the answer. Set the cap for the loss
      you can accept, not to keep workers busy.
    </p>

    <p class="note">
      Each run is a <code>-drain</code>: it folds the log from sequence 1 and
      stops at zero pending, so all four read the same log from the start and
      the times can be compared. The log is re-seeded between runs, because
      draining it empties it.
    </p>
  </div>
</template>

<style scoped>
/* A row that has not run is grey and says so. It is deliberately not a row
   of zeros: a zero is a measurement, and no measurement was made. */
tr.empty {
  opacity: 0.55;
}

.nope {
  color: var(--p-text-muted-color);
  font-style: italic;
}
</style>
