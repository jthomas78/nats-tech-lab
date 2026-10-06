<script setup>
// The cluster's stream: Publish new, Verify storage, and the service's
// ledger, one entry per message ID. A timeout is "outcome unknown" (rule 6);
// only a later Verify says present or absent, and only as of its own time.
// Retry same ID sends `retryOf`; Publish new never does.
import { computed } from 'vue'

import { STREAM } from '../config.js'
import { hms, pendingOf, secsSince } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
  actions: { type: Object, required: true },
  cluster: { type: String, required: true },
})

const RETRYABLE = new Set(['timed_out_unknown', 'refused_not_sent', 'cancelled_unknown'])

const entries = computed(() => [...(props.state.snap?.ledger?.[props.cluster] ?? [])].reverse())
const ready = computed(() => props.state.service.reachable === true && props.state.snap?.rig?.status === 'ready')
const publishing = computed(() => pendingOf(props.state.snap, 'publish', props.cluster))
const verifying = computed(() => pendingOf(props.state.snap, 'verify', props.cluster))

function last(e) {
  return e.attempts[e.attempts.length - 1]
}

function canRetry(e) {
  return ready.value && !publishing.value && RETRYABLE.has(last(e)?.outcome)
}

function pill(a) {
  switch (a.outcome) {
    case 'pending': {
      const s = secsSince(props.state, a.at)
      return { cls: 'pend', text: `waiting for ack ${s?.toFixed(1)} s / ${a.timeoutS} s` }
    }
    case 'acked':
      return { cls: '', text: `acked, seq ${a.seq}` }
    case 'acked_duplicate':
      return { cls: '', text: `acked as duplicate, seq ${a.seq}` }
    case 'refused_not_sent':
      return { cls: 'err', text: 'refused, not sent' }
    case 'timed_out_unknown':
      return { cls: 'unknown', text: `timed out at ${a.timeoutS} s, outcome unknown` }
    case 'cancelled_unknown':
      return { cls: 'unknown', text: 'cancelled, outcome unknown' }
    default:
      return { cls: 'err', text: a.text || a.outcome }
  }
}

function storage(e) {
  const v = e.storage
  if (!v) return null
  return v.present
    ? { cls: 'ok', text: `present, seq ${v.seq}, at ${hms(v.asOf)}` }
    : { cls: '', text: `absent at ${hms(v.asOf)}` }
}
</script>

<template>
  <div
    class="stream"
    :data-testid="`lane-${cluster}`"
  >
    <div class="stream-h">
      <b>{{ STREAM[cluster] }}</b>
      <span class="n">3 copies in {{ cluster }}</span>
      <span class="sp" />
      <button
        class="btn sm"
        data-testid="publish"
        :disabled="!ready || !!publishing"
        @click="actions.publish(cluster)"
      >
        <template v-if="publishing">
          <span class="spin" />Publishing
        </template>
        <template v-else>
          Publish new
        </template>
      </button>
      <button
        class="btn sm"
        data-testid="verify"
        :disabled="!ready || !!verifying || !entries.length"
        @click="actions.verify(cluster)"
      >
        <template v-if="verifying">
          <span class="spin" />Reading
        </template>
        <template v-else>
          Verify storage
        </template>
      </button>
    </div>
    <div class="ledger">
      <div
        v-if="!entries.length"
        class="empty"
      >
        No publishes in this session.
      </div>
      <div
        v-for="e in entries"
        :key="e.id"
        class="msg"
        :data-testid="`msg-${e.id}`"
      >
        <div class="msg-h">
          <span class="mono">{{ e.id }}</span>
          <span>{{ e.attempts.length > 1 ? `${e.attempts.length} attempts` : hms(e.attempts[0]?.at) }}</span>
          <button
            v-if="RETRYABLE.has(last(e)?.outcome)"
            class="btn xs"
            data-testid="retry"
            :disabled="!canRetry(e)"
            @click="actions.retry(cluster, e.id)"
          >
            Retry same ID
          </button>
        </div>
        <div class="msg-a">
          <template
            v-for="(a, i) in e.attempts"
            :key="a.cmd"
          >
            <span
              v-if="e.attempts.length > 1"
              class="n"
            >{{ i + 1 }}.</span>
            <span
              class="pill"
              :class="pill(a).cls"
              :title="a.text"
            >{{ pill(a).text }}</span>
            <span class="n">via {{ a.via || '—' }}</span>
          </template>
          <span
            v-if="storage(e)"
            class="pill"
            :class="storage(e).cls"
            data-testid="storage"
          >{{ storage(e).text }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
