<script setup>
// The service's meta summary, rendered and never recomputed. Leader and term
// are shown only when the state is Agreed (rule 1): any other state shows the
// views the readings hold, and the counts, never a guess.
import { computed } from 'vue'

import { FRESH_S } from '../config.js'

const props = defineProps({
  state: { type: Object, required: true },
})

const TITLE = {
  agreed: 'Agreed',
  agreed_no_leader: 'Agreed: no leader',
  disagreement: 'Disagreement',
  stale: 'Stale',
  insufficient: 'Insufficient readings',
}

const s = computed(() => props.state.snap?.summary ?? null)
const st = computed(() => (s.value && TITLE[s.value.state] ? s.value.state : 'unknown'))
const title = computed(() => TITLE[st.value] ?? 'No readings yet')
const r = computed(() => s.value?.readings ?? { fresh: 0, invalid: 0, noAnswer: 0, stale: 0, neverAnswered: 0 })
const p = computed(() => s.value?.processes ?? { running: 0, stopped: 0, gone: 0, unknown: 0 })
const views = computed(() => (st.value === 'agreed' ? [] : (s.value?.views ?? [])))
const fresh = FRESH_S
</script>

<template>
  <div
    class="summary"
    data-testid="summary"
  >
    <div>
      <div class="k">
        Meta group, from the monitors
      </div>
      <div
        class="v"
        :class="st"
        data-testid="summary-state"
      >
        {{ title }}
        <template v-if="st === 'agreed'">
          <span data-testid="summary-leader">{{ s.leader }}</span>
          <span class="pill">term {{ s.term }}</span>
        </template>
      </div>
      <div class="n">
        {{ s?.text || 'The service has no readings to summarise yet.' }}
      </div>
      <div
        v-if="views.length"
        class="views"
      >
        <div
          v-for="v in views"
          :key="`${v.leader}@${v.term}`"
        >
          <b>{{ v.leader ? `${v.leader}, term ${v.term}` : `no leader, term ${v.term}` }}</b>
          <span>from {{ v.servers.join(' ') }}</span>
        </div>
      </div>
    </div>
    <div>
      <div class="k">
        Readings (fresh under {{ fresh.toFixed(1) }} s)
      </div>
      <div class="counts">
        <b>{{ r.fresh }}</b><span>fresh</span>
        <b>{{ r.stale }}</b><span>stale, shown greyed</span>
        <b>{{ r.noAnswer }}</b><span>no answer at the last poll</span>
        <b>{{ r.neverAnswered }}</b><span>never answered</span>
        <template v-if="r.invalid">
          <b>{{ r.invalid }}</b><span>answered, but not readable</span>
        </template>
      </div>
    </div>
    <div>
      <div class="k">
        Processes, from the OS
      </div>
      <div class="counts">
        <b>{{ p.running }}</b><span>running</span>
        <b>{{ p.stopped }}</b><span>stopped (SIGSTOP)</span>
        <b>{{ p.gone }}</b><span>gone</span>
        <b>{{ p.unknown }}</b><span>unknown or unverified</span>
      </div>
    </div>
    <div class="derived">
      <div class="k">
        Quorum rule<span class="dtag">derived</span>
      </div>
      <div class="v">
        {{ s?.majority || 5 }} of 9
      </div>
      <div class="n">
        Arithmetic, not a reading. Unknown readings are never counted as zero.
      </div>
    </div>
  </div>
</template>
