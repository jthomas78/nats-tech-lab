<script setup>
// Lesson 01 — one log, two sides. Four tabs (plan section 10.6.1, D9).
//
// The four tabs are what left the rail. They are a real PrimeVue `Tabs`
// carrying `class="panel-tabs"` — the repo's one style for a top tab strip,
// the same as AboutPanel.vue. Never a chip or pill toggle for this role;
// chips are reserved for filters (shared/unifi-theme/LAYOUT.md).
//
//   Overview        the argument: how far behind each side is, and the two
//                   buckets side by side
//   ODOMETER        the log itself, newest first
//   odometer-write  every key a RULE is checked against
//   odometer-read   every key a QUESTION is answered from
//   Rehydrate       the demo's headline question, measured on demand
//
// D10 — the Overview tab must show BOTH buckets side by side. CLAUDE.md says
// "Two buckets, not one. The split is the demo", and the per-bucket tabs exist
// for browsing keys, not for making the argument. A change that leaves only
// one bucket on Overview has broken the demo, and StreamCqrsPanel.spec.js
// fails when it does.
//
// The tab strip sits flush and each tab's content carries the card, exactly as
// AboutPanel does. Wrapping the strip in a card puts the tablist on a
// background it does not expect and costs a pile of compensating overrides.
import { computed, ref } from 'vue'

import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'

import { READ_KV, STREAM, WRITE_KV } from '../config.js'
import { tabsFor } from '../view/lessons.js'
import BucketKeys from './BucketKeys.vue'
import BucketPanel from './BucketPanel.vue'
import EventLog from './EventLog.vue'
import LagLane from './LagLane.vue'
import RehydratePanel from './RehydratePanel.vue'

const props = defineProps({
  vehicle: { type: String, default: null },
  writeDoc: { type: Object, default: null },
  readDoc: { type: Object, default: null },
  writeRows: { type: Array, default: () => [] },
  readRows: { type: Array, default: () => [] },
  logRows: { type: Array, default: () => [] },
  head: { type: Number, default: 0 },
  positions: { type: Object, default: () => ({ head: 0, writeSeq: 0, readSeq: 0 }) },
  scope: { type: String, default: 'all vehicles' },
})

const TABS = tabsFor('lesson-01')
const tab = ref('overview')
const current = computed(() => TABS.find((t) => t.key === tab.value) ?? TABS[0])

const headLabel = computed(() =>
  props.vehicle ? `newest event for ${props.vehicle}` : 'stream head',
)
const logLabel = computed(() =>
  props.vehicle
    ? `${STREAM} · the events for ${props.vehicle}, up to seq ${props.positions.head}`
    : '',
)
</script>

<template>
  <section
    class="lesson"
    data-testid="stream-cqrs-panel"
  >
    <!-- The write door. App.vue owns WHAT it is; this panel owns only whether
         the tab you are looking at has any business showing one. A tab marked
         readOnly in lessons.js gets no door: Rehydrate measures a rebuild, and
         a row of write buttons above it invites you to change the thing being
         measured while it is being measured (04.7.15). -->
    <slot
      v-if="!current.readOnly"
      name="write-door"
    />

    <header>
      <p class="eyebrow">
        Lesson 01 · one log, two sides
      </p>
      <code class="cmd">{{ current.cmd }}</code>
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
        <!-- Overview — the argument. The lane first, then both buckets. -->
        <TabPanel value="overview">
          <LagLane
            :head="positions.head"
            :write-seq="positions.writeSeq"
            :read-seq="positions.readSeq"
            :scope="scope"
            :head-label="headLabel"
            :log-label="logLabel"
          />

          <!-- D10. One vehicle gets the two documents; the whole bucket gets
               the two key lists. Either way, two buckets, side by side. -->
          <div
            v-if="vehicle"
            class="cols"
            data-testid="overview-buckets"
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
          <div
            v-else
            class="cols"
            data-testid="overview-buckets"
          >
            <BucketKeys
              side="write"
              :bucket="WRITE_KV"
              :rows="writeRows"
              :head="head"
            />
            <BucketKeys
              side="read"
              :bucket="READ_KV"
              :rows="readRows"
              :head="head"
            />
          </div>
        </TabPanel>

        <!-- ODOMETER — the source of truth, row by row. -->
        <TabPanel value="stream">
          <EventLog
            :rows="logRows"
            :head="head"
            :write-seq="positions.writeSeq"
            :read-seq="positions.readSeq"
            :scope="scope"
            :show-vehicle="!vehicle"
          />
        </TabPanel>

        <!-- One bucket, browsed. The chosen vehicle's document first, then
             every key beside it, so one key can be read in context. -->
        <TabPanel value="write">
          <div
            v-if="vehicle"
            class="one"
          >
            <BucketPanel
              side="write"
              :bucket="WRITE_KV"
              :key-name="`vehicle.${vehicle}`"
              :doc="writeDoc"
              :head="positions.head"
            />
          </div>
          <BucketKeys
            side="write"
            :bucket="WRITE_KV"
            :rows="writeRows"
            :head="head"
          />
        </TabPanel>

        <TabPanel value="read">
          <div
            v-if="vehicle"
            class="one"
          >
            <BucketPanel
              side="read"
              :bucket="READ_KV"
              :key-name="`vehicle.${vehicle}`"
              :doc="readDoc"
              :head="positions.head"
            />
          </div>
          <BucketKeys
            side="read"
            :bucket="READ_KV"
            :rows="readRows"
            :head="head"
          />
        </TabPanel>

        <!-- Rehydrate — the headline question. It is the only tab that asks
             the write side to do work, so it never runs on its own. -->
        <TabPanel value="rehydrate">
          <RehydratePanel :vehicle="vehicle" />
        </TabPanel>
      </TabPanels>
    </Tabs>
  </section>
</template>

<style scoped>
.lesson {
  margin-top: 20px;
}

header {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.eyebrow {
  margin: 0;
}

/* The command that produces what the tab shows. Every tab has one, so nothing
   on this screen is a claim you cannot check in a terminal. */
.cmd {
  margin-left: auto;
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

/* Everything about the strip itself comes from `.panel-tabs` in
   shared/unifi-theme/unifi.css. Only the gap above it is this file's
   business — that is the point of there being one tab style in the repo. */
.panel-tabs {
  margin-top: 14px;
}

/* Side by side, because the point is the difference between the two. They
   stack below 1100px rather than squeezing the read side's big number. */
.cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-top: 20px;
  align-items: start;
}

@media (max-width: 1100px) {
  .cols {
    grid-template-columns: 1fr;
  }
}

/* BucketPanel has no top margin of its own — it is normally a grid cell. */
.one {
  margin-top: 20px;
}
</style>
