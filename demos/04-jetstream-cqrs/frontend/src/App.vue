<script setup>
// The screen proper. The rail picks one of two things to show.
//
// "How it works" is the demo explained — the README and the drawings, both
// read from the files that already hold them. Everything else is the demo
// running, in four pieces, in the order the demo argues:
//
//   1. the command bar   the only thing that writes, and it decides nothing
//   2. the lag lane      both sides drawn against the head of the log at once
//   3. the two buckets   the same vehicle, stored twice, for two different jobs
//   4. the log           the source of truth, with the lag told row by row
//
// The bar is first because the demo is done in that order: send a command,
// then watch it appear below. Still no rules here — every value on this page
// was decided by domain.go and folded into KV by a projector.
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
import BucketKeys from './components/BucketKeys.vue'
import BucketPanel from './components/BucketPanel.vue'
import CommandBar from './components/CommandBar.vue'
import EventLog from './components/EventLog.vue'
import LagLane from './components/LagLane.vue'
import { COMMAND_API, NATS_WS, READ_KV, STREAM, SUBJECT_PREFIX, WRITE_KV } from './config.js'
import { useOdometer } from './nats/useOdometer.js'

const { status, error, head, writes, reads, log, vehicles, lags, connect, disconnect } =
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

const view = ref('storage-stream')

// The guide is a page of its own, not a panel among the live ones. Somebody
// reading what CQRS means here should not have to read it past a lag lane
// that is moving.
const isAbout = computed(() => view.value === 'about')

// One vehicle, or none. `null` means a storage row is selected and every panel
// widens to the whole bucket.
const vehicle = computed(() =>
  view.value.startsWith('vehicle-') ? view.value.slice('vehicle-'.length) : null,
)
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
// lastSeq; for the whole bucket they are the furthest any key has been folded
// to, which is where the projector itself has reached.
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

// A retired vehicle says so in the rail; everything else shows how far the read
// side has folded it, which is the number the lane moves.
function badgeFor(id) {
  const w = writes.get(id)
  if (w?.status === 'retired') return 'retired'
  const r = reads.get(id)
  return String(r?.lastSeq ?? w?.lastSeq ?? 0)
}

const sections = computed(() => [
  {
    eyebrow: 'Guide',
    items: [{ key: 'about', label: 'How it works' }],
  },
  {
    eyebrow: 'Vehicles',
    items: vehicles.value.length
      ? vehicles.value.map((id) => ({ key: `vehicle-${id}`, label: id, badge: badgeFor(id) }))
      : [{ key: 'vehicles-none', label: 'No vehicles yet' }],
  },
  {
    eyebrow: 'Storage',
    items: [
      { key: 'storage-stream', label: STREAM, badge: String(head.value) },
      { key: 'storage-write', label: WRITE_KV, badge: String(writes.size) },
      { key: 'storage-read', label: READ_KV, badge: String(reads.size) },
    ],
  },
])

// A vehicle that disappears from both buckets must not leave the rail pointing
// at a row that is gone.
watch(sections, (next) => {
  const keys = next.flatMap((s) => s.items.map((i) => i.key))
  if (!keys.includes(view.value)) view.value = 'storage-stream'
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
      <b>{{ isAbout ? 'How it works' : (vehicle ?? 'JetStream + CQRS') }}</b>
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
        aria-label="Vehicles and storage"
      />
    </template>

    <header class="pagehead">
      <h1>{{ isAbout ? 'How it works' : (vehicle ?? 'Odometer') }}</h1>
      <Tag
        v-if="!isAbout && vehicleStatus"
        :severity="STATUS_SEVERITY[vehicleStatus] ?? 'info'"
        :value="vehicleStatus"
        data-testid="vehicle-status"
      />
      <Tag
        v-if="!isAbout && vehiclePlate"
        severity="secondary"
        :value="vehiclePlate"
      />
      <code
        v-if="!isAbout"
        class="subject"
      >{{ subject }}</code>
    </header>

    <AboutPanel v-if="isAbout" />

    <p
      v-if="!isAbout && !vehicle"
      class="sub"
    >
      One log, two sides. The write side checks a rule against the state the log
      already holds. The read side answers a question the log was never shaped
      for. Both trail the stream head, and the gap is what this screen exists to
      show. Pick a vehicle to narrow every panel to one key.
    </p>

    <p
      v-if="!isAbout && error"
      class="panel err"
      data-testid="connection-error"
    >
      {{ error }}
    </p>

    <CommandBar
      v-if="!isAbout"
      :vehicle="vehicle"
      :pending="pending"
      :outcomes="outcomes"
      @run="run"
    />

    <LagLane
      v-if="!isAbout"
      :head="positions.head"
      :write-seq="positions.writeSeq"
      :read-seq="positions.readSeq"
      :scope="scope"
      :head-label="vehicle ? `newest event for ${vehicle}` : 'stream head'"
      :log-label="vehicle ? `${STREAM} · the events for ${vehicle}, up to seq ${positions.head}` : ''"
    />

    <div
      v-if="!isAbout && vehicle"
      class="cols"
    >
      <BucketPanel
        side="write"
        :bucket="WRITE_KV"
        :key-name="`vehicle.${vehicle}`"
        :doc="writeDoc"
        :head="positions.head"
      />
      <BucketPanel
        side="read"
        :bucket="READ_KV"
        :key-name="`vehicle.${vehicle}`"
        :doc="readDoc"
        :head="positions.head"
      />
    </div>

    <BucketKeys
      v-if="view === 'storage-write'"

      side="write"
      :bucket="WRITE_KV"
      :rows="[...writes.values()]"
      :head="head"
    />
    <BucketKeys
      v-if="view === 'storage-read'"
      side="read"
      :bucket="READ_KV"
      :rows="[...reads.values()]"
      :head="head"
    />

    <EventLog
      v-if="!isAbout"
      :rows="rows"
      :head="head"
      :write-seq="positions.writeSeq"
      :read-seq="positions.readSeq"
      :scope="scope"
      :show-vehicle="!vehicle"
    />

    <footer class="wiring">
      <span>reads · {{ NATS_WS }}</span>
      <span>commands · {{ COMMAND_API }}</span>
      <span>watching · {{ WRITE_KV }}, {{ READ_KV }}, {{ STREAM }}</span>
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

/* Side by side, because the point is the difference between the two. They
   stack below 1100px rather than squeezing the read side's big number. */
.cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-top: 20px;
}

@media (max-width: 1100px) {
  .cols {
    grid-template-columns: 1fr;
  }
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
