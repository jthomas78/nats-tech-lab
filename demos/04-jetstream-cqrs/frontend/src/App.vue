<script setup>
// The rail selects a lesson; each lesson owns its local controls.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import Tag from 'primevue/tag'

import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

import { useCommands } from './commands/useCommands.js'
import CommandBar from './components/CommandBar.vue'
import PoolPanel from './components/PoolPanel.vue'
import StreamCqrsPanel from './components/StreamCqrsPanel.vue'
import {
  COMMAND_API,
  NATS_WS,
  POOL_KV,
  POOL_TRUTH_KV,
  POOL_WORKERS_KV,
  READ_KV,
  STREAM,
  WRITE_KV,
} from './config.js'
import { useOdometer } from './nats/useOdometer.js'
import { crumbFor, railSections } from './view/lessons.js'

const { status, error, head, messages, bytes, writes, reads, pool, poolWorkers, poolTruth, log, vehicles, lags, connect, disconnect } =
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

const view = ref('lesson-01')

const isLesson01 = computed(() => view.value === 'lesson-01')
const isLesson02 = computed(() => view.value === 'lesson-02')

const sections = railSections()
const crumb = computed(() => crumbFor(view.value))

// What the page is actually watching, not what it hoped to. The three pool
// buckets only exist once somebody has run `cqrs pool`, so naming them
// unconditionally would claim a subscription the page has not got.
const watching = computed(() => {
  const names = [WRITE_KV, READ_KV, STREAM]
  if (pool.size) names.push(POOL_KV)
  if (poolWorkers.size) names.push(POOL_WORKERS_KV)
  if (poolTruth.size) names.push(POOL_TRUTH_KV)
  return names.join(', ')
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
      <h1>{{ crumb.heading }}</h1>
    </header>

    <template v-if="isLesson01">
      <p
        v-if="error"
        class="panel err"
        data-testid="connection-error"
      >
        {{ error }}
      </p>

      <StreamCqrsPanel
        :vehicles="vehicles"
        :writes="writes"
        :reads="reads"
        :log-rows="log"
        :head="head"
        :messages="messages"
        :bytes="bytes"
        :lags="lags"
      >
        <template #write-door="{ vehicle }">
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
      :truth="poolTruth"
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
