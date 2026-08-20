<script setup>
// Leaflet + OpenStreetMap overlay for BR-TP46-BR-TP50's operating areas.
//
// The GeoJSON is fetched at runtime from public/geo/ rather than imported,
// so it never enters the JS bundle: it is ~470 KB of geometry that most
// screens in this app never need.
//
// Deliberately NOT the authoritative control. The checklist beside it writes
// through the same handler; this view exists because "the whole Western
// Cape" is faster to click than to find in a list, not because a map is the
// only way to express coverage. Anything only achievable here would be
// unreachable without a pointer.
import 'leaflet/dist/leaflet.css'
import L from 'leaflet'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps({
  assigned: { type: Array, default: () => [] },
  busy: { type: Boolean, default: false },
})
const emit = defineEmits(['toggle'])

const mapEl = ref(null)
const failed = ref('')
let map = null
let layer = null

// Palette from shared/unifi-theme — the map is app UI, not a third-party
// widget, so it uses the same accent as every other selectable surface here.
const ACCENT = '#006FFF'
const STROKE = '#4A515B'

function styleFor(feature) {
  const isAssigned = props.assigned.includes(feature.properties.code)
  return {
    color: isAssigned ? ACCENT : STROKE,
    weight: isAssigned ? 2 : 1,
    fillColor: isAssigned ? ACCENT : '#1A1E23',
    fillOpacity: isAssigned ? 0.45 : 0.15,
  }
}

function restyle() {
  if (layer) layer.setStyle(styleFor)
}

onMounted(async () => {
  map = L.map(mapEl.value, { attributionControl: true, zoomControl: true })
  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; OpenStreetMap contributors',
    maxZoom: 10,
  }).addTo(map)

  try {
    const res = await fetch('/geo/operating-areas.geojson')
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
    const geo = await res.json()

    layer = L.geoJSON(geo, {
      style: styleFor,
      onEachFeature: (feature, lyr) => {
        const { code, name, country } = feature.properties
        lyr.bindTooltip(`${name} (${code})`, { sticky: true })
        lyr.on('click', () => {
          if (props.busy) return
          emit('toggle', code, country)
        })
      },
    }).addTo(map)
    map.fitBounds(layer.getBounds(), { padding: [12, 12] })
  } catch (e) {
    // The checklist still works without the map, so this degrades rather
    // than blocking the tab — but it says so instead of showing empty grey.
    failed.value = e.message
    map.setView([-26, 24], 4)
  }
})

watch(() => props.assigned, restyle, { deep: true })

// Leaflet measures its container once, at init. This component mounts inside
// a tab panel that is display:none until its tab is selected, so that first
// measurement is 0x0 and the map renders as a broken sliver. A ResizeObserver
// is used rather than watching the active tab because it fixes every cause of
// the same problem — tab switch, sidebar collapse, window resize — instead of
// only the one that was noticed.
let observer = null
onMounted(() => {
  if (typeof ResizeObserver === 'undefined' || !mapEl.value) return
  observer = new ResizeObserver(() => {
    if (!map) return
    map.invalidateSize()
    if (layer && layer.getBounds().isValid()) {
      map.fitBounds(layer.getBounds(), { padding: [12, 12] })
    }
  })
  observer.observe(mapEl.value)
})

onBeforeUnmount(() => {
  if (observer) observer.disconnect()
  observer = null
  if (map) map.remove()
  map = null
  layer = null
})
</script>

<template>
  <div class="area-map-wrap">
    <div
      ref="mapEl"
      class="area-map"
    />
    <p
      v-if="failed"
      class="area-map-failed"
    >
      <i class="pi pi-exclamation-triangle" />
      Map overlay unavailable ({{ failed }}). Use the region list — it is the same selection.
    </p>
  </div>
</template>

<style scoped>
.area-map-wrap {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.area-map {
  height: 420px;
  width: 100%;
  border: 1px solid var(--lab-border, #4a515b);
  border-radius: 6px;
  background: var(--lab-panel, #1a1e23);
}

.area-map-failed {
  margin: 0;
  font-size: 12px;
  color: var(--lab-warn, #9a7b1e);
}

/* Leaflet paints its own light chrome; keep it inside the app's palette. */
:deep(.leaflet-container) {
  background: var(--lab-panel, #1a1e23);
  font: inherit;
}

:deep(.leaflet-control-attribution),
:deep(.leaflet-tooltip) {
  background: var(--lab-panel, #1a1e23);
  color: var(--lab-text, #dee0e3);
  border-color: var(--lab-border, #4a515b);
}
</style>
