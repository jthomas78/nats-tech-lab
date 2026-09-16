<script setup>
// Lesson 01 · Rehydrate — the demo's headline question, on screen.
//
//   "Rehydrating an aggregate — how much does a snapshot buy you?"
//
// Every other tab watches a projection arrive over the WebSocket. This one
// asks the write side to rebuild an aggregate twice, right now, and puts the
// two costs side by side. It is the only read on this screen that goes over
// HTTP, and rehydrate/api.js explains why.
//
// Three deliberate choices:
//
//   It does not run on its own. Rebuilding from sequence 1 reads the whole
//   log for that vehicle, and that must never happen because somebody
//   clicked a tab. You press the button.
//
//   It needs a vehicle. "All vehicles" is not a thing you can rehydrate —
//   an aggregate is one vehicle, and a rule is checked against one.
//
//   It never hides a disagreement. If the two sides rebuild different
//   states, verdict() returns `void` and the ratio is not shown at all.
//   A flattering multiple over two different vehicles is worse than no
//   number, and 04.7.12 is why that sentence is in this file.
import { computed, ref, watch } from 'vue'

import Button from 'primevue/button'

import VehiclePicker from './VehiclePicker.vue'
import BenchFixture from './BenchFixture.vue'
import { fetchBoth, fetchRehydration } from '../rehydrate/api.js'
import { formatCount } from '../view/format.js'
import { formatMs, formatSpeedup, trailsBy, verdict } from '../view/rehydrate.js'

const props = defineProps({
  vehicles: { type: Array, default: () => [] },
  writes: { type: Object, default: () => new Map() },
  reads: { type: Object, default: () => new Map() },
})

// WHICH log is being rebuilt, and whose history it is.
//
// null means the demo's own log and the vehicle the picker is on. A seeded
// fixture sets it to the bench stream instead — those vehicles live in a
// different bucket, so they never appear in the picker and could not be
// reached any other way.
const vehicle = ref(null)
const target = ref(null)
const active = computed(() => target.value ?? { source: 'live', vehicle: vehicle.value })

const cold = ref(null)
const warm = ref(null)
const busy = ref('')
const ranAt = ref(null)
let targetVersion = 0

// A result only ever describes the vehicle it was measured on. Switching the
// picker clears both halves rather than leaving V1's numbers under V2's name.
watch(vehicle, () => measure(null))

watch(() => props.vehicles, (next) => {
  if (vehicle.value && !next.includes(vehicle.value)) vehicle.value = null
})

const okCold = computed(() => (cold.value?.kind === 'ok' ? cold.value : null))
const okWarm = computed(() => (warm.value?.kind === 'ok' ? warm.value : null))
const result = computed(() => verdict(okCold.value, okWarm.value))
const ratio = computed(() =>
  result.value.kind === 'measured' ? formatSpeedup(result.value.ratio) : '',
)

// A new target is a new subject. The old numbers are cleared rather than left
// under the new name — the same reason switching the picker clears them.
function measure(next) {
  targetVersion += 1
  target.value = next
  cold.value = null
  warm.value = null
  ranAt.value = null
}

function backToLive() {
  measure(null)
}

async function runBoth() {
  if (!active.value.vehicle || busy.value) return
  const version = targetVersion
  busy.value = 'both'
  const both = await fetchBoth(active.value.vehicle, { source: active.value.source })
  busy.value = ''
  if (version !== targetVersion) return
  cold.value = both.cold
  warm.value = both.warm
  ranAt.value = new Date()
}

async function runOne(snapshot) {
  if (!active.value.vehicle || busy.value) return
  const version = targetVersion
  busy.value = snapshot ? 'warm' : 'cold'
  const out = await fetchRehydration(active.value.vehicle, snapshot, {
    source: active.value.source,
  })
  busy.value = ''
  if (version !== targetVersion) return
  if (snapshot) warm.value = out
  else cold.value = out
  ranAt.value = new Date()
}

// One description per side, so the template holds no branching about which
// half it is drawing.
const sides = computed(() => [
  {
    key: 'cold',
    tone: 'cold',
    title: 'From sequence 1',
    flag: '-snapshot=false',
    out: cold.value,
  },
  {
    key: 'warm',
    tone: 'warm',
    title: 'From the snapshot, then the tail',
    flag: '-snapshot=true',
    out: warm.value,
  },
])

function rows(out) {
  return [
    { term: 'replayed from', value: `seq ${formatCount(out.fromSeq)}` },
    { term: 'events read', value: formatCount(out.eventsRead) },
    { term: 'last seq', value: formatCount(out.lastSeq) },
    { term: 'state', value: `${out.status || '(unknown)'} · ${out.plate || '—'}` },
  ]
}
</script>

<template>
  <section
    class="rehydrate"
    data-testid="rehydrate-panel"
  >
    <header>
      <h3>How much does a snapshot buy you?</h3>
      <p class="hint">
        Rebuilds the same aggregate twice — once from sequence 1, once from the
        snapshot plus the tail. It reads only: nothing is appended to the log,
        so you can press it as often as you like.
      </p>
    </header>

    <BenchFixture @measure="measure" />

    <div class="controls">
      <span>Live vehicle · ODOMETER</span>
      <VehiclePicker
        v-model="vehicle"
        :vehicles="vehicles"
        :writes="writes"
        :reads="reads"
        aria-label="Choose a live vehicle to rehydrate"
      />
    </div>

    <p
      v-if="!active.vehicle"
      class="empty"
      data-testid="rehydrate-needs-vehicle"
    >
      Pick a vehicle above, or seed a fixture and measure that. An aggregate is
      one vehicle, so there is no such thing as rehydrating all of them.
    </p>

    <template v-else>
      <p
        class="target"
        data-testid="rehydrate-target"
      >
        Measuring <code>{{ active.vehicle }}</code> on
        <code>{{ active.source === 'bench' ? 'ODOMETER_BENCH' : 'ODOMETER' }}</code>.
        <button
          v-if="active.source === 'bench'"
          type="button"
          class="back"
          data-testid="rehydrate-back-to-live"
          @click="backToLive"
        >
          Measure the demo's own log instead
        </button>
      </p>

      <div class="controls">
        <Button
          label="Run both"
          icon="pi pi-play"
          size="small"
          :loading="busy === 'both'"
          :disabled="Boolean(busy)"
          data-testid="rehydrate-run-both"
          @click="runBoth"
        />
        <Button
          label="Run no-snapshot only"
          severity="secondary"
          outlined
          size="small"
          :loading="busy === 'cold'"
          :disabled="Boolean(busy)"
          @click="runOne(false)"
        />
        <Button
          label="Run snapshot only"
          severity="secondary"
          outlined
          size="small"
          :loading="busy === 'warm'"
          :disabled="Boolean(busy)"
          @click="runOne(true)"
        />
        <span
          v-if="ranAt"
          class="ran"
        >ran at {{ ranAt.toTimeString().slice(0, 8) }}</span>
      </div>

      <div class="cols">
        <article
          v-for="s in sides"
          :key="s.key"
          class="side"
          :class="s.tone"
          :data-testid="`rehydrate-${s.key}`"
        >
          <header>
            <h4>{{ s.title }}</h4>
            <code>{{ s.flag }}</code>
          </header>

          <template v-if="s.out?.kind === 'ok'">
            <p class="big">
              {{ formatMs(s.out.elapsedMs) }}<small>ms</small>
            </p>
            <dl>
              <template
                v-for="r in rows(s.out)"
                :key="r.term"
              >
                <dt>{{ r.term }}</dt>
                <dd>{{ r.value }}</dd>
              </template>
            </dl>
          </template>

          <p
            v-else-if="s.out?.kind === 'broken'"
            class="broken"
          >
            {{ s.out.error }} — {{ s.out.message }}
          </p>
          <p
            v-else
            class="empty"
          >
            Not run yet.
          </p>
        </article>
      </div>

      <div
        class="verdict"
        :class="result.kind"
        data-testid="rehydrate-verdict"
      >
        <p
          v-if="ratio"
          class="n"
        >
          About {{ ratio }} faster
        </p>
        <p class="say">
          {{ result.text }}
        </p>
      </div>

      <p
        v-if="okWarm"
        class="stale"
        data-testid="rehydrate-stale"
      >
        <b>The snapshot is always stale, and that is not a bug.</b>
        The write consumer is asynchronous, so the snapshot trails the log — it
        trailed by {{ formatCount(trailsBy(okWarm)) }} event(s) on this run.
        Rehydration reads the snapshot and then replays that tail. Code that
        stops at the snapshot is wrong, not fast.
      </p>
    </template>
  </section>
</template>

<style scoped>
.rehydrate {
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
}

h3 {
  margin: 0 0 2px;
  font-size: 13px;
}

.hint {
  margin: 0;
  max-width: 78ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.controls {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 14px;
}

.ran {
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

.cols {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 16px;
  margin-top: 16px;
}

/* The two sides are NOT the write/read colours. This tab compares two ways of
   doing the same write-side job, so --d4-write on one of them would claim a
   CQRS split that is not what is being shown. Loss red for the expensive
   side, read teal for the cheap one. */
.side {
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  border-top: 2px solid var(--lab-panel-border);
}

.side.cold {
  border-top-color: var(--d4-lost);
}

.side.warm {
  border-top-color: var(--d4-read);
}

.side header {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
}

h4 {
  margin: 0;
  font-size: 12.5px;
}

.side code {
  color: var(--p-text-disabled-color);
  font-size: 11px;
}

.big {
  margin: 10px 0 0;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 32px;
  line-height: 1.1;
  font-weight: 600;
}

.cold .big {
  color: var(--d4-lost);
}

.warm .big {
  color: var(--d4-read);
}

.big small {
  margin-left: 4px;
  color: var(--p-text-muted-color);
  font-size: 14px;
  font-weight: 400;
}

dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 5px 18px;
  margin: 12px 0 0;
  padding-top: 10px;
  border-top: 1px solid var(--lab-panel-border);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

dt {
  color: var(--p-text-disabled-color);
}

dd {
  margin: 0;
}

.verdict {
  margin-top: 16px;
  padding: 12px 14px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
}

.verdict.measured {
  border-color: color-mix(in srgb, var(--d4-read) 40%, var(--lab-panel-border));
}

.verdict.void {
  border-color: color-mix(in srgb, var(--d4-lost) 55%, var(--lab-panel-border));
}

.verdict .n {
  margin: 0;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 24px;
  font-weight: 600;
  color: var(--d4-read);
}

.verdict.void .say {
  color: var(--d4-lost);
}

.say {
  margin: 6px 0 0;
  max-width: 80ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.target {
  margin: 12px 0 0;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.target code {
  color: var(--p-text-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.back {
  margin-left: 8px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--d4-read);
  font: inherit;
  cursor: pointer;
  text-decoration: underline;
}

.empty,
.broken,
.stale {
  margin: 12px 0 0;
  max-width: 78ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.broken {
  color: var(--d4-lost);
}

.stale b {
  color: var(--p-text-color);
}
</style>
