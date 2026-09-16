<script setup>
// The log, newest first. The only source of truth, drawn as a list.
//
// The last column is the lag told one row at a time: an event is "neither yet"
// until a projector folds it, then "read", then "write · read". Watching that
// column fill in from the bottom as the projectors catch up is the same story
// the lane above tells as a picture.
import { computed } from 'vue'

import { foldedInto, formatClock } from '../view/format.js'

const props = defineProps({
  rows: { type: Array, default: () => [] },
  head: { type: Number, default: 0 },
  writeSeq: { type: Number, default: 0 },
  readSeq: { type: Number, default: 0 },
  scope: { type: String, default: 'all vehicles' },
  // Hidden when one vehicle is selected — every row is that vehicle.
  showVehicle: { type: Boolean, default: true },
})

const marked = computed(() =>
  props.rows.map((row) => ({
    ...row,
    sides: foldedInto(row.seq, { writeSeq: props.writeSeq, readSeq: props.readSeq }),
  })),
)
</script>

<template>
  <section
    class="log"
    data-testid="log"
  >
    <header>
      <p class="eyebrow">
        The log, newest first — {{ scope }}
      </p>
      <span class="tag">{{ rows.length }} shown · head seq {{ head }}</span>
    </header>

    <table v-if="marked.length">
      <thead>
        <tr>
          <th class="right">
            Seq
          </th>
          <th v-if="showVehicle">
            Vehicle
          </th>
          <th>Event</th>
          <th>Payload</th>
          <th class="right">
            At
          </th>
          <th class="right">
            Folded into
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="row in marked"
          :key="row.seq"
          :class="{ fresh: row.sides.length === 0 }"
        >
          <td class="right seq">
            {{ row.seq }}
          </td>
          <td v-if="showVehicle">
            {{ row.vehicle }}
          </td>
          <td class="type">
            {{ row.type }}
          </td>
          <td>{{ row.detail }}</td>
          <td class="right at">
            {{ formatClock(row.at) }}
          </td>
          <td class="right folded">
            <span
              v-if="row.sides.length === 0"
              class="none"
            >neither yet</span>
            <template v-else>
              <span
                v-for="(s, i) in row.sides"
                :key="s"
                :class="s"
              >{{ i ? ' · ' : '' }}{{ s }}</span>
            </template>
          </td>
        </tr>
      </tbody>
    </table>
    <p
      v-else
      class="empty"
    >
      No events for {{ scope }} in the current tail window.
    </p>
  </section>
</template>

<style scoped>
.log {
  margin-top: 20px;
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
}

header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.eyebrow {
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

table {
  width: 100%;
  margin-top: 10px;
  border-collapse: collapse;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

th {
  padding: 0 14px 6px 0;
  border-bottom: 1px solid var(--lab-panel-border);
  color: var(--p-text-disabled-color);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-align: left;
  text-transform: uppercase;
}

td {
  padding: 4px 14px 4px 0;
  border-bottom: 1px solid color-mix(in srgb, var(--lab-panel-border) 50%, transparent);
}

th.right,
td.right {
  text-align: right;
}

/* Only the LAST column loses its right padding, so the table ends flush with
   the panel. Zeroing it on every .right cell instead put the right-aligned
   seq hard against the vehicle beside it, with no gap at all. */
th:last-child,
td:last-child {
  padding-right: 0;
}

.seq,
.at {
  color: var(--p-text-disabled-color);
}

.type {
  color: var(--p-primary-color);
}

/* An event neither projector has folded yet. It is the newest thing in the
   system and the reason the lane above is not flat. */
tr.fresh .seq {
  color: var(--p-text-color);
  font-weight: 600;
}

.folded .none {
  color: var(--p-text-disabled-color);
}

.folded .write {
  color: var(--d4-write);
}

.folded .read {
  color: var(--d4-read);
}

.empty {
  margin: 12px 0 0;
  color: var(--p-text-muted-color);
  font-size: 12px;
}
</style>
