<script setup>
import { computed } from 'vue'

import { CATEGORY_ORDER, categoryLabel, DOMAIN_CATEGORIES } from '../categories'
import { useDictionaryStore } from '../stores/dictionary'
import { categoryIcon, typeIcon } from '../typeIcons'

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
  <template
    v-for="group in referenceGroups"
    :key="group.key"
  >
    <div class="nav-group">
      <div class="eyebrow">
        {{ group.label }}
      </div>
      <div
        v-for="t in group.types"
        :key="t.typeKey"
        class="nav-item"
        :class="{ active: store.activeView === 'items' && t.typeKey === store.selectedType }"
        @click="select(t.typeKey)"
      >
        <i :class="['pi', typeIcon(t.typeKey, t.category)]" />
        <span class="label-fade">{{ t.name }}</span>
        <span class="nav-badge">{{ store.typeCounts[t.typeKey] ?? 0 }}</span>
      </div>
    </div>
  </template>

  <div
    v-if="domainGroups.length > 0"
    class="nav-group"
  >
    <div class="eyebrow">
      Domain
    </div>
    <div
      v-for="group in domainGroups"
      :key="group.key"
      class="nav-item"
      :class="{ active: store.activeView === 'domain-category' && store.selectedCategory === group.key }"
      @click="store.showCategoryView(group.key)"
    >
      <i :class="['pi', categoryIcon(group.key)]" />
      <span class="label-fade">{{ group.label }}</span>
      <span class="nav-badge">{{ group.types.length }}</span>
    </div>
  </div>

  <p
    v-if="groups.length === 0"
    class="lab-muted label-fade"
  >
    No dictionary types registered yet.
  </p>

  <div class="nav-group">
    <div class="eyebrow">
      Tools
    </div>
    <div
      class="nav-item"
      :class="{ active: store.activeView === 'localization' }"
      @click="store.showLocalizationView()"
    >
      <i class="pi pi-language" />
      <span class="label-fade">Localization</span>
    </div>
    <div
      class="nav-item"
      :class="{ active: store.activeView === 'versioning' }"
      @click="store.showVersioningView()"
    >
      <i class="pi pi-history" />
      <span class="label-fade">Versioning</span>
    </div>
  </div>
</template>
