<script setup>
// Lesson 02 · the pool's own log — the seed group (04.9.3, decisions D1, D2).
//
// One group, one job: say what is in ODOMETER_POOL, and put it there.
//
// It sits ABOVE the tab strip, and that placement is the decision (D1). The
// log is the SAME log on all four tabs — Live, Starvation, Redelivery and
// Performance all fold it. A seed control inside one of those tabs would read
// as belonging to that tab's measurement, and a reader who re-seeded from
// Starvation would reasonably wonder whether Performance had been re-seeded
// too.
//
// Same shape as BenchFixture.vue on lesson 01, for the same reasons:
//
//   The log is named ONCE, in the header, with its count and its bytes. A
//   length is never shown on its own — the standing rule set by the user
//   2026-09-16. A count nobody can price is how 100 000 000 events sounds
//   reasonable until you learn it is 7.6 GB.
//
//   ONE primary button. The size list is the server's, and the default is the
//   one `cqrs pool -seed` defaults to, so the button and the bare command
//   build the same log.
//
//   The press is PRICED BEFORE IT HAPPENS. A reader who was never told the
//   cost is spending disk they did not agree to.
//
//   The button PRINTS ITS COMMANDS (D2). Every number this demo shows must be
//   reproducible in a terminal, and a log you cannot rebuild by hand is a
//   magic trick.
//
// Careful with the name: ODOMETER is a PREFIX of ODOMETER_POOL, so a
// half-copied name still reads as plausible. Nothing here may name the demo's
// own log; the spec holds it to /ODOMETER(?!_POOL)/.
import { computed, onMounted, ref, watch } from 'vue'

import Button from 'primevue/button'

import { dropPool, fetchPool, seedPool } from '../pool/api.js'
import { POOL_STREAM } from '../config.js'
import { POOL_BYTES_PER_EVENT, POOL_RM_CMD, POOL_SEED_CMD } from '../view/lessons.js'
import { formatBytes, formatCount } from '../view/format.js'

// The length is reported ONCE, here, and neither source can supply it alone.
//
// The stream watch sees an APPEND and never a deletion -- a stream that has
// been dropped simply stops sending, so the wire went on reporting 10 000
// events for a log that was gone. That was found in the browser, not by a
// spec. The GET sees the log exactly as it was when it was asked, and never
// moves afterwards.
//
// So the LATER of the two wins: a press refreshes from the GET, an event
// refreshes from the wire. One number on the page, and it is whichever of the
// two spoke most recently.
const props = defineProps({
  messages: { type: Number, default: 0 },
  bytes: { type: Number, default: 0 },
})

const emit = defineEmits(['state'])

const state = ref(null)
// What the header prints. Seeded from whichever source last spoke; see above.
const shown = ref({ events: props.messages, bytes: props.bytes })
watch(
  () => [props.messages, props.bytes],
  ([events, bytes]) => {
    shown.value = { events, bytes }
  },
)
// null when idle; 'seed' or 'rm' while one is in flight.
const busy = ref(null)

const ok = computed(() => (state.value?.kind === 'ok' ? state.value : null))
const broken = computed(() => (state.value?.kind === 'broken' ? state.value : null))

// The server's list wins. The first entry is the default `cqrs pool -seed`
// uses, so the button and the bare command build the same log.
const seedSize = computed(() => ok.value?.sizes?.[0] ?? 10_000)

// A measured cost beats a remembered one. Once the log holds anything, its own
// bytes-per-event is the honest divisor; on an empty log the recorded constant
// is the best this screen can do, and the `~` says so.
const perEvent = computed(() =>
  shown.value.events > 0 ? shown.value.bytes / shown.value.events : POOL_BYTES_PER_EVENT,
)
const costBytes = computed(() => Math.round(seedSize.value * perEvent.value))

// D8. The shim runs one pool at a time and refuses a second. The screen greys
// the button rather than warning about it — a control you can press and then
// be told off by is worse than one you cannot press.
const locked = computed(() => Boolean(busy.value) || Boolean(ok.value?.running))

async function load() {
  state.value = await fetchPool()
  if (state.value?.kind === 'ok') {
    shown.value = { events: state.value.events, bytes: state.value.bytes }
  }
  emit('state', state.value)
}

// Reading what the log holds is a GET. It changes nothing, so it may run on
// mount — unlike a seed, which may not.
onMounted(load)

async function press(what, call) {
  if (locked.value) return
  busy.value = what
  state.value = await call()
  if (state.value?.kind === 'ok') {
    shown.value = { events: state.value.events, bytes: state.value.bytes }
  }
  emit('state', state.value)
  busy.value = null
}

const seed = () => press('seed', () => seedPool(seedSize.value))
const drop = () => press('rm', () => dropPool())
</script>

<template>
  <section
    class="pool-fixture"
    data-testid="pool-fixture"
  >
    <header class="head">
      <h4>The log this lesson folds</h4>
      <p
        class="says"
        data-testid="pool-fixture-holds"
      >
        <template v-if="shown.events > 0">
          <code data-testid="pool-log-size">{{ POOL_STREAM }} ·
            {{ formatCount(shown.events) }} events ·
            {{ formatBytes(shown.bytes) }}</code>
        </template>
        <template v-else>
          <code data-testid="pool-log-size">{{ POOL_STREAM }}</code> holds
          <b>nothing yet</b> — seed it before you run anything.
        </template>
        <span
          v-if="ok?.running"
          class="ran"
          data-testid="pool-fixture-running"
        >{{ ok.runningWorkers }} workers are running now</span>
      </p>
    </header>

    <p
      v-if="broken"
      class="broken"
      data-testid="pool-fixture-broken"
    >
      {{ broken.error }} — {{ broken.message }}
    </p>

    <div class="controls">
      <Button
        label="Seed the log"
        icon="pi pi-database"
        size="small"
        :loading="busy === 'seed'"
        :disabled="locked"
        data-testid="pool-fixture-seed"
        @click="seed"
      />
      <Button
        label="Delete it"
        icon="pi pi-trash"
        size="small"
        severity="secondary"
        outlined
        :loading="busy === 'rm'"
        :disabled="locked"
        data-testid="pool-fixture-rm"
        @click="drop"
      />
      <span
        class="cost"
        data-testid="pool-fixture-cost"
      >
        {{ formatCount(seedSize) }} events · ~{{ formatBytes(costBytes) }} ·
        replaces what is there
      </span>
    </div>

    <div
      class="cmd"
      data-testid="pool-fixture-cmd"
    >
      <p>Or run them in a terminal — same code, same log:</p>
      <ul>
        <li><code>{{ POOL_SEED_CMD }}</code></li>
        <li><code>{{ POOL_RM_CMD }}</code></li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.pool-fixture {
  padding: 12px 14px;
  margin-bottom: 12px;
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

.broken,
.cmd,
.says,
.cost {
  margin: 0;
  max-width: 80ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.cmd {
  margin-top: 10px;
}

.cmd p {
  margin: 0;
}

/* One command per line. Two set as one run of mono text read as one long
   command, and a reader who copies the middle of it pastes something the
   binary rejects. */
.cmd ul {
  margin: 5px 0 0;
  padding: 0;
  list-style: none;
}

.cmd li {
  margin-top: 2px;
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

.ran {
  color: var(--d4-lost);
}
</style>
