<script setup>
// The exercise 10 guide (rule 10: guidance never acts). Text only. It has
// no command buttons; the only button hides or shows it. A step is ticked
// only when the page sees the user do it: from the service's action events,
// or from a summary or phase the page itself saw.
import { computed, ref, watch } from 'vue'

import { commandKinds, hms } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
})

const open = ref(true)

// Two things are seen, not done: the first agreed summary, and the first
// moment both regions were dark together. They are kept here, because the
// service keeps no record of what the page saw.
const seen = ref({ agreed: null, bothDark: null })

watch(
  () => props.state.snap,
  (s) => {
    if (!s) return
    if (!seen.value.agreed && s.summary?.state === 'agreed') seen.value.agreed = s.at
    const ph = Object.fromEntries((s.clusters ?? []).map((c) => [c.cluster, c.phase]))
    if (!seen.value.bothDark && ph.za === 'dark' && ph.au === 'dark') seen.value.bothDark = s.at
  },
  { immediate: true },
)

// A new service session starts the guide again.
watch(
  () => props.state.session,
  () => {
    seen.value = { agreed: null, bothDark: null }
  },
)

const t = (iso) => Date.parse(iso)

const did = computed(() => {
  const kinds = Object.values(commandKinds(props.state.history))
  const all = (kind, cluster) => kinds.filter((k) => k.kind === kind && (cluster === undefined || k.cluster === cluster))
  const first = (kind, cluster) => all(kind, cluster)[0]?.at ?? null
  const after = (kind, cluster, iso) => all(kind, cluster).find((k) => t(k.at) > t(iso))?.at ?? null
  const latest = (...isos) => (isos.every(Boolean) ? isos.reduce((a, b) => (t(a) > t(b) ? a : b)) : null)

  const zaFreezes = all('freeze', 'za')
  const second = zaFreezes[1]?.at
  const bothDark = seen.value.bothDark
  return [
    seen.value.agreed,
    first('leadership', 'arb'),
    first('freeze', 'za'),
    latest(first('publish', 'arb'), first('publish', 'za'), first('probe')),
    zaFreezes[0] ? after('resume', 'za', zaFreezes[0].at) : null,
    first('verify', 'za'),
    second ? after('resume', 'za', second) : null,
    bothDark ? latest(after('resume', 'za', bothDark), after('resume', 'au', bothDark)) : null,
  ]
})

const STEPS = [
  'Start or attach the rig. Wait for the summary to say Agreed.',
  'Press Request leadership here on arb. Note the leader and term in the leadership card.',
  'Freeze za. Does the leader or the term change? Read the Transitions card.',
  'Publish new to ODOMETER_ARB and to ODOMETER_ZA. Then try the metadata operation.',
  'Resume za. Read the time to agreement, and whether the term moved.',
  'Verify storage on za. Retry any unknown message with the same ID.',
  'Repeat steps 3 to 5. Compare the transitions.',
  'Optional: freeze za and au together. Compare an arb publish with the metadata operation. Then restore all.',
]

const rows = computed(() => {
  let next = false
  return STEPS.map((text, i) => {
    const when = did.value[i]
    let cls = when ? 'done' : ''
    if (!when && !next) {
      cls = 'next'
      next = true
    }
    return { text, when, cls }
  })
})
</script>

<template>
  <div
    class="guide"
    data-testid="guide"
  >
    <div class="guide-h">
      <h2>Exercise 10 guide</h2>
      <span class="pill">optional</span>
      <button
        class="toggle"
        type="button"
        data-testid="guide-toggle"
        @click="open = !open"
      >
        {{ open ? 'Hide' : 'Show' }}
      </button>
    </div>
    <template v-if="open">
      <p class="gi">
        You do each step with the controls. A step is ticked only when the page sees you do it. Nothing here runs a step for you.
      </p>
      <ol class="steps">
        <li
          v-for="r in rows"
          :key="r.text"
          :class="r.cls"
        >
          <span>{{ r.text }}<span
            v-if="r.when"
            class="when"
          > {{ hms(r.when) }}</span></span>
        </li>
      </ol>
    </template>
  </div>
</template>
