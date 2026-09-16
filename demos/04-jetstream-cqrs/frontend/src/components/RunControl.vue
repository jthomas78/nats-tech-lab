<script setup>
// Lesson 02 · the one Run control, shared by all four tabs (04.9.5, D2, D4).
//
// One component, not four. Four buttons written separately drift apart: one
// gets a price and the others do not, one prints its commands and the others
// forget, one keeps working after the shim is already busy.
//
// It knows nothing about pools. It is handed a price, a list of commands and a
// percentage, and it emits `run` and `stop`. The tab owns the run; this owns
// the way a run is offered.
//
// D4 — the price is stated BEFORE the press. A screen that goes quiet for
// ninety seconds looks broken, and a reader who was never told the cost is
// spending time they did not agree to.
//
// D2 — the commands are a visible block under the button. Not a tooltip, not a
// footnote. A number this demo shows must be reproducible in a terminal.
//
// D12 — one bar for the WHOLE set, and it says which run it is on. The
// percentage comes from useRunProgress, which reads the workers bucket.

import Button from 'primevue/button'

const props = defineProps({
  // What the press costs, said plainly.
  runs: { type: Number, default: 1 },
  workers: { type: Number, default: 0 },
  seconds: { type: Number, default: 0 },

  // What it will type, in order.
  commands: { type: Array, default: () => [] },

  label: { type: String, default: 'Run it' },

  // Someone else holds the shim. The shim allows one run at a time, so a
  // second Run is refused rather than offered and then rejected (D8).
  locked: { type: Boolean, default: false },
  running: { type: Boolean, default: false },

  // D6 — Live alone runs open-ended, so Live alone can be stopped.
  stoppable: { type: Boolean, default: false },

  percent: { type: Number, default: 0 },
  note: { type: String, default: '' },
  stalled: { type: Boolean, default: false },
})

defineEmits(['run', 'stop'])

// "1 runs" is the kind of small wrongness that makes a reader stop trusting
// the numbers beside it.
const many = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`
</script>

<template>
  <div
    class="run-control"
    data-testid="run-control"
  >
    <div class="controls">
      <Button
        :label="props.label"
        icon="pi pi-play"
        size="small"
        :loading="props.running && !props.stoppable"
        :disabled="props.locked || props.running"
        data-testid="run-go"
        @click="$emit('run')"
      />
      <Button
        v-if="props.stoppable && props.running"
        label="Stop"
        icon="pi pi-stop"
        size="small"
        severity="secondary"
        outlined
        data-testid="run-stop"
        @click="$emit('stop')"
      />
      <span
        class="cost"
        data-testid="run-cost"
      >
        {{ many(props.workers, 'worker') }} ·
        {{ many(props.runs, 'run') }} ·
        about {{ props.seconds }} seconds
      </span>
    </div>

    <!-- The bar is only drawn while a set is in flight. A bar sitting at 0%
         on a page nobody has pressed reads as a run that failed to start. -->
    <div
      v-if="props.running"
      class="bar"
      data-testid="run-bar"
    >
      <div class="track">
        <div
          class="fill"
          :class="{ stuck: props.stalled }"
          :style="{ width: props.percent + '%' }"
        />
      </div>
      <p class="says">
        <b>{{ props.percent }}%</b>
        <span v-if="props.note"> — {{ props.note }}</span>
      </p>
      <p
        v-if="props.stalled"
        class="stuck-says"
        data-testid="run-stalled"
      >
        Nothing has moved for longer than the consumer's ack-wait. The workers
        are waiting for a redelivery, or they are not coming back.
      </p>
    </div>

    <div
      class="cmd"
      data-testid="run-cmd"
    >
      <p>The same run in a terminal:</p>
      <!-- One command per line. Two set as one run of mono text read as one
           long command, and a reader who copies the middle of it pastes
           something the binary rejects. -->
      <ul>
        <li
          v-for="c in props.commands"
          :key="c"
        >
          <code>{{ c }}</code>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.controls {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.cmd,
.cost,
.says,
.stuck-says {
  margin: 0;
  max-width: 80ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.bar {
  margin-top: 12px;
}

.track {
  height: 6px;
  border-radius: 3px;
  background: var(--lab-panel-border);
  overflow: hidden;
}

.fill {
  height: 100%;
  background: var(--p-primary-color);
  transition: width 240ms linear;
}

.fill.stuck {
  background: var(--d4-lost);
}

.says {
  margin-top: 5px;
}

.stuck-says {
  margin-top: 4px;
  color: var(--d4-lost);
}

.cmd {
  margin-top: 12px;
}

.cmd ul {
  margin: 5px 0 0;
  padding: 0;
  list-style: none;
}

.cmd li {
  margin-top: 2px;
}
</style>
