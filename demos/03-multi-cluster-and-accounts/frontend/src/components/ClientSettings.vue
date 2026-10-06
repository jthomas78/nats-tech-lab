<script setup>
// The two client settings. They live in the shared controller and travel in
// each command body; the service keeps none of its own.
import { TIMEOUT_CHOICES, VIA_CHOICES } from '../config.js'

const via = defineModel('via', { type: String, required: true })
const timeoutS = defineModel('timeoutS', { type: Number, required: true })
</script>

<template>
  <div
    class="client"
    data-testid="client"
  >
    <label>Client connection
      <select
        v-model="via"
        data-testid="via"
      >
        <option
          v-for="v in VIA_CHOICES"
          :key="v"
          :value="v"
        >{{ v === 'auto' ? 'Auto' : v }}</option>
      </select>
    </label>
    <label>Publish timeout
      <select
        v-model.number="timeoutS"
        data-testid="timeout"
      >
        <option
          v-for="t in TIMEOUT_CHOICES"
          :key="t"
          :value="t"
        >{{ t }} s</option>
      </select>
    </label>
    <span class="help">
      <template v-if="via === 'auto'">
        Auto: the destination's own cluster, then arb, then the other region. Running servers only.
      </template>
      <template v-else>
        Every command goes through a running server in {{ via }}. The destination stream is still the panel's.
      </template>
      The server used is shown on each result.
    </span>
  </div>
</template>
