<script setup lang="ts">
const props = defineProps<{
  status: 'approved' | 'proposed' | 'deferred' | 'completed'
}>()

const labels: Record<string, string> = {
  approved: 'Approved',
  proposed: 'Proposed',
  deferred: 'Deferred',
  completed: 'Completed',
}
</script>

<template>
  <span class="verdict-badge" :class="`verdict-badge--${props.status}`">
    {{ labels[props.status] ?? props.status }}
  </span>
</template>

<style scoped>
.verdict-badge {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.verdict-badge--approved,
.verdict-badge--completed {
  background: rgba(0, 111, 255, 0.16);
  color: var(--lab-accent);
}

.verdict-badge--proposed {
  background: rgba(154, 161, 169, 0.18);
  color: var(--vp-c-text-2);
}

.verdict-badge--deferred {
  background: rgba(219, 158, 0, 0.18);
  color: #c98a00;
}
</style>
