<script setup>
// Lesson 01 · Performance — the demo's headline question, on screen.
//
//   "Rehydrating an aggregate — how much does a snapshot buy you?"
//
// Every other tab watches a projection arrive over the WebSocket. This one
// asks the write side to rebuild an aggregate twice, right now, and puts the
// two costs side by side. It is the only read on this screen that goes over
// HTTP, and rehydrate/api.js explains why.
//
// Four deliberate choices:
//
//   It does not run on its own. Rebuilding from sequence 1 reads the whole
//   log for that vehicle, and that must never happen because somebody
//   clicked a tab. You press the button.
//
//   ONE control aims it, and that control sits in the same row as the
//   buttons it aims (D12). The tab used to have two — a live-vehicle picker
//   and a Measure button per fixture — and the one drawn above silently
//   overrode the one drawn below.
//
//   It measures ODOMETER_BENCH and nothing else (D13). The demo's own log is
//   a few dozen events long; a speed-up measured over it is noise, and a tab
//   that offered it would be offering a number worth nothing. No option,
//   label or command on this tab names ODOMETER.
//
//   It never hides a disagreement. If the two sides rebuild different
//   states, verdict() returns `void` and the ratio is not shown at all.
//   A flattering multiple over two different vehicles is worse than no
//   number, and 04.7.12 is why that sentence is in this file.
import { computed, ref } from 'vue'

import Button from 'primevue/button'
import Select from 'primevue/select'

import BenchFixture from './BenchFixture.vue'
import { fetchBoth, fetchRehydration } from '../rehydrate/api.js'
import { BENCH_SIZES, benchVehicle } from '../view/lessons.js'
import { formatCount } from '../view/format.js'
import { formatMs, formatSpeedup, trailsBy, verdict } from '../view/rehydrate.js'

// WHAT is being rebuilt. A fixture vehicle, or null. BenchFixture owns the
// read and hands its answer up, so there is one fetch on the tab and one
// list both halves agree on.
const bench = ref(null)
const target = ref(null)

const cold = ref(null)
const warm = ref(null)
const busy = ref('')
const ranAt = ref(null)
let targetVersion = 0

const ok = computed(() => (bench.value?.kind === 'ok' ? bench.value : null))
const sizes = computed(() => (ok.value?.sizes?.length ? ok.value.sizes : [...BENCH_SIZES]))

// D14 and D15. Every size is an option so a reader can see what is missing;
// only a seeded one can be chosen, because an unseeded one has nothing to
// rebuild. Each option carries its own count — that is the number that
// decides which fixture is worth measuring.
const options = computed(() =>
  sizes.value.map((size) => {
    const f = ok.value?.fixtures?.find((x) => x.size === size)
    const vehicle = f?.vehicle ?? benchVehicle(size)
    return {
      value: vehicle,
      label: vehicle,
      count: f ? `${formatCount(f.events)} events` : 'not seeded',
      disabled: !f,
    }
  }),
)

const okCold = computed(() => (cold.value?.kind === 'ok' ? cold.value : null))
const okWarm = computed(() => (warm.value?.kind === 'ok' ? warm.value : null))
const result = computed(() => verdict(okCold.value, okWarm.value))
const ratio = computed(() =>
  result.value.kind === 'measured' ? formatSpeedup(result.value.ratio) : '',
)

// A new target is a new subject. The old numbers are cleared rather than left
// under the new name — one fixture's measurement under another's label is a
// lie the screen would tell silently.
function measure(next) {
  targetVersion += 1
  target.value = next
  cold.value = null
  warm.value = null
  ranAt.value = null
}

// BenchFixture read the stream, or just seeded it. A fixture that has been
// purged away is no longer something to aim at, so the panel lets go of it.
function onState(next) {
  bench.value = next
  const still = next?.fixtures?.some((f) => f.vehicle === target.value)
  if (target.value && !still) measure(null)
}

async function runBoth() {
  if (!target.value || busy.value) return
  const version = targetVersion
  busy.value = 'both'
  const both = await fetchBoth(target.value, { source: 'bench' })
  busy.value = ''
  if (version !== targetVersion) return
  cold.value = both.cold
  warm.value = both.warm
  ranAt.value = new Date()
}

async function runOne(snapshot) {
  if (!target.value || busy.value) return
  const version = targetVersion
  busy.value = snapshot ? 'warm' : 'cold'
  const out = await fetchRehydration(target.value, snapshot, { source: 'bench' })
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
    <BenchFixture @state="onState" />

    <header>
      <h3>How much does a snapshot buy you?</h3>
      <p class="hint">
        Rebuilds one fixture twice — once from sequence 1, once from the
        snapshot plus the tail. It reads only: nothing is appended, so you can
        press it as often as you like.
      </p>
    </header>

    <div
      class="controls"
      data-testid="rehydrate-controls"
    >
      <Select
        :model-value="target"
        :options="options"
        option-label="label"
        option-value="value"
        option-disabled="disabled"
        placeholder="Pick what to measure"
        size="small"
        class="picker"
        aria-label="Pick a fixture to measure"
        @update:model-value="measure"
      >
        <template #option="{ option }">
          <span class="opt">
            <span>{{ option.label }}</span>
            <span class="c">{{ option.count }}</span>
          </span>
        </template>
      </Select>
      <Button
        label="Run both"
        icon="pi pi-play"
        size="small"
        :loading="busy === 'both'"
        :disabled="!target || Boolean(busy)"
        data-testid="rehydrate-run-both"
        @click="runBoth"
      />
      <Button
        label="No-snapshot only"
        severity="secondary"
        outlined
        size="small"
        :loading="busy === 'cold'"
        :disabled="!target || Boolean(busy)"
        @click="runOne(false)"
      />
      <Button
        label="Snapshot only"
        severity="secondary"
        outlined
        size="small"
        :loading="busy === 'warm'"
        :disabled="!target || Boolean(busy)"
        @click="runOne(true)"
      />
      <span
        v-if="ranAt"
        class="ran"
      >ran at {{ ranAt.toTimeString().slice(0, 8) }}</span>
    </div>

    <p
      v-if="!target"
      class="empty"
      data-testid="rehydrate-needs-fixture"
    >
      Pick a fixture to measure. An aggregate is one vehicle, so there is no
      such thing as rebuilding all of them at once.
    </p>
    <p
      v-else
      class="target"
      data-testid="rehydrate-target"
    >
      Measuring <code>{{ target }}</code> on <code>ODOMETER_BENCH</code>.
    </p>

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
  </section>
</template>

<style scoped>
.rehydrate {
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
}

.rehydrate > header {
  margin-top: 20px;
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

.picker {
  min-width: 190px;
}

.opt {
  display: flex;
  gap: 16px;
  justify-content: space-between;
  width: 100%;
}

.opt .c {
  color: var(--p-text-muted-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11.5px;
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
