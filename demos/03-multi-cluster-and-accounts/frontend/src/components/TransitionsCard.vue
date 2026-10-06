<script setup>
// The last five changes, timed by the service from a confirmed change to
// the next agreed summary. The page computes nothing here; it renders the
// service's transition log.
import { computed } from 'vue'

import { hms, nsToS, secsSince } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
})

const LABEL = { freeze: 'Freeze', resume: 'Resume', leadership: 'Leadership requested in' }

const rows = computed(() => {
  const t = props.state.snap?.transitions ?? { closed: [] }
  const out = []
  if (t.open) out.push({ ...t.open, open: true })
  return out.concat(t.closed ?? []).slice(0, 5)
})

function label(tr) {
  return `${LABEL[tr.change.kind] ?? tr.change.kind} ${tr.change.cluster}`
}

function result(tr) {
  if (tr.open) return { pill: 'pend', text: `watching ${secsSince(props.state, tr.change.at)?.toFixed(1)} s` }
  switch (tr.outcome) {
    case 'agreed':
      return { strong: `agreed after ${nsToS(tr.elapsedNs).toFixed(1)} s` }
    case 'no_agreement':
      return { pill: 'warn', text: 'no agreement in 15 s' }
    case 'no_change':
      return { pill: '', text: 'no change in 15 s' }
    case 'superseded':
      return { plain: 'next change came first' }
    default:
      return { plain: tr.text || tr.outcome || '—' }
  }
}

function change(tr) {
  if (!tr.after || !tr.before) return null
  if (tr.after.leader === tr.before.leader && tr.after.term === tr.before.term) return { cls: '', text: 'unchanged' }
  return { cls: 'warn', text: tr.delta || `term ${tr.before.term} → ${tr.after.term}` }
}
</script>

<template>
  <div
    class="card"
    data-testid="transitions-card"
  >
    <h3>Transitions</h3>
    <div
      class="n"
      style="margin: -2px 0 4px"
    >
      Time from a confirmed change to the next agreed reading, polled every 0.5 s.
    </div>
    <div
      v-if="!rows.length"
      class="n"
    >
      Freeze, resume or request leadership. Each change is timed here, so repeated tries can be compared.
    </div>
    <div
      v-else
      class="tlist"
    >
      <div
        v-for="tr in rows"
        :key="`${tr.id}-${tr.open ? 'o' : 'c'}`"
        class="tr"
      >
        <div class="tr-h">
          <b>{{ label(tr) }}</b>
          <span class="n">{{ hms(tr.change.at) }}</span>
        </div>
        <div class="tr-b">
          <span
            v-if="result(tr).pill !== undefined"
            class="pill"
            :class="result(tr).pill"
          >{{ result(tr).text }}</span>
          <b v-else-if="result(tr).strong">{{ result(tr).strong }}</b>
          <span
            v-else
            class="n"
          >{{ result(tr).plain }}</span>
          <template v-if="tr.after">
            <span class="n">then</span>
            <span>{{ tr.after.leader }}, term {{ tr.after.term }}</span>
            <span
              v-if="change(tr)"
              class="pill"
              :class="change(tr).cls"
            >{{ change(tr).text }}</span>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>
