<script setup>
// Shared, data-driven sidebar nav list — extracted from admin's and
// seafreight-app's near-duplicate NavSidebar.vue components as part of the
// AppShell migration (see .claude/plans/AppShell-Extraction-Plan.md, "In
// scope"). Renders onto the shell's own `.nav-group`/`.eyebrow`/`.nav-item`/
// `.nav-badge`/`.label-fade` classes (styled globally by app-shell.css, so
// no local <style> block is needed here). Sections without an `eyebrow`
// render as a single ungrouped block — used by seafreight's flat view list.
defineProps({
  // [{ eyebrow?: string, items: [{ key, label, icon?, badge? }] }]
  sections: { type: Array, required: true },
  modelValue: { type: String, required: true },
  ariaLabel: { type: String, default: 'Views' },
})
defineEmits(['update:modelValue'])
</script>

<template>
  <nav :aria-label="ariaLabel">
    <div
      v-for="(section, i) in sections"
      :key="i"
      class="nav-group"
    >
      <div
        v-if="section.eyebrow"
        class="eyebrow"
      >
        {{ section.eyebrow }}
      </div>
      <button
        v-for="item in section.items"
        :key="item.key"
        type="button"
        class="nav-item"
        :class="{ active: item.key === modelValue }"
        :aria-pressed="item.key === modelValue"
        @click="$emit('update:modelValue', item.key)"
      >
        <component
          :is="item.icon"
          v-if="item.icon"
        />
        <span class="label-fade">{{ item.label }}</span>
        <span
          v-if="item.badge != null"
          class="nav-badge"
        >{{ item.badge }}</span>
      </button>
    </div>
  </nav>
</template>
