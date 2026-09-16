<script setup>
// Lesson 01: explanation, live CQRS, and measured rehydration.
import { computed, ref, watch } from 'vue'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'

import { READ_KV, STREAM, SUBJECT_PREFIX, WRITE_KV } from '../config.js'
import { tabsFor, SHOWCASE_COMMANDS } from '../view/lessons.js'
import { formatBytes, formatCount } from '../view/format.js'
import AboutPanel from './AboutPanel.vue'
import BucketKeys from './BucketKeys.vue'
import BucketPanel from './BucketPanel.vue'
import EventLog from './EventLog.vue'
import LagLane from './LagLane.vue'
import RehydratePanel from './RehydratePanel.vue'
import VehiclePicker from './VehiclePicker.vue'

const props = defineProps({
  vehicles: { type: Array, default: () => [] },
  writes: { type: Object, default: () => new Map() },
  reads: { type: Object, default: () => new Map() },
  logRows: { type: Array, default: () => [] },
  head: { type: Number, default: 0 },
  messages: { type: Number, default: 0 },
  bytes: { type: Number, default: 0 },
  lags: { type: Object, default: () => ({ head: 0, writeSeq: 0, readSeq: 0 }) },
})
const TABS = tabsFor('lesson-01')
const tab = ref('overview')
const vehicle = ref(null)
const current = computed(() => TABS.find(t => t.key === tab.value) ?? TABS[0])
// `null` means all vehicles, and every panel widens to the whole bucket.
const scope = computed(() => vehicle.value ?? 'all vehicles')

const writeDoc = computed(() => (vehicle.value ? (props.writes.get(vehicle.value) ?? null) : null))
const readDoc = computed(() => (vehicle.value ? (props.reads.get(vehicle.value) ?? null) : null))

const rows = computed(() =>
  vehicle.value ? props.logRows.filter((r) => r.vehicle === vehicle.value) : props.logRows,
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
    : { head: props.lags.head, writeSeq: props.lags.writeSeq, readSeq: props.lags.readSeq },
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

const writeRows = computed(() => [...props.writes.values()])
const readRows = computed(() => [...props.reads.values()])

// Clear the local selection if the vehicle disappears from both buckets.
watch(() => props.vehicles, (next) => {
  if (vehicle.value && !next.includes(vehicle.value)) vehicle.value = null
})

const headLabel = computed(() => vehicle.value ? `newest event for ${vehicle.value}` : 'stream head')
const logLabel = computed(() => vehicle.value ? `${STREAM} · the events for ${vehicle.value}, up to seq ${positions.value.head}` : `${STREAM} · ${formatCount(props.messages)} events · ${formatBytes(props.bytes)} · the only source of truth`)
</script>

<template>
  <section
    class="lesson"
    data-testid="stream-cqrs-panel"
  >
    <header>
      <p class="eyebrow">
        Lesson 01 · one log, two sides
      </p>
      <code
        v-if="current.cmd"
        class="cmd"
      >{{ current.cmd }}</code>
    </header>
    <Tabs
      v-model:value="tab"
      class="panel-tabs"
    >
      <TabList>
        <Tab
          v-for="t in TABS"
          :key="t.key"
          :value="t.key"
          :data-testid="`lesson-01-tab-${t.key}`"
        >
          {{ t.label }}
        </Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="overview">
          <template v-if="tab === 'overview'">
            <section
              class="group summary"
              data-testid="lesson-summary"
            >
              <h3>Stream + CQRS — the short version</h3>
              <p>One log is the source of truth. Commands check the vehicle's history before appending an event to ODOMETER.</p>
              <p>Two independent consumers fold that log: odometer-write holds the snapshot used to check a rule; odometer-read holds the answer shaped for a question. Showcase lets you watch both trail the log.</p>
              <p>Performance measures the same rebuild from sequence 1 and from a snapshot plus its tail. How much does a snapshot buy you?</p>
              <nav aria-label="References">
                <a
                  href="https://docs.nats.io/learn/jetstream/"
                  target="_blank"
                  rel="noopener noreferrer"
                >NATS JetStream</a>
                <a
                  href="https://learn.microsoft.com/en-us/azure/architecture/patterns/cqrs"
                  target="_blank"
                  rel="noopener noreferrer"
                >CQRS pattern</a>
              </nav>
            </section>
            <AboutPanel />
          </template>
        </TabPanel>
        <TabPanel value="showcase">
          <div
            v-if="tab === 'showcase'"
            data-testid="showcase"
          >
            <div class="selection">
              <VehiclePicker
                v-model="vehicle"
                :vehicles="vehicles"
                :writes="writes"
                :reads="reads"
              />
              <Tag
                v-if="vehicleStatus"
                :severity="STATUS_SEVERITY[vehicleStatus] ?? 'info'"
                :value="vehicleStatus"
                data-testid="vehicle-status"
              />
              <Tag
                v-if="vehiclePlate"
                severity="secondary"
                :value="vehiclePlate"
              />
              <code class="cmd">{{ subject }}</code>
            </div>
            <div data-group="write">
              <slot
                name="write-door"
                :vehicle="vehicle"
              />
            </div>
            <div data-group="lag">
              <LagLane
                :head="positions.head"
                :write-seq="positions.writeSeq"
                :read-seq="positions.readSeq"
                :scope="scope"
                :head-label="headLabel"
                :log-label="logLabel"
              />
            </div>
            <section
              class="group"
              data-group="kv"
            >
              <header><h3>KV Stores</h3><code class="cmd">{{ SHOWCASE_COMMANDS.write }}</code><code class="cmd">{{ SHOWCASE_COMMANDS.read }}</code></header>
              <div class="cols">
                <div data-testid="showcase-write">
                  <BucketPanel
                    v-if="vehicle"
                    side="write"
                    :bucket="WRITE_KV"
                    :key-name="`vehicle.${vehicle}`"
                    :doc="writeDoc"
                    :head="positions.head"
                  />
                  <BucketKeys
                    side="write"
                    :bucket="WRITE_KV"
                    :rows="writeRows"
                    :head="head"
                  />
                </div>
                <div data-testid="showcase-read">
                  <BucketPanel
                    v-if="vehicle"
                    side="read"
                    :bucket="READ_KV"
                    :key-name="`vehicle.${vehicle}`"
                    :doc="readDoc"
                    :head="positions.head"
                  />
                  <BucketKeys
                    side="read"
                    :bucket="READ_KV"
                    :rows="readRows"
                    :head="head"
                  />
                </div>
              </div>
            </section>
            <section
              class="group"
              data-group="stream"
            >
              <header><h3>Stream · {{ STREAM }}</h3><code class="cmd">{{ SHOWCASE_COMMANDS.stream }}</code><code class="cmd">{{ SHOWCASE_COMMANDS.info }}</code></header>
              <p
                class="stream-size"
                data-testid="stream-size"
              >
                {{ formatCount(messages) }} events · {{ formatBytes(bytes) }} · whole stream
              </p>
              <EventLog
                :rows="rows"
                :head="head"
                :write-seq="positions.writeSeq"
                :read-seq="positions.readSeq"
                :scope="scope"
                :show-vehicle="!vehicle"
              />
            </section>
          </div>
        </TabPanel>
        <TabPanel value="performance">
          <KeepAlive>
            <RehydratePanel v-if="tab === 'performance'" />
          </KeepAlive>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </section>
</template>

<style scoped>
.lesson { margin-top: 20px; }
header, .selection, nav { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.eyebrow, h3 { margin: 0; }
h3 { font-size: 13px; }
.cmd { margin-left: auto; color: var(--p-text-disabled-color); font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace; font-size: 11px; }
.panel-tabs { margin-top: 14px; }
.group { margin-top: 20px; padding: 14px 16px; border: 1px solid var(--lab-panel-border); border-radius: 6px; background: var(--lab-panel-bg); }
.summary p { max-width: 90ch; color: var(--p-text-muted-color); }
a { color: var(--p-primary-color); }
.cols { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; margin-top: 14px; align-items: start; }
.stream-size { color: var(--p-text-muted-color); font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace; font-size: 12px; }
@media (max-width: 1100px) { .cols { grid-template-columns: 1fr; } }
</style>
