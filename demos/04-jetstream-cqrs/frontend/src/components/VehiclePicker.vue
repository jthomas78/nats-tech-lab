<script setup>
// What the rail's "Vehicles" group used to be (plan section 10.6.1, D9).
//
// The rail is a lesson index now and never grows, so the vehicle list had to
// become a control inside the panel. A picker is the honest shape for it: the
// list is data and changes while you watch. Each tab owns its selection:
// Showcase scopes the live views; Performance selects one aggregate to rebuild.
//
// "All vehicles" is a real option, not an empty state. Widening back to the
// whole bucket is half of what this control is for, and a clear button hides
// that behind an icon.
//
// The badge is what the rail carried: a retired vehicle says so, and every
// other vehicle shows how far the READ side has folded it — the number the lag
// lane moves. It is read-side because that is the number that visibly trails.
import { computed } from 'vue'

import Select from 'primevue/select'

// "All vehicles" needs a real option VALUE, and `null` cannot be one: PrimeVue
// reads a null model value as "nothing is selected" and falls back to the
// placeholder, so the picker came up blank instead of saying what it was
// showing. The sentinel is internal — this component still emits `null` for
// all vehicles, because that is what every panel above it already means by it.
const ALL = '__all__'

const props = defineProps({
  modelValue: { type: String, default: null },
  vehicles: { type: Array, default: () => [] },
  writes: { type: Object, default: () => new Map() },
  reads: { type: Object, default: () => new Map() },
})

const emit = defineEmits(['update:modelValue'])

const selected = computed(() => props.modelValue ?? ALL)

function pick(value) {
  emit('update:modelValue', value === ALL ? null : value)
}

function badgeFor(id) {
  const w = props.writes.get(id)
  if (w?.status === 'retired') return 'retired'
  const r = props.reads.get(id)
  return String(r?.lastSeq ?? w?.lastSeq ?? 0)
}

const options = computed(() => [
  { id: ALL, label: 'All vehicles', badge: String(props.vehicles.length) },
  ...props.vehicles.map((id) => ({ id, label: id, badge: badgeFor(id) })),
])
</script>

<template>
  <Select
    :model-value="selected"
    :options="options"
    option-label="label"
    option-value="id"
    data-testid="vehicle-picker"
    aria-label="Choose a vehicle for Showcase"
    class="picker"
    @update:model-value="pick"
  >
    <template #option="{ option }">
      <span class="row">
        <span class="name">{{ option.label }}</span>
        <span class="badge">{{ option.badge }}</span>
      </span>
    </template>
  </Select>
</template>

<style scoped>
/* Monospace, because every vehicle id on this screen is monospace — in the
   log, in the key names, in the buckets. The picker names the same thing. */
.picker {
  min-width: 15rem;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.badge {
  margin-left: auto;
  color: var(--p-text-disabled-color);
  font-size: 11px;
}
</style>
