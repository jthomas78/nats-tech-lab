<script setup>
// The lag lane — the one drawing this demo exists for.
//
// A terminal prints one line at a time, so it can tell you the write side is at
// sequence 124 and the read side at 126, but it cannot show you both distances
// at once. This does: one axis from sequence 1 to the head of the log, the log
// itself drawn full-width, and a marker per side sitting where that side has
// folded up to. The gap between a marker and the right-hand end is the lag.
//
// All the maths is in view/lane.js and is specced there. This file only draws.
import { computed } from 'vue'

import { READ_KV, STREAM, WRITE_KV } from '../config.js'
import { laneDescription, lanePoints } from '../view/lane.js'

const props = defineProps({
  head: { type: Number, default: 0 },
  writeSeq: { type: Number, default: 0 },
  readSeq: { type: Number, default: 0 },
  // What the lane is measuring: one vehicle's key, or every key in the bucket.
  scope: { type: String, default: 'all vehicles' },
  // What the right-hand end of the axis is. For the whole bucket that is the
  // head of the log; for one vehicle it is that vehicle's newest event, which
  // is the only thing its own snapshot could possibly be behind.
  headLabel: { type: String, default: 'stream head' },
  logLabel: { type: String, default: '' },
})

const points = computed(() =>
  lanePoints({ head: props.head, writeSeq: props.writeSeq, readSeq: props.readSeq }),
)
const description = computed(() => laneDescription(points.value, props.scope))
const caughtUp = computed(() => points.value.writeLag === 0 && points.value.readLag === 0)
const logLine = computed(
  () => props.logLabel || `${STREAM} · ${points.value.head} events, the only source of truth`,
)
</script>

<template>
  <section
    class="lane"
    data-testid="lag-lane"
  >
    <header>
      <p class="eyebrow">
        How far behind each side is — {{ scope }}
      </p>
      <span
        class="tag"
        :class="{ ok: caughtUp }"
        data-testid="lag-summary"
      >{{ caughtUp ? 'both caught up' : `${points.writeLag} / ${points.readLag} events behind` }}</span>
    </header>

    <svg
      viewBox="0 0 1000 104"
      role="img"
      :aria-label="description"
    >
      <!-- The write side, above the log. -->
      <line
        class="seg write"
        :x1="points.axis.x0"
        y1="18"
        :x2="points.write.x"
        y2="18"
      />
      <circle
        class="dot write"
        :cx="points.write.x"
        cy="18"
        r="4.5"
      />
      <text
        class="marker write"
        :x="points.write.label.x"
        y="9"
        :text-anchor="points.write.label.anchor"
      >{{ WRITE_KV }} · snapshot at {{ points.write.seq }}</text>

      <!-- The log itself. The only source of truth, so it is always full. -->
      <line
        class="seg log"
        :x1="points.axis.x0"
        y1="44"
        :x2="points.axis.x1"
        y2="44"
      />
      <text
        class="label"
        :x="points.axis.x0"
        y="34"
      >{{ logLine }}</text>

      <!-- The read side, below the log. -->
      <line
        class="seg read"
        :x1="points.axis.x0"
        y1="70"
        :x2="points.read.x"
        y2="70"
      />
      <circle
        class="dot read"
        :cx="points.read.x"
        cy="70"
        r="4.5"
      />
      <text
        class="marker read"
        :x="points.read.label.x"
        y="63"
        :text-anchor="points.read.label.anchor"
      >{{ READ_KV }} · projected to {{ points.read.seq }}</text>

      <!-- The axis. -->
      <line
        class="axis"
        :x1="points.axis.x0"
        y1="88"
        :x2="points.axis.x1"
        y2="88"
      />
      <line
        class="tick"
        :x1="points.axis.x0"
        y1="84"
        :x2="points.axis.x0"
        y2="92"
      />
      <line
        class="tick"
        :x1="points.axis.x1"
        y1="84"
        :x2="points.axis.x1"
        y2="92"
      />
      <text
        class="label"
        :x="points.axis.x0"
        y="102"
      >seq 1</text>
      <text
        class="label"
        :x="points.axis.x1"
        y="102"
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
  /* The markers slide as the projectors catch up. Moving them instantly reads
     as a redraw; easing them reads as the lag closing, which is the point. */
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

.seg.read,
.dot.read {
  stroke: var(--d4-read);
  fill: var(--d4-read);
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

.marker.read {
  fill: var(--d4-read);
}

@media (prefers-reduced-motion: reduce) {
  .seg,
  .dot,
  .marker {
    transition: none;
  }
}
</style>
