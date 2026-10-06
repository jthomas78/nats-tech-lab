<script setup>
// One cluster: Request leadership here, its dark control, a one-line note on
// the last leadership request when it named this cluster, its three servers
// side by side, and its stream. The whole panel takes the warning border
// while the cluster is dark.
import { computed } from 'vue'

import { ROLE } from '../config.js'
import { hms, pendingOf } from '../usePlayground.js'

import DarkControl from './DarkControl.vue'
import ServerRow from './ServerRow.vue'
import StreamLane from './StreamLane.vue'

const props = defineProps({
  state: { type: Object, required: true },
  actions: { type: Object, required: true },
  cluster: { type: String, required: true },
})

const servers = computed(() => (props.state.snap?.servers ?? []).filter((s) => s.cluster === props.cluster))
const pids = computed(() => Object.fromEntries((props.state.snap?.rig?.servers ?? []).map((s) => [s.server, s.pid ?? 0])))
const phase = computed(() => (props.state.snap?.clusters ?? []).find((c) => c.cluster === props.cluster)?.phase)
const ready = computed(() => props.state.service.reachable === true && props.state.snap?.rig?.status === 'ready')
const leading = computed(() => pendingOf(props.state.snap, 'leadership'))

const note = computed(() => {
  const L = props.state.snap?.leadership
  if (!L || L.cluster !== props.cluster) return ''
  let s = `Leadership requested here at ${hms(L.at)}: `
  if (!L.reply) s += 'waiting for the reply.'
  else if (L.reply !== 'accepted') s += `${L.reply}.`
  else if (L.observed) s += `accepted, then observed ${L.observed.leader}, term ${L.observed.term}.`
  else if (leading.value?.cluster === props.cluster) s += 'accepted, watching for agreement.'
  else s += 'accepted, no new agreed leader observed.'
  return s
})
</script>

<template>
  <section
    class="cp"
    :class="{ hub: ROLE[cluster] === 'hub', isdark: phase === 'dark' }"
    :data-testid="`cp-${cluster}`"
  >
    <div class="cp-head">
      <div class="cp-title">
        <span class="cp-name">{{ cluster }}</span>
        <span class="cp-role">{{ ROLE[cluster] }}</span>
        <button
          class="btn sm"
          data-testid="leadership"
          :disabled="!ready || !!leading"
          @click="actions.requestLeadership(cluster)"
        >
          <template v-if="leading && leading.cluster === cluster">
            <span class="spin" />Requested
          </template>
          <template v-else>
            Request leadership here
          </template>
        </button>
      </div>
      <DarkControl
        :state="state"
        :actions="actions"
        :cluster="cluster"
      />
      <div
        v-if="note"
        class="leadnote"
        data-testid="leadnote"
      >
        {{ note }}
      </div>
    </div>
    <div class="srvs">
      <ServerRow
        v-for="s in servers"
        :key="s.server"
        :server="s"
        :pid="pids[s.server] || 0"
      />
    </div>
    <StreamLane
      :state="state"
      :actions="actions"
      :cluster="cluster"
    />
  </section>
</template>
