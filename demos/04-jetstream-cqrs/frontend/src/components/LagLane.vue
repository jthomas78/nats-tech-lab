<script setup>
// The lag lane — the one drawing this demo exists for.
//
// A terminal prints one line at a time, so it can tell you the write side is at
// sequence 124 and the read side at 126, but it cannot show you both distances
// at once. This does: one axis from sequence 1 to the head of the log, the log
// itself drawn full-width, and a marker per fold sitting where that fold has
// got to. The gap between a marker and the right-hand end is the lag.
//
// Phase 04.7 made it N markers instead of two. A worker pool has one row per
// worker and the panel does not know how many until it reads the heartbeat
// bucket, so the drawing takes a list of rows and draws whatever it is given.
// Passing `rows` is the general form; passing `writeSeq`/`readSeq` is the
// three-row CQRS form, which is the same drawing with a fixed list.
//
// All the maths is in view/lane.js and is specced there. This file only draws.
import { computed } from 'vue'

import { laneDescription, laneRows, lanePoints } from '../view/lane.js'

const props = defineProps({
  head: { type: Number, default: 0 },
  writeSeq: { type: Number, default: 0 },
  readSeq: { type: Number, default: 0 },
  // The general form: `{ id, text, seq, tone, kind }`. See view/lane.js.
  // When this is empty the component falls back to the write/log/read lane.
  rows: { type: Array, default: () => [] },
  // What the lane is measuring: one vehicle's key, or every key in the bucket.
  scope: { type: String, default: 'all vehicles' },
  // What the right-hand end of the axis is. For the whole bucket that is the
  // head of the log; for one vehicle it is that vehicle's newest event, which
  // is the only thing its own snapshot could possibly be behind.
  headLabel: { type: String, default: 'stream head' },
  logLabel: { type: String, default: '' },
  title: { type: String, default: '' },
})

const points = computed(() =>
  props.rows.length
    ? laneRows({ head: props.head, rows: props.rows })
    : lanePoints({
        head: props.head,
        writeSeq: props.writeSeq,
        readSeq: props.readSeq,
        logLabel: props.logLabel,
      }),
)

const description = computed(() => laneDescription(points.value, props.scope))
const folds = computed(() => points.value.rows.filter((row) => row.kind !== 'log'))
const caughtUp = computed(() => folds.value.every((row) => row.lag === 0))
const summary = computed(() =>
  caughtUp.value
    ? 'both caught up'
    : `${folds.value.map((row) => row.lag).join(' / ')} events behind`,
)
const heading = computed(() => props.title || `How far behind each side is — ${props.scope}`)
</script>

<template>
  <section
    class="lane"
    data-testid="lag-lane"
  >
    <header>
      <p class="eyebrow">
        {{ heading }}
      </p>
      <span
        class="tag"
        :class="{ ok: caughtUp }"
        data-testid="lag-summary"
      >{{ summary }}</span>
    </header>

    <svg
      :viewBox="`0 0 1000 ${points.height}`"
      role="img"
      :aria-label="description"
    >
      <!-- One row per fold, plus the log itself. The log is always full width:
           it IS the head, so it cannot be behind it. -->
      <g
        v-for="row in points.rows"
        :key="row.id"
      >
        <line
          class="seg"
          :class="row.tone"
          :x1="points.axis.x0"
          :y1="row.y"
          :x2="row.x"
          :y2="row.y"
        />
        <circle
          v-if="row.kind !== 'log'"
          class="dot"
          :class="row.tone"
          :cx="row.x"
          :cy="row.y"
          r="4.5"
        />
        <text
          class="marker"
          :class="row.kind === 'log' ? 'label' : row.tone"
          :x="row.kind === 'log' ? points.axis.x0 : row.label.x"
          :y="row.labelY"
          :text-anchor="row.kind === 'log' ? 'start' : row.label.anchor"
        >{{ row.text }}</text>
      </g>

      <!-- The axis. -->
      <line
        class="axis"
        :x1="points.axis.x0"
        :y1="points.axisY"
        :x2="points.axis.x1"
        :y2="points.axisY"
      />
      <line
        class="tick"
        :x1="points.axis.x0"
        :y1="points.axisY - 4"
        :x2="points.axis.x0"
        :y2="points.axisY + 4"
      />
      <line
        class="tick"
        :x1="points.axis.x1"
        :y1="points.axisY - 4"
        :x2="points.axis.x1"
        :y2="points.axisY + 4"
      />
      <text
        class="label"
        :x="points.axis.x0"
        :y="points.axisY + 14"
      >seq 1</text>
      <text
        class="label"
        :x="points.axis.x1"
        :y="points.axisY + 14"
        text-anchor="end"
      >seq {{ points.head }} — {{ headLabel }}</text>
    </svg>
  </section>
</template>

<style scoped>
.lane {
  margin-top: 20px;
  padding: 14px 16px 10px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
}

.lane header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.lane .eyebrow {
  margin: 0;
}

.tag {
  margin-left: auto;
  padding: 2px 8px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 999px;
  color: var(--p-text-muted-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  white-space: nowrap;
}

.tag.ok {
  color: var(--ok);
  border-color: color-mix(in srgb, var(--ok) 40%, var(--lab-panel-border));
}

svg {
  display: block;
  width: 100%;
  height: auto;
  margin-top: 10px;
}

.seg {
  stroke-width: 6;
  stroke-linecap: round;
  /* The markers slide as the folds catch up. Moving them instantly reads as a
     redraw; easing them reads as the lag closing, which is the point. */
  transition: x2 240ms ease-out;
}

.dot {
  transition: cx 240ms ease-out;
}

.marker {
  transition: x 240ms ease-out;
}

.seg.log {
  stroke: var(--p-text-disabled-color);
}

.seg.write,
.dot.write {
  stroke: var(--d4-write);
  fill: var(--d4-write);
}

/* A pool worker folds into a read model, so it is the read colour. It is not
   a third side of CQRS — see styles/sides.css. */
.seg.read,
.dot.read,
.seg.worker,
.dot.worker {
  stroke: var(--d4-read);
  fill: var(--d4-read);
}

/* A killed worker, or a fold that refused an event (BR-OD08). */
.seg.lost,
.dot.lost {
  stroke: var(--d4-lost);
  fill: var(--d4-lost);
}

.axis {
  stroke: var(--p-text-disabled-color);
  stroke-width: 1;
}

.tick {
  stroke: var(--p-text-disabled-color);
  stroke-width: 1;
}

.label,
.marker {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  fill: var(--p-text-disabled-color);
}

.marker.write {
  fill: var(--d4-write);
}

.marker.read,
.marker.worker {
  fill: var(--d4-read);
}

.marker.lost {
  fill: var(--d4-lost);
}

@media (prefers-reduced-motion: reduce) {
  .seg,
  .dot,
  .marker {
    transition: none;
  }
}
</style>
