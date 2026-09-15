<script setup>
// One KV bucket, for one vehicle. Two of these sit side by side, and the whole
// argument of the demo is the difference between them:
//
//   odometer-write  what a RULE needs — status and plate, and nothing else.
//   odometer-read   what a QUESTION needs — the total, the trip count, the
//                   time of the last trip. None of it is in the write side,
//                   because no rule ever reads it.
//
// The panel draws whatever is in the document. It never computes a total from
// the log, because inventing the read model in the browser would hide the very
// thing the read side is for.
import { computed } from 'vue'

import { formatClock, formatCount, formatKm } from '../view/format.js'

const props = defineProps({
  side: { type: String, required: true }, // 'write' | 'read'
  bucket: { type: String, required: true },
  keyName: { type: String, default: '' },
  doc: { type: Object, default: null },
  head: { type: Number, default: 0 },
})

const behind = computed(() => Math.max(0, props.head - Number(props.doc?.lastSeq ?? 0)))

const fields = computed(() => {
  if (props.doc === null) return []
  if (props.side === 'write') {
    return [
      { term: 'state.status', value: props.doc.status || '(none)' },
      { term: 'state.plate', value: props.doc.plate || '(none)' },
      { term: 'lastSeq', value: String(props.doc.lastSeq) },
    ]
  }
  return [
    { term: 'totalKm', value: formatKm(props.doc.totalKm), big: true },
    { term: 'trips', value: formatCount(props.doc.trips) },
    { term: 'lastTripAt', value: formatClock(props.doc.lastTripAt) },
    { term: 'status', value: props.doc.status || '(none)' },
  ]
})
</script>

<template>
  <section
    class="bucket"
    :class="side"
    :data-testid="`bucket-${side}`"
  >
    <header>
      <p class="eyebrow">
        {{ bucket }} · key {{ keyName || '(none)' }}
      </p>
      <span
        v-if="doc"
        class="tag"
      >lastSeq {{ doc.lastSeq }} · {{ behind }} behind</span>
    </header>

    <dl v-if="doc">
      <template
        v-for="f in fields"
        :key="f.term"
      >
        <dt>{{ f.term }}</dt>
        <dd :class="{ big: f.big }">
          {{ f.value }}
        </dd>
      </template>
    </dl>
    <p
      v-else
      class="empty"
    >
      No key for this vehicle in {{ bucket }} yet. The projector for this side
      either has not reached the vehicle's first event, or is not running.
    </p>

    <p
      v-if="side === 'write' && doc"
      class="note"
    >
      The write side carries no total. A rule never reads one, so the aggregate
      never stores one.
    </p>
  </section>
</template>

<style scoped>
.bucket {
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

header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.eyebrow {
  margin: 0;
  overflow-wrap: anywhere;
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

dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 6px 18px;
  margin: 12px 0 0;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

dt {
  color: var(--p-text-disabled-color);
}

dd {
  margin: 0;
}

dd.big {
  font-size: 22px;
  line-height: 1;
  font-weight: 600;
}

.empty,
.note {
  margin: 12px 0 0;
  max-width: 48ch;
  color: var(--p-text-muted-color);
  font-size: 12px;
}
</style>
