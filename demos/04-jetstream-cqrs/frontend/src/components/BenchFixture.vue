<script setup>
// Lesson 01 · Performance — Stream information (04.7.16, reshaped by 04.7.18).
//
// One group, one job: say what is in ODOMETER_BENCH, and put it there. It
// reports and it seeds. It does NOT aim the measurement — the picker in the
// group below does that, and two controls aiming one measurement is the
// defect 04.7.18 exists to remove (D16).
//
// Five deliberate choices:
//
//   The stream is named ONCE, in the header, with its count and its bytes.
//   A length is never shown on its own: the standing rule set by the user
//   2026-09-16. A count nobody can price is how 100 000 000 events sounds
//   reasonable until you learn it is 7.6 GB (D15).
//
//   ONE button seeds all three sizes (D18). Three buttons made the reader
//   choose a size before they had any reason to prefer one, and the answer
//   was always "all of them, so I can compare".
//
//   The table always shows all three. A size nobody has seeded is a row that
//   says so. A missing row reads as a screen that has not loaded.
//
//   The press is PRICED BEFORE IT HAPPENS, and reported while it runs. One
//   press writes 1 110 000 events; a screen that went quiet for that long
//   would look broken, and a reader who was never told the cost would be
//   spending disk they did not agree to.
//
//   The button PRINTS ITS COMMANDS (plan section 10.9). Every number this
//   demo shows must be reproducible in a terminal, and a fixture you cannot
//   rebuild by hand is a magic trick.
//
// This writes, and the Performance tab is read-only (04.7.15). Those do not
// disagree: 04.7.15 keeps the tab from writing to the log it is MEASURING.
// This builds a different log, before any measurement of it exists.
import { computed, onMounted, ref } from 'vue'

import Button from 'primevue/button'

import { fetchBench, seedFixture } from '../rehydrate/api.js'
import { BENCH_BYTES_PER_EVENT, BENCH_SIZES, benchCmd, benchVehicle } from '../view/lessons.js'
import { formatBytes, formatCount } from '../view/format.js'

const emit = defineEmits(['state'])

const state = ref(null)
// null when idle. While seeding: which size is being written, how many are
// done, and how many events that is out of the total.
const progress = ref(null)

const ok = computed(() => (state.value?.kind === 'ok' ? state.value : null))
const broken = computed(() => (state.value?.kind === 'broken' ? state.value : null))

// The server's list wins. BENCH_SIZES is only what is drawn before the first
// answer arrives.
const sizes = computed(() => (ok.value?.sizes?.length ? ok.value.sizes : [...BENCH_SIZES]))
const stream = computed(() => ok.value?.stream || 'ODOMETER_BENCH')
const cmds = computed(() => sizes.value.map(benchCmd))

// One row per size, seeded or not. The server only answers for sizes that
// exist, so an unseeded row is built here from the size alone — which is why
// benchVehicle() has to be spelled in JavaScript as well as in Go.
const rows = computed(() =>
  sizes.value.map((size) => {
    const f = ok.value?.fixtures?.find((x) => x.size === size)
    return f ? { ...f, seeded: true } : { size, vehicle: benchVehicle(size), seeded: false }
  }),
)

const totalEvents = computed(() => sizes.value.reduce((sum, n) => sum + n, 0))

// A measured cost beats a remembered one. Once the stream holds anything, its
// own bytes-per-event is the honest divisor; before that, the recorded
// constant is the best this screen can do, and the `~` says so.
const perEvent = computed(() =>
  ok.value?.events > 0 ? ok.value.bytes / ok.value.events : BENCH_BYTES_PER_EVENT,
)
const totalBytes = computed(() => Math.round(totalEvents.value * perEvent.value))

async function load() {
  state.value = await fetchBench()
  emit('state', state.value)
}

// Reading what the fixture holds is a GET. It changes nothing, so it may run
// on mount — unlike the measurement itself, which may not.
onMounted(load)

// One press, three calls, in size order. The shim seeds one size per call and
// stays that way: a streamed per-event count is a bigger change than this
// screen buys, and counting completed sizes is a report nobody can dispute.
async function seedAll() {
  if (progress.value) return
  const list = [...sizes.value]
  let done = 0
  for (const [i, size] of list.entries()) {
    progress.value = {
      vehicle: benchVehicle(size),
      step: i + 1,
      of: list.length,
      done,
      total: totalEvents.value,
    }
    state.value = await seedFixture(size)
    emit('state', state.value)
    done += size
    if (state.value?.kind === 'broken') break
  }
  progress.value = null
}

const pct = computed(() =>
  progress.value?.total ? Math.round((progress.value.done / progress.value.total) * 100) : 0,
)
</script>

<template>
  <section
    class="bench"
    data-testid="bench-fixture"
  >
    <header
      v-if="ok"
      class="head"
      data-testid="bench-holds"
    >
      <h4>Stream information</h4>
      <p class="says">
        <code>{{ stream }}</code> ·
        <b>{{ formatCount(ok.events) }}</b> events ·
        <b>{{ formatBytes(ok.bytes) }}</b>
        <span
          v-if="ok.elapsedMs"
          class="ran"
        >seeded in {{ Math.round(ok.elapsedMs) }} ms</span>
      </p>
    </header>

    <p
      v-if="broken"
      class="broken"
      data-testid="bench-broken"
    >
      {{ broken.error }} — {{ broken.message }}
    </p>

    <template v-else-if="ok">
      <div class="controls">
        <Button
          label="Seed all three"
          icon="pi pi-database"
          size="small"
          :loading="Boolean(progress)"
          :disabled="Boolean(progress)"
          data-testid="bench-seed"
          @click="seedAll"
        />
        <span
          class="cost"
          data-testid="bench-cost"
        >
          {{ formatCount(totalEvents) }} events · ~{{ formatBytes(totalBytes) }} ·
          replaces what is there
        </span>
      </div>

      <div
        v-if="progress"
        class="prog"
        data-testid="bench-progress"
      >
        <p class="prog-head">
          <span>Seeding <b>{{ progress.vehicle }}</b> — {{ progress.step }} of {{ progress.of }}</span>
          <span class="mono">{{ formatCount(progress.done) }} / {{ formatCount(progress.total) }} · {{ pct }}%</span>
        </p>
        <div class="bar">
          <span :style="{ width: `${pct}%` }" />
        </div>
      </div>

      <table data-testid="bench-rows">
        <thead>
          <tr>
            <th>fixture</th>
            <th>events</th>
            <th>snapshot at</th>
            <th>tail left</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="r in rows"
            :key="r.vehicle"
            :class="{ off: !r.seeded }"
            :data-testid="`bench-row-${r.vehicle}`"
          >
            <td><code>{{ r.vehicle }}</code></td>
            <template v-if="r.seeded">
              <td>{{ formatCount(r.events) }}</td>
              <td>seq {{ formatCount(r.snapSeq) }}</td>
              <td>{{ formatCount(r.tailLeft) }}</td>
            </template>
            <td
              v-else
              colspan="3"
            >
              not seeded
            </td>
          </tr>
        </tbody>
      </table>

      <p class="hint">
        The snapshot stops short of the head on purpose. Each fixture leaves a
        tail of events after it, so the cheap side still has to replay
        something — a snapshot that was complete would measure a lookup, not a
        rehydration.
      </p>

      <p
        class="cmd"
        data-testid="bench-cmd"
      >
        Or run them in a terminal — same code, same fixtures:
        <code
          v-for="c in cmds"
          :key="c"
        >{{ c }}</code>
      </p>
    </template>
  </section>
</template>

<style scoped>
.bench {
  padding: 12px 14px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
}

.head {
  display: flex;
  align-items: baseline;
  gap: 12px;
  flex-wrap: wrap;
}

h4 {
  margin: 0;
  font-size: 12.5px;
}

.hint,
.broken,
.cmd,
.says,
.cost {
  margin: 0;
  max-width: 80ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.hint,
.cmd {
  margin-top: 10px;
}

.broken {
  margin-top: 8px;
  color: var(--d4-lost);
}

.controls {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 12px;
}

code {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11.5px;
}

.cmd code {
  margin-right: 10px;
  color: var(--p-text-color);
}

.says b {
  color: var(--p-text-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.ran {
  margin-left: 8px;
  color: var(--p-text-disabled-color);
  font-size: 11px;
}

.prog {
  margin-top: 12px;
  padding: 9px 11px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 5px;
}

.prog-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin: 0;
  font-size: 11.5px;
}

.prog-head .mono {
  color: var(--p-text-muted-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.bar {
  overflow: hidden;
  height: 6px;
  margin-top: 7px;
  border-radius: 3px;
  background: var(--lab-panel-border);
}

.bar span {
  display: block;
  height: 100%;
  background: var(--p-primary-color);
  transition: width 120ms linear;
}

table {
  margin-top: 12px;
  border-collapse: collapse;
  font-size: 12px;
}

th,
td {
  padding: 5px 14px 5px 0;
  text-align: left;
  border-bottom: 1px solid var(--lab-panel-border);
}

th {
  color: var(--p-text-disabled-color);
  font-weight: 500;
}

tr.off td {
  opacity: 0.55;
}
</style>
