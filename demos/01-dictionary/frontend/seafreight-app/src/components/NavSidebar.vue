<script setup>
import { useI18n } from 'vue-i18n'

defineProps({
  views: { type: Array, required: true }, // [{ key, label, icon }]
  modelValue: { type: String, required: true },
})
defineEmits(['update:modelValue'])

const { t } = useI18n()
</script>

<template>
  <nav class="nav-sidebar" :aria-label="t('nav.viewSelector')">
    <button
      v-for="view in views"
      :key="view.key"
      type="button"
      class="nav-item"
      :class="{ active: view.key === modelValue }"
      :aria-pressed="view.key === modelValue"
      @click="$emit('update:modelValue', view.key)"
    >
      <component :is="view.icon" class="nav-icon" />
      <span>{{ view.label }}</span>
    </button>
  </nav>
</template>

<style scoped>
.nav-sidebar {
  width: 160px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding-right: 0.625rem;
  border-right: 1px solid var(--lab-panel-border);
}
.nav-item {
  all: unset;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 8px 6px 6px;
  border-left: 2px solid transparent;
  border-radius: 4px;
  cursor: pointer;
  color: var(--p-text-muted-color);
  font-size: 13px;
}
.nav-icon {
  font-size: 15px;
  flex-shrink: 0;
}
.nav-item:hover {
  background: var(--lab-panel-bg);
  color: var(--p-text-color);
}
.nav-item.active {
  background: var(--lab-panel-bg);
  color: var(--p-text-color);
  border-left-color: var(--lab-accent);
}
.nav-item:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
</style>
