<script setup>
// The screen proper. The rail picks a LESSON; the lesson picks its own tabs.
//
// Phase 04.7 (D9) changed what the rail is. It used to list the vehicles and
// the three storage objects, so it grew every time the demo did. It is now a
// lesson index and it never grows:
//
//   GUIDE    How it works
//   LESSONS  01 · Stream + CQRS
//            02 · Scaling a consumer
//
// What left the rail did not disappear — it became a control inside the panel.
// The vehicles are a picker in the pagehead (VehiclePicker.vue); the storage
// objects are a tab strip inside lesson 01 (StreamCqrsPanel.vue). The rail now
// answers "what am I being taught", and the panel answers "what am I looking
// at" — two questions that were fighting over one list.
//
// Nothing an accepted command returns is written into the panels. The command
// gives back a sequence number; the screen learns what it MEANT from the read
// path, which is the whole point of the split.
//
// AppShell.vue is consumed, never re-implemented — the topbar, the rail and
// the collapse control belong to every app in the repo at once.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import Tag from 'primevue/tag'

import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

import { useCommands } from './commands/useCommands.js'
import AboutPanel from './components/AboutPanel.vue'
import CommandBar from './components/CommandBar.vue'
import PoolPanel from './components/PoolPanel.vue'
import StreamCqrsPanel from './components/StreamCqrsPanel.vue'
import VehiclePicker from './components/VehiclePicker.vue'
import {
  COMMAND_API,
  NATS_WS,
  POOL_KV,
  POOL_WORKERS_KV,
  READ_KV,
  STREAM,
  SUBJECT_PREFIX,
  WRITE_KV,
} from './config.js'
import { useOdometer } from './nats/useOdometer.js'
import { GUIDE, crumbFor, railSections } from './view/lessons.js'

const { status, error, head, writes, reads, pool, poolWorkers, log, vehicles, lags, connect, disconnect } =
  useOdometer()

onMounted(connect)
onBeforeUnmount(disconnect)

const { pending, outcomes, run } = useCommands()

// One label and one colour per connection state. A reconnect is reported, not
// hidden: nats-core retries on its own and the screen should say so.
const STATES = {
  idle: { label: 'Not connected', severity: 'danger' },
  connecting: { label: 'Connecting', severity: 'warn' },
  connected: { label: 'Watching', severity: 'success' },
  reconnecting: { label: 'Reconnecting', severity: 'warn' },
  closed: { label: 'Disconnected', severity: 'secondary' },
  error: { label: 'Not connected', severity: 'danger' },
}
const connection = computed(() => STATES[status.value] ?? STATES.idle)

// The rail's selection and the vehicle are now two separate things. They used
// to be one `view` string (`vehicle-truck-7`), which is why picking a vehicle
// and picking a lesson could not both be true at once.
const view = ref('lesson-01')
const vehicle = ref(null)

const isAbout = computed(() => view.value === GUIDE.key)
const isLesson01 = computed(() => view.value === 'lesson-01')
const isLesson02 = computed(() => view.value === 'lesson-02')

const sections = railSections()
const crumb = computed(() => crumbFor(view.value, vehicle.value))

// What the page is actually watching, not what it hoped to. The two pool
// buckets only exist once somebody has run `cqrs pool`, so naming them
// unconditionally would claim a subscription the page has not got.
const watching = computed(() => {
  const names = [WRITE_KV, READ_KV, STREAM]
  if (pool.size) names.push(POOL_KV)
  if (poolWorkers.size) names.push(POOL_WORKERS_KV)
  return names.join(', ')
})

// `null` means all vehicles, and every panel widens to the whole bucket.
const scope = computed(() => vehicle.value ?? 'all vehicles')

const writeDoc = computed(() => (vehicle.value ? (writes.get(vehicle.value) ?? null) : null))
const readDoc = computed(() => (vehicle.value ? (reads.get(vehicle.value) ?? null) : null))

const rows = computed(() =>
  vehicle.value ? log.value.filter((r) => r.vehicle === vehicle.value) : log.value,
)

// What a single key can be behind is its OWN newest event, not the head of the
// whole log. Measuring one vehicle against the stream head reports a vehicle
// that has been parked since sequence 1 as sixteen events behind, which is a
// lie about the projector. The log is newest first, so row 0 is the newest.
//
// A vehicle whose last event has fallen out of the tail window has no rows at
// all; then the furthest either side has folded is the newest sequence this
// screen can honestly claim to know about.
const vehicleHead = computed(() =>
  Math.max(
    rows.value.length ? rows.value[0].seq : 0,
    writeDoc.value?.lastSeq ?? 0,
    readDoc.value?.lastSeq ?? 0,
  ),
)

// Scoped to the selection. For one vehicle the two positions are that key's own
// lastSeq; for all vehicles they are the furthest any key has been folded to,
// which is where the projector itself has reached.
const positions = computed(() =>
  vehicle.value
    ? {
        head: vehicleHead.value,
        writeSeq: writeDoc.value?.lastSeq ?? 0,
        readSeq: readDoc.value?.lastSeq ?? 0,
      }
    : { head: lags.value.head, writeSeq: lags.value.writeSeq, readSeq: lags.value.readSeq },
)

const subject = computed(() =>
  vehicle.value ? `${SUBJECT_PREFIX}.${vehicle.value}.>` : `${SUBJECT_PREFIX}.>`,
)

// The state shown beside the vehicle name comes from the WRITE side, because
// that is the state a rule is checked against. The read side's copy can be
// older, and showing the older one here would misreport what a command would
// hit right now.
const vehicleStatus = computed(() => writeDoc.value?.status ?? readDoc.value?.status ?? '')
const vehiclePlate = computed(() => writeDoc.value?.plate ?? readDoc.value?.plate ?? '')

const STATUS_SEVERITY = { registered: 'success', retired: 'secondary' }

const writeRows = computed(() => [...writes.values()])
const readRows = computed(() => [...reads.values()])

// A vehicle that disappears from both buckets must not leave the picker
// pointing at a key that is gone. The rail cannot go stale any more — its three
// rows are fixed — so this watches the vehicle list instead of the rail.
watch(vehicles, (next) => {
  if (vehicle.value && !next.includes(vehicle.value)) vehicle.value = null
})
</script>

<template>
  <AppShell>
    <template #brand>
      <span class="dot">4</span>
      <span>Odometer</span>
    </template>

    <template #breadcrumb>
      <span>Demo 04</span>
      <span class="sep">/</span>
      <span>{{ crumb.lesson }}</span>
      <span class="sep">/</span>
      <b>{{ crumb.title }}</b>
    </template>

    <template #topbar-right>
      <Tag
        data-testid="connection-status"
        :severity="connection.severity"
        :value="connection.label"
      />
    </template>

    <template #sidebar>
      <NavList
        v-model="view"
        :sections="sections"
        aria-label="Lessons"
      />
    </template>

    <header class="pagehead">
      <h1>{{ isAbout ? GUIDE.label : crumb.title }}</h1>
      <Tag
        v-if="isLesson01 && vehicleStatus"
        :severity="STATUS_SEVERITY[vehicleStatus] ?? 'info'"
        :value="vehicleStatus"
        data-testid="vehicle-status"
      />
      <Tag
        v-if="isLesson01 && vehiclePlate"
        severity="secondary"
        :value="vehiclePlate"
      />
      <VehiclePicker
        v-if="isLesson01"
        v-model="vehicle"
        :vehicles="vehicles"
        :writes="writes"
        :reads="reads"
      />
      <code
        v-if="isLesson01"
        class="subject"
      >{{ subject }}</code>
    </header>

    <AboutPanel v-if="isAbout" />

    <template v-if="isLesson01">
      <p
        v-if="!vehicle"
        class="sub"
      >
        One log, two sides. The write side checks a rule against the state the
        log already holds. The read side answers a question the log was never
        shaped for. Both trail the stream head, and the gap is what this screen
        exists to show. Pick a vehicle to narrow every tab to one key.
      </p>

      <p
        v-if="error"
        class="panel err"
        data-testid="connection-error"
      >
        {{ error }}
      </p>

      <!-- The write door goes THROUGH the panel, not above it. This file still
           owns what the door is and what pressing it does; the panel owns
           whether the open tab should show one at all. Rehydrate is marked
           readOnly in lessons.js and gets no door (04.7.15). -->
      <StreamCqrsPanel
        :vehicle="vehicle"
        :write-doc="writeDoc"
        :read-doc="readDoc"
        :write-rows="writeRows"
        :read-rows="readRows"
        :log-rows="rows"
        :head="head"
        :positions="positions"
        :scope="scope"
      >
        <template #write-door>
          <CommandBar
            :vehicle="vehicle"
            :pending="pending"
            :outcomes="outcomes"
            @run="run"
          />
        </template>
      </StreamCqrsPanel>
    </template>

    <PoolPanel
      v-if="isLesson02"
      :head="head"
      :workers="poolWorkers"
      :pool="pool"
      :reads="reads"
    />

    <footer class="wiring">
      <span>reads · {{ NATS_WS }}</span>
      <span>commands · {{ COMMAND_API }}</span>
      <span>watching · {{ watching }}</span>
    </footer>
  </AppShell>
</template>

<style scoped>
/* Only what the shared theme and app-shell.css have no class for. The
   brandmark (.dot), the breadcrumb (.sep, b) and the eyebrow all come from
   app-shell.css — anything reusable belongs there, not here. */
.pagehead {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.pagehead h1 {
  margin: 0;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.subject {
  margin-left: auto;
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

.sub {
  max-width: 84ch;
  color: var(--p-text-muted-color);
}

.panel {
  margin-top: 20px;
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
}

.panel.err {
  border-color: var(--err);
  color: var(--err);
}

.wiring {
  display: flex;
  gap: 24px;
  flex-wrap: wrap;
  margin-top: 20px;
  padding-top: 12px;
  border-top: 1px solid var(--lab-panel-border);
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}
</style>
