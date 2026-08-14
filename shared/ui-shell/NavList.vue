<script setup>
// Shared, data-driven sidebar nav list — extracted from admin's and
// seafreight-app's near-duplicate NavSidebar.vue components as part of the
// AppShell migration (see .claude/plans/AppShell-Extraction-Plan.md, "In
// scope"). Renders onto the shell's own `.nav-group`/`.eyebrow`/`.nav-item`/
// `.nav-badge`/`.label-fade` classes (styled globally by app-shell.css, so
// no local <style> block is needed here). Sections without an `eyebrow`
// render as a single ungrouped block — used by seafreight's flat view list.
//
// Two nesting levels are supported, and an entry in `sections` is one of:
//
//   { eyebrow?: string, items: [...] }            a section (level 1 + 2)
//   { group: string, sections: [ <section> ] }     a collapsible group
//
// The `group` form adds the outer System/Platform banding — a clickable,
// accent-tinted banner over one or more ordinary sections. Both forms may be
// mixed in one array and render in array order, so an app can keep a flat
// ungrouped section (admin's "Overview") above its grouped ones. Collapse
// state lives here, not in the consuming app: like AppShell's own sidebar
// collapse it's presentation-only, and no app needs to read or drive it.
import { computed, ref } from 'vue'

const props = defineProps({
  // [{ eyebrow?, items: [{ key, label, icon?, badge? }] } | { group, sections }]
  sections: { type: Array, required: true },
  modelValue: { type: String, required: true },
  ariaLabel: { type: String, default: 'Views' },
})
defineEmits(['update:modelValue'])

// Normalize both entry forms to one shape so the item markup below is
// written once. An ungrouped entry becomes a nameless group of one section.
const groups = computed(() =>
  props.sections.map((entry, i) => ({
    id: entry.group ?? `ungrouped-${i}`,
    label: entry.group ?? null,
    sections: entry.group ? entry.sections : [entry],
  })),
)

// Collapsed rather than expanded groups are tracked, so a group added to
// `sections` later starts open without having to be registered anywhere.
const collapsedGroups = ref(new Set())

function toggleGroup(id) {
  const next = new Set(collapsedGroups.value)
  if (!next.delete(id)) next.add(id)
  collapsedGroups.value = next
}
</script>

<template>
  <nav :aria-label="ariaLabel">
    <template
      v-for="group in groups"
      :key="group.id"
    >
      <button
        v-if="group.label"
        type="button"
        class="eyebrow nav-group-toggle"
        :class="{ 'is-open': !collapsedGroups.has(group.id) }"
        :aria-expanded="!collapsedGroups.has(group.id)"
        @click="toggleGroup(group.id)"
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.4"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M9 6l6 6-6 6" />
        </svg>
        <span class="label-fade">{{ group.label }}</span>
      </button>

      <div
        class="nav-group-body"
        :class="{
          'is-grouped': group.label,
          'is-collapsed': group.label && collapsedGroups.has(group.id),
        }"
      >
        <div
          v-for="(section, i) in group.sections"
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
      </div>
    </template>
  </nav>
</template>
