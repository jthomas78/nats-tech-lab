<script setup lang="ts">
defineProps<{ text: string }>()
</script>

<template>
  <p class="eyebrow-label">{{ text }}</p>
</template>

<style scoped>
.eyebrow-label {
  margin: 0 0 4px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
</style>
