<script setup>
/* The page body, shared by both of demo 04's entries (task 16d).

   Standalone, `App.vue` wraps this in `@ui-shell/AppShell`. Embedded in
   `lab-shell`, `plugin/OdometerRoute.vue` wraps it instead, because the shell
   owns the outer chrome there (BR-AS09). The panels, their tabs and their
   interaction are the same object in both — this component is a move, not a
   redesign.

   One prop, the object `useDemoState()` returns. Passing the state rather than
   twenty props keeps the two entries from drifting apart field by field. */
import CommandBar from './CommandBar.vue'
import PoolPanel from './PoolPanel.vue'
import StreamCqrsPanel from './StreamCqrsPanel.vue'

defineProps({
  state: { type: Object, required: true },
})
</script>

<template>
  <header class="pagehead">
    <h1>{{ state.crumb.heading }}</h1>
  </header>

  <template v-if="state.isLesson01">
    <p
      v-if="state.error"
      class="panel err"
      data-testid="connection-error"
    >
      {{ state.error }}
    </p>

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
