<script setup>
// The last "Request leadership here" (rule 5). The page reports the leader
// it then observed, which may be elsewhere. When it did land where asked,
// it says so, and says that nothing keeps it there.
import { computed } from 'vue'

import { clusterOf, hms, nsToS, pendingOf, secsSince } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
})

const L = computed(() => props.state.snap?.leadership ?? null)
const pending = computed(() => {
  const p = pendingOf(props.state.snap, 'leadership')
  return p && L.value && p.id === L.value.cmd ? p : null
})

const reply = computed(() => {
  const l = L.value
  if (!l.reply) {
    const s = secsSince(props.state, l.at)
    return { cls: 'pend', text: `waiting ${s?.toFixed(1)} s / 3 s` }
  }
  if (l.reply === 'accepted') return { cls: 'ok', text: 'accepted' }
  return { cls: 'err', text: l.reply }
})

const observed = computed(() => {
  const l = L.value
  if (l.reply !== 'accepted') return null
  if (l.observed) {
    const o = l.observed
    return { text: `${o.leader} in ${clusterOf(o.leader)}, term ${o.term}, ${nsToS(l.tookNs).toFixed(1)} s after the reply` }
  }
  if (pending.value) return { pill: 'pend', text: 'watching for agreement, up to 10 s' }
  return { text: 'no new agreed leader in 10 s' }
})

const landed = computed(() => L.value?.observed && clusterOf(L.value.observed.leader) === L.value.cluster)
</script>

<template>
  <div
    class="card"
    data-testid="leadership-card"
  >
    <h3>Last leadership request</h3>
    <div
      v-if="!L"
      class="n"
    >
      No request yet. Press "Request leadership here" on a cluster. The result stays here until the next request.
    </div>
    <template v-else>
      <div class="kv">
        <span>Requested</span>
        <span><b>{{ L.cluster }}</b> at {{ hms(L.at) }}{{ L.via ? `, via ${L.via} as $SYS` : '' }}</span>
        <span>Before</span>
        <span>{{ L.before ? `${L.before.leader}, term ${L.before.term}` : 'no agreement seen yet' }}</span>
        <span>Reply</span>
        <span><span
          class="pill"
          :class="reply.cls"
        >{{ reply.text }}</span></span>
        <span>Observed</span>
        <span v-if="!observed">—</span>
        <span v-else-if="observed.pill"><span
          class="pill"
          :class="observed.pill"
        >{{ observed.text }}</span></span>
        <span v-else>{{ observed.text }}</span>
      </div>
      <div
        v-if="landed"
        class="n"
        style="margin-top: 3px"
      >
        The leader is in the requested cluster now. Nothing keeps it there.
      </div>
    </template>
  </div>
</template>
