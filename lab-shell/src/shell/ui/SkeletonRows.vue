<script setup>
/* The route-panel skeleton: a header row plus fading body rows, sweeping.
   Shown while a route's remote is loading (BR-AS08). */
import './loading.css'

defineProps({
  rows: { type: Number, default: 5 },
  label: { type: String, default: 'Loading' },
})

const HEADER = [130, 70, 80, 56, 56, 110]
const BODY = [160, 64, 74, 44, 48, 140]
</script>

<template>
  <div
    class="skel-panel"
    role="status"
    :aria-label="label"
  >
    <p class="skel-caption">
      {{ label }}…
    </p>
    <div class="skel-head">
      <div
        v-for="(w, i) in HEADER"
        :key="`h${i}`"
        class="skel"
        :style="{ width: `${w}px` }"
      />
    </div>
    <div
      v-for="row in rows"
      :key="row"
      class="skel-row"
      :style="{ opacity: 1 - (row - 1) * 0.16 }"
    >
      <div
        v-for="(w, i) in BODY"
        :key="`b${row}-${i}`"
        class="skel"
        :class="[`delay-${row % 3}`]"
        :style="{ width: `${w - row * 4}px` }"
      />
    </div>
  </div>
</template>

<style scoped>
.skel-panel {
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  padding: 14px 16px;
}
.skel-caption {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--p-text-muted-color);
}
.skel-head {
  display: flex;
  gap: 24px;
  padding: 4px 8px 10px;
  border-bottom: 1px solid var(--lab-panel-border);
}
.skel-row {
  display: flex;
  gap: 24px;
  height: 24px;
  align-items: center;
  padding: 0 8px;
}
</style>
