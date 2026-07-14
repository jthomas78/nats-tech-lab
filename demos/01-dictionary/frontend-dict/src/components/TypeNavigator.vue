<script setup>
import Badge from 'primevue/badge'
import { computed } from 'vue'

import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()

const types = computed(() =>
  [...store.types].sort((a, b) => a.typeKey.localeCompare(b.typeKey)),
)

function select(typeKey) {
  if (typeKey !== store.selectedType) store.selectType(typeKey)
}
</script>

<template>
  <div class="lab-panel type-navigator">
    <h3>Dictionary Types</h3>
    <ul class="type-list">
      <li
        v-for="t in types"
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
    <p
      v-if="types.length === 0"
      class="lab-muted"
    >
      No dictionary types registered yet.
    </p>
  </div>
</template>

<style scoped>
.type-navigator {
  min-width: 12rem;
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
  background: var(--lab-accent);
  color: #fff;
}
.type-name {
  font-size: 12px;
  text-transform: capitalize;
}
</style>
