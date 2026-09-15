<script setup>
// One KV bucket, every key in it. This is `nats kv ls` on the screen, and it is
// what the storage rows in the rail select.
//
// It exists because the two buckets hold DIFFERENT COLUMNS for the same
// vehicles. Seeing both lists is the fastest way to notice that the write side
// never stores a total.
import { computed } from 'vue'

import { maxSeq } from '../nats/model.js'
import { formatClock, formatCount, formatKm } from '../view/format.js'

const props = defineProps({
  side: { type: String, required: true }, // 'write' | 'read'
  bucket: { type: String, required: true },
  rows: { type: Array, default: () => [] },
  head: { type: Number, default: 0 },
})

const columns = computed(() =>
  props.side === 'write'
    ? [
        { key: 'status', label: 'status' },
        { key: 'plate', label: 'plate' },
      ]
    : [
        { key: 'totalKm', label: 'totalKm', align: 'right' },
        { key: 'trips', label: 'trips', align: 'right' },
        { key: 'lastTripAt', label: 'lastTripAt', align: 'right' },
      ],
)

function cell(row, key) {
  if (key === 'totalKm') return formatKm(row.totalKm)
  if (key === 'trips') return formatCount(row.trips)
  if (key === 'lastTripAt') return formatClock(row.lastTripAt)
  return row[key] || '(none)'
}

// Plain code-unit order, the same order the rail lists vehicles in. A locale
// compare would fold case and put truck-7 above V1 here but not there.
// One "behind" for the whole bucket, not one per key. A key's own lastSeq is
// the last event for THAT vehicle, so comparing each row to the stream head
// would report every parked vehicle as far behind. The projector's real
// position is the furthest it has folded anything to.
const folded = computed(() => maxSeq(props.rows))
const behind = computed(() => Math.max(0, props.head - folded.value))

const sorted = computed(() =>
  [...props.rows].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)),
)
</script>

<template>
  <section
    class="bucket"
    :class="side"
    :data-testid="`bucket-keys-${side}`"
  >
    <header>
      <p class="eyebrow">
        {{ bucket }} · {{ rows.length }} keys
      </p>
      <span
        class="tag"
        data-testid="bucket-behind"
      >folded to seq {{ folded }} · {{ behind }} behind</span>
    </header>

    <table v-if="sorted.length">
      <thead>
        <tr>
          <th>key</th>
          <th
            v-for="c in columns"
            :key="c.key"
            :class="c.align"
          >
            {{ c.label }}
          </th>
          <th class="right">
            lastSeq
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="row in sorted"
          :key="row.id"
        >
          <td class="key">
            vehicle.{{ row.id }}
          </td>
          <td
            v-for="c in columns"
            :key="c.key"
            :class="c.align"
          >
            {{ cell(row, c.key) }}
          </td>
          <td class="right seq">
            {{ row.lastSeq }}
          </td>
        </tr>
      </tbody>
    </table>
    <p
      v-else
      class="empty"
    >
      {{ bucket }} is empty. Run <code>cqrs {{ side === 'write' ? 'snapshotter' : 'projector' }}</code>
      to fill it — the bucket is built by folding the stream, so it stays empty
      until something folds.
    </p>
  </section>
</template>

<style scoped>
.bucket {
  margin-top: 20px;
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
  border-top: 2px solid var(--lab-panel-border);
}

.bucket.write {
  border-top-color: var(--d4-write);
}

.bucket.read {
  border-top-color: var(--d4-read);
}

.eyebrow {
  margin: 0;
}

header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.tag {
  margin-left: auto;
  padding: 2px 8px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 999px;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  white-space: nowrap;
}

.bucket.write .tag {
  color: var(--d4-write);
  border-color: color-mix(in srgb, var(--d4-write) 40%, var(--lab-panel-border));
}

.bucket.read .tag {
  color: var(--d4-read);
  border-color: color-mix(in srgb, var(--d4-read) 40%, var(--lab-panel-border));
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

/* Only the LAST column loses its right padding. On every .right cell it
   closed the gap between a right-aligned number and the column after it. */
th:last-child,
td:last-child {
  padding-right: 0;
}

.bucket.write .key {
  color: var(--d4-write);
}

.bucket.read .key {
  color: var(--d4-read);
}

.seq {
  color: var(--p-text-disabled-color);
}

.empty {
  margin: 12px 0 0;
  max-width: 60ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

code {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}
</style>
