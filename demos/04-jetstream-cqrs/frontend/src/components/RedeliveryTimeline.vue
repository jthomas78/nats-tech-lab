<script setup>
// One recorded redelivery, drawn on a time line.
//
// The shape is the mockup's Tab 3 hero (diagrams/worker-pool-ui-mockup.html),
// with one deliberate change. The mockup starts the line at the FIRST
// delivery — "t = 0s, #94 → worker 2, delivery 1". The pool does not know that
// moment: its kill clock starts when the worker is killed, and nothing else
// records when an event was first handed out. So this line starts at the
// silence. Everything on it is a number the run actually produced.
//
// Why a drawing at all, when the table above already has the figures: the
// table says the event was dropped, the line says WHEN the fold moved past
// it. The drop is not an accident at the end — it was already decided while
// the AckWait was still counting.

import { computed } from 'vue'

const props = defineProps({
  run: { type: Object, required: true },
})

const r = computed(() => props.run)

const label = computed(
  () =>
    `A time line of one event. At zero seconds worker ${r.value.killedWorker} goes silent while holding sequence ` +
    `${r.value.killSeq}, with no ack and no nak. AckWait counts for ${r.value.ackWait} while the other workers ` +
    `fold ${r.value.ranOn} more events. At ${r.value.waitedSeconds} seconds the server redelivers sequence ` +
    `${r.value.killSeq} to worker ${r.value.toWorker} as delivery ${r.value.delivery}. The watermark has already ` +
    `reached ${r.value.foldAt}, so the event is dropped and acked. The wait recovered nothing.`,
)
</script>

<template>
  <svg
    class="tl"
    viewBox="0 0 1080 180"
    role="img"
    :aria-label="label"
  >
    <path
      d="M160 62 L1040 62"
      stroke="var(--lab-panel-border)"
      stroke-width="1"
    />

    <!-- The silence. -->
    <circle
      cx="160"
      cy="62"
      r="5"
      fill="var(--d4-lost)"
    />
    <text
      x="160"
      y="42"
      class="t bad"
      text-anchor="middle"
    >t = 0s</text>
    <text
      x="160"
      y="86"
      class="l bad"
      text-anchor="middle"
    >worker {{ r.killedWorker }} goes silent</text>
    <text
      x="160"
      y="102"
      class="s"
      text-anchor="middle"
    >holding #{{ r.killSeq }} · no ack, no nak</text>

    <!-- The wait. Dashed, because nothing is happening to this event. -->
    <path
      d="M170 62 L604 62"
      stroke="#f0b429"
      stroke-width="1.5"
      stroke-dasharray="5 4"
    />
    <text
      x="387"
      y="42"
      class="t wait"
      text-anchor="middle"
    >AckWait counting · {{ r.ackWait }}</text>
    <text
      x="387"
      y="102"
      class="s"
      text-anchor="middle"
    >the other workers fold {{ r.ranOn }} more events</text>

    <!-- The redelivery. -->
    <circle
      cx="610"
      cy="62"
      r="5"
      fill="var(--d4-write)"
    />
    <text
      x="610"
      y="42"
      class="t w"
      text-anchor="middle"
    >t = {{ r.waitedSeconds }}s</text>
    <text
      x="610"
      y="86"
      class="l"
      text-anchor="middle"
    >#{{ r.killSeq }} → worker {{ r.toWorker }}</text>
    <text
      x="610"
      y="102"
      class="s"
      text-anchor="middle"
    >delivery {{ r.delivery }}</text>

    <!-- The end of it. -->
    <circle
      cx="1040"
      cy="62"
      r="5"
      fill="var(--d4-lost)"
    />
    <text
      x="1040"
      y="42"
      class="t bad"
      text-anchor="end"
    >t = {{ r.waitedSeconds }}s</text>
    <text
      x="1040"
      y="86"
      class="l bad"
      text-anchor="end"
    >#{{ r.killSeq }} ≤ lastSeq {{ r.foldAt }} · dropped, acked</text>
    <text
      x="1040"
      y="102"
      class="s"
      text-anchor="end"
    >not counted twice, and not counted once</text>

    <text
      x="8"
      y="146"
      class="s"
    >The drop is not decided at the end of the line. It is decided while AckWait is still counting, by the
      {{ r.ranOn }} events the other workers fold in the meantime.</text>
    <text
      x="8"
      y="166"
      class="s"
    >This is the case the watermark is built for — the SAME sequence arriving twice. It is not the out-of-order
      case on the Live tab.</text>
  </svg>
</template>

<style scoped>
/* Capped, not stretched. Left free the SVG fills a 1440px column and the
   labels, which are positioned for a 1080-unit line, drift apart and clip. */
.tl {
  display: block;
  width: 100%;
  max-width: 1080px;
  height: auto;
  margin: 12px 0 4px;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.t {
  font-size: 10.5px;
  fill: var(--p-text-disabled-color);
}

.t.bad {
  fill: var(--d4-lost);
}

.t.w {
  fill: var(--d4-write);
}

.t.wait {
  fill: #f0b429;
}

.l {
  font-size: 11.5px;
  fill: var(--p-text-color);
}

.l.bad {
  fill: var(--d4-lost);
}

.s {
  font-size: 10.5px;
  fill: var(--p-text-disabled-color);
}
</style>
