<script setup>
// One server. Process state (from the OS) and monitor state (from its HTTP
// port) sit in two separate cells, because neither proves the other (rule 2).
// A monitor that does not answer is "no answer", never "down". A stale
// reading is greyed and says so (rule 3).
import { computed } from 'vue'

import { MONITOR_PORT } from '../config.js'
import { nsToS } from '../usePlayground.js'

const props = defineProps({
  server: { type: Object, required: true },
  pid: { type: Number, default: 0 },
})

const PROC = {
  running: ['running', 'ok'],
  stopped: ['stopped (T)', 'warn'],
  gone: ['not running', ''],
  unknown: ['unknown', ''],
}
const proc = computed(() => PROC[props.server.process] ?? PROC.unknown)

const reading = computed(() => props.server.reading ?? null)
const stale = computed(() => !!reading.value && !props.server.fresh)
const age = computed(() => nsToS(props.server.readingAgeNs))

const monitor = computed(() => {
  if (props.server.attemptFailed) return { dot: 'warn', text: 'no answer' }
  if (reading.value) return { dot: 'ok', text: 'answered' }
  return { dot: '', text: 'not polled' }
})

const ROLE = { leader: 'solid', candidate: 'cand' }
const role = computed(() => (reading.value?.state ?? '').toLowerCase())
</script>

<template>
  <div
    class="srv"
    :data-testid="`srv-${server.server}`"
  >
    <div class="srv-top">
      <span class="srv-n">{{ server.server }}</span>
      <span class="cell-k">pid {{ pid || '—' }}</span>
    </div>
    <div class="srv-pm">
      <span class="cell-k">Process</span>
      <span
        class="cell-v"
        data-testid="proc"
      ><span
        class="dot"
        :class="proc[1]"
      />{{ proc[0] }}</span>
      <span class="cell-k">Monitor</span>
      <span
        class="cell-v"
        data-testid="monitor"
      ><span
        class="dot"
        :class="monitor.dot"
      />{{ monitor.text }}</span>
    </div>
    <div
      v-if="!reading"
      class="belief"
      data-testid="belief"
    >
      <div
        class="line"
        style="color: var(--p-text-disabled-color)"
      >
        Never answered
      </div>
      <div class="age">
        port {{ MONITOR_PORT[server.server] }}
      </div>
    </div>
    <div
      v-else
      class="belief"
      :class="{ stale }"
      data-testid="belief"
    >
      <div class="line">
        <span
          v-if="reading.kind === 'ok' && reading.leader"
          class="who"
        >{{ reading.leader }}</span>
        <span
          v-else-if="reading.kind === 'no_leader' || reading.kind === 'ok'"
          class="who"
          style="color: var(--warn)"
        >no leader</span>
        <span
          v-else
          class="who"
          style="color: var(--warn)"
        >{{ reading.kind }}</span>
        <span
          v-if="reading.term"
          class="term"
        >term {{ reading.term }}</span>
        <span
          v-if="role"
          class="pill"
          :class="ROLE[role]"
        >{{ role }}</span>
      </div>
      <div
        class="age"
        :class="{ stale }"
      >
        {{ stale ? `last answer ${age.toFixed(1)} s ago, stale` : `read ${age.toFixed(1)} s ago` }}
      </div>
    </div>
  </div>
</template>
