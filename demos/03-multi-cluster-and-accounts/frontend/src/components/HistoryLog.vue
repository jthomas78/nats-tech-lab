<script setup>
// The session history, newest first: the service's actions, results and
// observations, the commands still pending (from the service's own pending
// list, with a running timer), and the POSTs it refused before any command
// began. Every command here ends (rule 9): its last line is a result, an
// error, a timeout or a cancellation.
import { computed, ref } from 'vue'

import { commandKinds, hms, nsToS, secsSince } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
})

const FILTERS = ['all', 'za', 'arb', 'au', 'errors']
const filter = ref('all')
const SHOWN = 300

// History events carry no cluster. A command's cluster comes from its action
// text; any other line is matched on a server name or a cluster word.
function clusterOfText(text) {
  const m = /\bt-(za|arb|au)-\d\b/.exec(text) ?? /\b(za|arb|au)\b/.exec(text)
  return m ? m[1] : ''
}

const rows = computed(() => {
  const kinds = commandKinds(props.state.history)
  const out = props.state.history.map((e) => {
    const kind = e.kind === 'result' ? e.end || 'result' : e.kind
    return {
      key: `h${e.seq}`,
      at: e.at,
      kind,
      text: e.text,
      cluster: (e.cmd && kinds[e.cmd]?.cluster) || clusterOfText(e.text),
      error: kind === 'error' || kind === 'timeout' || kind === 'cancelled',
    }
  })
  for (const p of props.state.snap?.pending ?? []) {
    out.push({
      key: `p${p.id}`,
      at: p.at,
      kind: 'pending',
      pending: p,
      text: p.text || `${p.kind}${p.cluster ? ` ${p.cluster}` : ''}`,
      cluster: p.cluster || '',
      error: false,
    })
  }
  for (const r of props.state.refusals) {
    out.push({
      key: `r${r.at}-${r.path}`,
      local: r.at,
      kind: 'refused',
      text: `${r.path}: ${r.message}`,
      cluster: clusterOfText(r.path),
      error: true,
    })
  }
  // Refusals carry local time; everything else server time. Order by server
  // time, with the refusals moved onto it by the measured skew.
  const when = (r) => (r.local !== undefined ? r.local + props.state.skewMs : Date.parse(r.at))
  out.sort((a, b) => when(b) - when(a))
  return out
})

const shown = computed(() => {
  const f = filter.value
  return rows.value
    .filter((r) => f === 'all' || (f === 'errors' ? r.error : r.cluster === f || r.cluster === ''))
    .slice(0, SHOWN)
})

function sub(r) {
  if (!r.pending) return ''
  const p = r.pending
  return `pending ${secsSince(props.state, p.at)?.toFixed(1)} s of ${nsToS(p.limitNs).toFixed(0)} s, ${p.lane} lane`
}

const time = (r) => (r.local !== undefined ? hms(r.local) : hms(r.at))
</script>

<template>
  <div
    class="side-h"
    data-testid="history-head"
  >
    <h2>History</h2>
    <div class="filters">
      <button
        v-for="f in FILTERS"
        :key="f"
        type="button"
        :class="{ on: filter === f }"
        :data-testid="`filter-${f}`"
        @click="filter = f"
      >
        {{ f }}
      </button>
    </div>
  </div>
  <div
    class="hist"
    data-testid="history"
  >
    <div
      v-for="r in shown"
      :key="r.key"
      class="ev"
      :class="{ err: r.error }"
      :data-kind="r.kind"
    >
      <span class="t">{{ time(r) }}</span>
      <span
        class="kind"
        :class="r.kind"
      ><span
        v-if="r.pending"
        class="pending-dot"
      />{{ r.kind }}</span>
      <span>
        <span class="txt">{{ r.text }}</span>
        <div
          v-if="sub(r)"
          class="sub"
        >{{ sub(r) }}</div>
      </span>
    </div>
    <div
      v-if="!shown.length"
      class="ev"
    >
      <span /><span />
      <span class="sub">Nothing yet. Start the rig, or attach to one.</span>
    </div>
  </div>
</template>
