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
//   { eyebrow?: string, id?: string, items: [...] }   a section (level 1 + 2)
//   { group: string, sections: [ <section> ] }        a collapsible group
//
// An item is a BUTTON by default and a real LINK when it carries `to`
// (D17-6). The two modes coexist in one list: a button-only consumer passes
// `modelValue` and reads `update:modelValue` exactly as before, and a link
// consumer passes neither, because the router already knows what is active
// and the browser already knows how to open a link in a new tab. A button
// that calls `router.push` would not satisfy that, which is why this is a
// real `<router-link>` and not a click handler.
//
// `section.id` is optional and falls back to the eyebrow text, so a consumer
// that never had one is untouched (amendment A5). It exists because identity
// must key off a stable id and never off a display label — a band renamed by
// its owner must not read as a different band.
//
// This component only RENDERS. Grouping, ordering and clash detection belong
// to the shell; nothing here knows what a plugin is.
//
// The `group` form adds the outer System/Platform banding — a clickable,
// accent-tinted banner over one or more ordinary sections. Both forms may be
// mixed in one array and render in array order, so an app can keep a flat
// ungrouped section (admin's "Overview") above its grouped ones. Collapse
// state lives here, not in the consuming app: like AppShell's own sidebar
// collapse it's presentation-only, and no app needs to read or drive it.
import { computed, ref } from 'vue'

const props = defineProps({
  // [{ eyebrow?, id?, items: [{ key, label, icon?, badge?, to? }] } | { group, sections }]
  sections: { type: Array, required: true },
  // Optional, because a link-mode list has no selection of its own to hold.
  modelValue: { type: String, default: null },
  ariaLabel: { type: String, default: 'Views' },
})
const emit = defineEmits(['update:modelValue'])

// One markup block for both modes, so a change to an item's look cannot
// reach one mode and miss the other.
const isLink = (item) => item.to != null
const tagFor = (item) => (isLink(item) ? 'router-link' : 'button')
const attrsFor = (item) =>
  isLink(item)
    ? { to: item.to, class: 'nav-item', activeClass: 'active' }
    : {
        type: 'button',
        class: ['nav-item', { active: item.key === props.modelValue }],
        'aria-pressed': item.key === props.modelValue,
      }

// A link is navigated by the router, so it announces no selection. Guarded
// here rather than in the template so the two modes cannot drift apart.
function pick(item) {
  if (isLink(item)) return
  emit('update:modelValue', item.key)
}

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
          :key="section.id ?? section.eyebrow ?? `section-${i}`"
          class="nav-group"
        >
          <div
            v-if="section.eyebrow"
            class="eyebrow"
          >
            {{ section.eyebrow }}
          </div>
          <component
            :is="tagFor(item)"
            v-for="item in section.items"
            :key="item.key"
            v-bind="attrsFor(item)"
            @click="pick(item)"
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
            <!-- A health or clash marker, drawn by whoever owns the meaning
                 of it. Empty for every consumer that passes no slot. -->
            <slot
              name="marker"
              :item="item"
            />
          </component>
        </div>
      </div>
    </template>
  </nav>
</template>
