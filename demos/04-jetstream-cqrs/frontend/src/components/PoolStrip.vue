<script setup>
// The log near its head, drawn as one chip per event, with the pool's
// watermark on it — and the consumer the watermark belongs to underneath.
//
// This is lesson 02's hero drawing, and it exists because the lane cannot do
// this job. The lane says "worker 3 is 4 events behind". It has nowhere to put
// the one event that is about to be thrown away, and that event IS the lesson:
// a chip is ordinary blue one moment and dead red the next, and no log file,
// no metric and no error anywhere reports it.
//
// The bar under the strip is drawn full width on purpose. There is ONE durable
// consumer; the position is its property, not a worker's. A drawing that gave
// each worker its own position would teach the bug this demo is about.
//
// All the state maths is in view/strip.js and is specced there. This file only
// draws, and it draws nothing the page has not watched: no chip, no bar and no
// sentence here is written unless the head, KV odometer-pool or a worker's own
// heartbeat put it there.
import { computed } from 'vue'

import { POOL_KV, POOL_STREAM, POOL_SUBJECT_PREFIX } from '../config.js'
import { dropNote, headGap, stripChips } from '../view/strip.js'

const props = defineProps({
  head: { type: Number, default: 0 },
  // How far KV odometer-pool has been folded — the watermark.
  foldSeq: { type: Number, default: 0 },
  // workerRows(), so each row carries `worker` and what it is `holding`.
  rows: { type: Array, default: () => [] },
  // The consumer's own limits, as the pool was started with them. Blank when
  // the panel does not know: a guessed limit is worse than no limit.
  maxAckPending: { type: String, default: '' },
  ackWait: { type: String, default: '' },
})

const chips = computed(() =>
  stripChips({ head: props.head, foldSeq: props.foldSeq, workers: props.rows }),
)
const note = computed(() => dropNote(chips.value, props.foldSeq))

// The window follows the watermark, so in a backlog the strip does not reach
// the head. Saying how far short it stops keeps the drawing from implying the
// log ends where the chips do.
const gap = computed(() => headGap(chips.value, props.head))

const WORD = {
  folded: 'already folded',
  mark: 'the watermark',
  flight: 'in flight',
  doomed: 'in flight and about to be dropped',
  pending: 'stored, not handed out yet',
}

// Spoken aloud, the strip has to say the same thing the colours do. A screen
// reader gets the sentence; everyone else gets the chips.
const description = computed(() => {
  if (chips.value.length === 0) return 'The log is empty, so there is nothing to draw.'
  const parts = chips.value.map((c) => `#${c.seq} ${WORD[c.state]}`)
  const tail =
    gap.value === 0
      ? `to the head at #${chips.value[chips.value.length - 1].seq}`
      : `to #${chips.value[chips.value.length - 1].seq}, with ${gap.value} more events stored before the head at #${props.head}`
  return `The ODOMETER log from #${chips.value[0].seq} ${tail}: ${parts.join('; ')}. ${
    note.value ?? 'Nothing is about to be dropped.'
  }`
})
</script>

<template>
  <section
    class="strip"
    data-testid="pool-strip"
    role="img"
    :aria-label="description"
  >
    <p class="eyebrow">
      {{ POOL_STREAM }} · {{ POOL_SUBJECT_PREFIX }}.&gt;
    </p>

    <div
      class="chips"
      aria-hidden="true"
    >
      <span
        v-for="c in chips"
        :key="c.seq"
        class="chip"
        :class="c.state"
        :data-testid="`chip-${c.seq}`"
      >
        <b>#{{ c.seq }}</b>
        <i v-if="c.state === 'mark'">lastSeq</i>
        <i v-else-if="c.worker">worker {{ c.worker }}</i>
      </span>
      <span
        class="head"
        data-testid="strip-head"
      >{{ gap === 0 ? 'head' : `+${gap.toLocaleString('en-GB')} more · head #${head.toLocaleString('en-GB')}` }}</span>
    </div>

    <p
      v-if="note"
      class="drop"
      data-testid="strip-drop"
    >
      {{ note }}
    </p>

    <div
      class="consumer"
      data-testid="strip-consumer"
    >
      <span class="who">
        <b>{{ POOL_KV }}</b>
        <i>one durable consumer · the position belongs here, not to a worker</i>
      </span>
      <span
        v-if="maxAckPending || ackWait"
        class="limits"
      >
        <i v-if="maxAckPending">MaxAckPending {{ maxAckPending }}</i>
        <i v-if="ackWait">AckWait {{ ackWait }}</i>
      </span>
    </div>
  </section>
</template>

<style scoped>
.strip {
  padding: 14px 16px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
}

.eyebrow {
  margin: 0 0 10px;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  letter-spacing: 0.09em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
}

/* The chips wrap rather than shrink. A sequence number that has been squeezed
   until it is unreadable is not a smaller drawing, it is a broken one. */
.chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.chip {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1px;
  min-width: 86px;
  padding: 7px 10px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 5px;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.chip b {
  font-size: 13px;
  font-weight: 500;
  color: var(--p-text-color);
}

.chip i {
  font-size: 9px;
  font-style: normal;
  letter-spacing: 0.04em;
}

/* Folded is deliberately the quietest state on the strip. It is the past, and
   the past is the part of a log nobody has to watch. */
.chip.folded b {
  color: var(--p-text-disabled-color);
}

.chip.mark {
  border-color: var(--d4-read);
  background: color-mix(in srgb, var(--d4-read) 10%, transparent);
}

.chip.mark b,
.chip.mark i {
  color: var(--d4-read);
}

.chip.flight {
  border-color: var(--d4-write);
  background: color-mix(in srgb, var(--d4-write) 10%, transparent);
}

.chip.flight b,
.chip.flight i {
  color: var(--d4-write);
}

/* Same shape as `flight`, one colour apart. That is the point: nothing about
   the event changed, only where the watermark got to. */
.chip.doomed {
  border-color: var(--d4-lost);
  background: color-mix(in srgb, var(--d4-lost) 12%, transparent);
}

.chip.doomed b,
.chip.doomed i {
  color: var(--d4-lost);
}

.head {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  color: var(--p-text-disabled-color);
}

.drop {
  margin: 12px 0 0;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  color: var(--d4-lost);
}

.consumer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-top: 16px;
  padding: 10px 14px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
}

.who {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.who b {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 13px;
  font-weight: 500;
}

.who i,
.limits i {
  font-size: 10.5px;
  font-style: normal;
  color: var(--p-text-disabled-color);
}

.limits {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
}
</style>
