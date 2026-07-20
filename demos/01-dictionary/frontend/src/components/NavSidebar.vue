<script setup>
// Grouped activity-bar sidebar. Same mechanics as frontend-port's NavSidebar
// (data-driven, v-model, accent left-border active state) plus frontend-dict's
// eyebrow grouping and mono count badges. Extend by adding to `sections` in the
// parent rather than introducing routing.
defineProps({
  // [{ eyebrow?: string, items: [{ key, label, icon, badge? }] }]
  sections: { type: Array, required: true },
  modelValue: { type: String, required: true },
  ariaLabel: { type: String, default: 'Views' },
})
defineEmits(['update:modelValue'])
</script>

<template>
  <nav class="nav-sidebar" :aria-label="ariaLabel">
    <template v-for="(section, i) in sections" :key="i">
      <div v-if="section.eyebrow" class="nav-eyebrow">{{ section.eyebrow }}</div>
      <button
        v-for="item in section.items"
        :key="item.key"
        type="button"
        class="nav-item"
        :class="{ active: item.key === modelValue }"
        :aria-pressed="item.key === modelValue"
        @click="$emit('update:modelValue', item.key)"
      >
        <component :is="item.icon" class="nav-icon" />
        <span class="nav-label">{{ item.label }}</span>
        <span v-if="item.badge != null" class="nav-badge">{{ item.badge }}</span>
      </button>
    </template>
  </nav>
</template>

<style scoped>
.nav-sidebar {
  width: 200px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding-right: 0.625rem;
  border-right: 1px solid var(--lab-panel-border);
}
.nav-eyebrow {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
  padding: 10px 6px 3px;
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
.nav-label {
  flex: 1;
}
.nav-badge {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  line-height: 14px;
  padding: 0 5px;
  border-radius: 3px;
  border: 1px solid var(--lab-panel-border);
  color: var(--p-text-muted-color);
  font-variant-numeric: tabular-nums;
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
.nav-item.active .nav-badge {
  color: var(--lab-accent);
  border-color: color-mix(in srgb, var(--lab-accent) 40%, transparent);
}
.nav-item:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
@media (max-width: 900px) {
  .nav-sidebar {
    width: auto;
    flex-direction: row;
    flex-wrap: wrap;
    border-right: none;
    border-bottom: 1px solid var(--lab-panel-border);
    padding: 0 0 0.5rem;
  }
  .nav-eyebrow {
    display: none;
  }
}
</style>
