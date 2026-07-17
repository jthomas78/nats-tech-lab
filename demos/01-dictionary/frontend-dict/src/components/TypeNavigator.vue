<script setup>
import Badge from 'primevue/badge'
import { computed } from 'vue'

import { CATEGORY_ORDER, categoryLabel, DOMAIN_CATEGORIES } from '../categories'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()

const groups = computed(() => {
  const byCategory = new Map()
  for (const t of store.types) {
    const key = t.category || 'standards'
    if (!byCategory.has(key)) byCategory.set(key, [])
    byCategory.get(key).push(t)
  }
  return CATEGORY_ORDER
    .filter((key) => byCategory.has(key))
    .map((key) => ({
      key,
      label: categoryLabel(key),
      types: byCategory.get(key).sort((a, b) => a.typeKey.localeCompare(b.typeKey)),
    }))
})

// Governance split (Phase 11.9): externally-owned reference data sits apart
// from the platform-owned Domain groups. The DOMAIN super-eyebrow below is a
// non-interactive label, as are the reference-data category eyebrows.
const referenceGroups = computed(() => groups.value.filter((g) => !DOMAIN_CATEGORIES.includes(g.key)))
const domainGroups = computed(() => groups.value.filter((g) => DOMAIN_CATEGORIES.includes(g.key)))

function select(typeKey) {
  if (typeKey !== store.selectedType) store.selectType(typeKey)
}
</script>

<template>
  <div class="lab-panel type-navigator">
    <h3>Dictionary Types</h3>
    <template
      v-for="group in referenceGroups"
      :key="group.key"
    >
      <div class="category-eyebrow static">
        {{ group.label }}
      </div>
      <ul class="type-list">
        <li
          v-for="t in group.types"
          :key="t.typeKey"
          :class="{ active: t.typeKey === store.selectedType }"
          @click="select(t.typeKey)"
        >
          <span class="type-name">{{ t.name }}</span>
          <Badge
            :value="store.typeCounts[t.typeKey] ?? 0"
            severity="secondary"
          />
        </li>
      </ul>
    </template>

    <template v-if="domainGroups.length > 0">
      <div class="nav-divider" />
      <div class="super-eyebrow">
        Domain
      </div>
      <ul class="type-list">
        <li
          v-for="group in domainGroups"
          :key="group.key"
          :class="{ active: store.activeView === 'domain-category' && store.selectedCategory === group.key }"
          @click="store.showCategoryView(group.key)"
        >
          <span class="type-name">{{ group.label }}</span>
        </li>
      </ul>
    </template>

    <p
      v-if="groups.length === 0"
      class="lab-muted"
    >
      No dictionary types registered yet.
    </p>

    <div class="nav-divider" />
    <ul class="type-list">
      <li
        class="localization-entry"
        :class="{ active: store.activeView === 'localization' }"
        @click="store.showLocalizationView()"
      >
        <span class="type-name">⚑ Localization</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.type-navigator {
  min-width: 12rem;
}
.super-eyebrow {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--lab-muted, #888);
  padding: 0.15rem 0.5rem;
  margin: 0 0 0.15rem;
}
.category-eyebrow {
  display: flex;
  align-items: center;
  gap: 0.3rem;
  width: 100%;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--lab-muted, #888);
  margin: 0.65rem 0 0.25rem;
  padding: 0.15rem 0.5rem;
  background: none;
  border: none;
  cursor: pointer;
  border-radius: 3px;
}
.category-eyebrow.static {
  cursor: default;
}
.category-eyebrow:first-of-type {
  margin-top: 0;
}
.type-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.type-list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.4rem 0.5rem;
  border-radius: 3px;
  cursor: pointer;
}
.type-list li:hover {
  background: var(--lab-disabled-bg);
}
.type-list li.active {
  background: var(--p-highlight-background);
  color: var(--p-highlight-color);
}
.type-name {
  font-size: 12px;
  text-transform: capitalize;
}
.nav-divider {
  height: 1px;
  background: var(--lab-disabled-bg);
  margin: 0.65rem 0;
}
.localization-entry .type-name {
  text-transform: none;
}
</style>
