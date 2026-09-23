<script setup>
/* The page body, shared by both of demo 04's entries (task 16d).

   Standalone, `App.vue` wraps this in `@ui-shell/AppShell`. Embedded in
   `lab-shell`, `plugin/OdometerRoute.vue` wraps it instead, because the shell
   owns the outer chrome there (BR-AS09). The panels, their tabs and their
   interaction are the same object in both — this component is a move, not a
   redesign.

   One prop, the object `useDemoState()` returns. Passing the state rather than
   twenty props keeps the two entries from drifting apart field by field. */
import { computed } from 'vue'

import DemoStatePanel from '@ui-shell/DemoStatePanel.vue'

import CommandBar from './CommandBar.vue'
import PoolPanel from './PoolPanel.vue'
import StreamCqrsPanel from './StreamCqrsPanel.vue'

const props = defineProps({
  state: { type: Object, required: true },
})

/* The demo's OWN running-state failure, drawn through the SAME component the
   shell uses before it mounts this plugin (BR-AS79, task 16e). The shell's
   check answered "available"; the connection dropped afterwards. That is the
   plugin's half of the split, and a reader should not have to learn a second
   layout to read it.

   The wording is written HERE, by the plugin, about its own connection. The
   shell writes its own in `demoReadinessText.js`. Only the presentation is
   shared. The raw client error is a chip, never spliced into a sentence. */
const BUSY = new Set(['connecting', 'reconnecting'])

const outage = computed(() => {
  const state = props.state
  if (!state.error) return null
  return {
    tone: BUSY.has(state.status) ? 'warn' : 'off',
    headline: BUSY.has(state.status)
      ? 'Reconnecting to NATS.'
      : 'Lost the connection to NATS.',
    detail:
      'The demo loaded, then the browser stopped receiving updates. What is on '
      + 'screen is the last state we saw, so it may be out of date.',
    items: [state.error],
    busy: BUSY.has(state.status),
  }
})
</script>

<template>
  <header class="pagehead">
    <h1>{{ state.crumb.heading }}</h1>
  </header>

  <!-- Demo-wide, not lesson-01's: a dropped connection stops both lessons
       updating, so it is reported wherever the reader happens to be. -->
  <DemoStatePanel
    v-if="outage"
    data-testid="connection-error"
    :tone="outage.tone"
    :headline="outage.headline"
    :detail="outage.detail"
    :items="outage.items"
    :busy="outage.busy"
    retry-label="Reconnect"
    @retry="state.connect"
  />

  <template v-if="state.isLesson01">
    <StreamCqrsPanel
      :vehicles="state.vehicles"
      :writes="state.writes"
      :reads="state.reads"
      :log-rows="state.log"
      :head="state.head"
      :messages="state.messages"
      :bytes="state.bytes"
      :lags="state.lags"
    >
      <template #write-door="{ vehicle }">
        <CommandBar
          :vehicle="vehicle"
          :pending="state.pending"
          :outcomes="state.outcomes"
          @run="state.run"
        />
      </template>
    </StreamCqrsPanel>
  </template>

  <!-- Lesson 02 is drawn from ODOMETER_POOL, so it is handed that log's
       head and size — never ODOMETER's. Passing lesson 01's head here made
       a caught-up pool look 84 000 events behind (04.8.9). -->
  <PoolPanel
    v-if="state.isLesson02"
    :head="state.poolHead"
    :messages="state.poolMessages"
    :bytes="state.poolBytes"
    :workers="state.poolWorkers"
    :pool="state.pool"
    :truth="state.poolTruth"
  />

  <footer class="wiring">
    <span>reads · {{ state.wiring.NATS_WS }}</span>
    <span>commands · {{ state.wiring.COMMAND_API }}</span>
    <span>watching · {{ state.watching }}</span>
  </footer>
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
