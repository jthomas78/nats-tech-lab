<script setup>
// Lesson 01 · Rehydrate — the fixture the measurement runs against (04.7.16).
//
// The demo's headline number needs a long log, and the demo's own log is
// short. This control builds one: a disposable stream, ODOMETER_BENCH, with
// 10 000 / 100 000 / 1 000 000 events in it.
//
// Four deliberate choices:
//
//   The sizes come from the SERVER, not from this file. A screen that offered
//   a size the write side does not have would be offering a 400.
//
//   The button PRINTS ITS COMMAND (plan section 10.9). Every number this demo
//   shows must be reproducible in a terminal, and a fixture you cannot rebuild
//   by hand is a magic trick.
//
//   No progress bar, no cancel, no background job. A million events is about
//   2.2 seconds — measured, and written into the plan. A progress bar would
//   take longer to read than the work takes to finish.
//
//   A length is NEVER shown on its own. The standing rule, set by the user
//   2026-09-16: wherever this demo reports a stream's message count, it
//   reports the bytes that count consumes as well. A count nobody can price
//   is how 100 000 000 events sounds reasonable until you learn it is 7.6 GB.
//
// This writes, and the Rehydrate tab is read-only (04.7.15). Those do not
// disagree: 04.7.15 keeps the tab from writing to the log it is MEASURING.
// This builds a different log, before any measurement of it exists.
import { computed, onMounted, ref } from 'vue'

import Button from 'primevue/button'

import { fetchBench, seedFixture } from '../rehydrate/api.js'
import { BENCH_SIZES, benchCmd } from '../view/lessons.js'
import { formatBytes, formatCount } from '../view/format.js'

const emit = defineEmits(['measure'])

const state = ref(null)
const busy = ref(0)
const chosen = ref(BENCH_SIZES[0])

const ok = computed(() => (state.value?.kind === 'ok' ? state.value : null))
const broken = computed(() => (state.value?.kind === 'broken' ? state.value : null))

// The server's list wins. BENCH_SIZES is only what is drawn before the first
// answer arrives.
const sizes = computed(() => (ok.value?.sizes?.length ? ok.value.sizes : [...BENCH_SIZES]))
const cmd = computed(() => benchCmd(chosen.value))
const stream = computed(() => ok.value?.stream || 'ODOMETER_BENCH')

async function load() {
  state.value = await fetchBench()
  if (ok.value?.sizes?.length && !ok.value.sizes.includes(chosen.value)) {
    chosen.value = ok.value.sizes[0]
  }
}

// Reading what the fixture holds is a GET. It changes nothing, so it may run
// on mount — unlike the measurement itself, which may not.
onMounted(load)

async function seed(size) {
  if (busy.value) return
  busy.value = size
  state.value = await seedFixture(size)
  busy.value = 0
}
</script>

<template>
  <section
    class="bench"
    data-testid="bench-fixture"
  >
    <header>
      <h4>A log long enough to measure</h4>
      <p class="hint">
        Builds a throwaway stream, <code>{{ stream }}</code>, so the rebuild
        below has real work to do. It is separate from the demo's own log: a
        million events dropped into <code>ODOMETER</code> would bury every
        other tab. Seeding again replaces it, it does not add to it.
      </p>
    </header>

    <div class="controls">
      <Button
        v-for="s in sizes"
        :key="s"
        :label="formatCount(s)"
        size="small"
        :outlined="s !== chosen"
        :severity="s === chosen ? 'primary' : 'secondary'"
        :disabled="Boolean(busy)"
        :data-testid="`bench-size-${s}`"
        @click="chosen = s"
      />
      <Button
        label="Seed"
        icon="pi pi-database"
        size="small"
        :loading="busy === chosen"
        :disabled="Boolean(busy)"
        data-testid="bench-seed"
        @click="seed(chosen)"
      />
    </div>

    <p class="cmd">
      Or run it in a terminal — same code, same fixture:
      <code data-testid="bench-cmd">{{ cmd }}</code>
    </p>

    <p
      v-if="broken"
      class="broken"
      data-testid="bench-broken"
    >
      {{ broken.error }} — {{ broken.message }}
    </p>

    <template v-else-if="ok">
      <p
        v-if="!ok.exists"
        class="empty"
        data-testid="bench-empty"
      >
        Nothing is seeded yet. <code>{{ stream }}</code> holds
        {{ formatCount(ok.events) }} events, {{ formatBytes(ok.bytes) }} on
        disk. Press Seed, and then pick a fixture to rebuild.
      </p>

      <template v-else>
        <p
          class="holds"
          data-testid="bench-holds"
        >
          <code>{{ ok.stream }}</code> holds
          <b>{{ formatCount(ok.events) }}</b> events,
          <b>{{ formatBytes(ok.bytes) }}</b> on disk.
          <span
            v-if="ok.elapsedMs"
            class="ran"
          >seeded in {{ Math.round(ok.elapsedMs) }} ms</span>
        </p>

        <table data-testid="bench-fixtures">
          <thead>
            <tr>
              <th>vehicle</th>
              <th>events</th>
              <th>snapshot at</th>
              <th>tail left</th>
              <th />
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="f in ok.fixtures"
              :key="f.vehicle"
            >
              <td><code>{{ f.vehicle }}</code></td>
              <td>{{ formatCount(f.events) }}</td>
              <td>seq {{ formatCount(f.snapSeq) }}</td>
              <td>{{ formatCount(f.tailLeft) }}</td>
              <td>
                <Button
                  label="Measure this"
                  size="small"
                  severity="secondary"
                  outlined
                  :data-testid="`bench-measure-${f.vehicle}`"
                  @click="emit('measure', { source: 'bench', vehicle: f.vehicle })"
                />
              </td>
            </tr>
          </tbody>
        </table>

        <p class="hint">
          The snapshot stops short of the head on purpose. Each fixture leaves
          a tail of events after it, so the cheap side still has to replay
          something — a snapshot that were complete would measure a lookup, not
          a rehydration.
        </p>
      </template>
    </template>
  </section>
</template>

<style scoped>
.bench {
  padding: 12px 14px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
}

h4 {
  margin: 0 0 2px;
  font-size: 12.5px;
}

.hint,
.empty,
.broken,
.cmd,
.holds {
  margin: 8px 0 0;
  max-width: 80ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.hint {
  margin-top: 2px;
}

.broken {
  color: var(--d4-lost);
}

.controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 12px;
}

code {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11.5px;
}

.cmd code {
  color: var(--p-text-color);
}

.holds b {
  color: var(--p-text-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.ran {
  margin-left: 8px;
  color: var(--p-text-disabled-color);
  font-size: 11px;
}

table {
  margin-top: 10px;
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
</style>
