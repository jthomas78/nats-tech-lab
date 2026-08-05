<script setup>
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import { getAccountsTopology, listAccounts } from '../api'
import SubjectPath from './SubjectPath.vue'

// Topology panel — the live export/import graph between accounts, read from
// each account's *current* resolver JWT (accounts-service's
// GET /api/accounts/topology, backed by Provisioner.LookupAccountClaims),
// not the bootstrap-time tenantImports() convention. Today's shape is
// always a star (every tenant imports the same fixed contract from
// PLATFORM; nothing imports from a tenant; SYS imports/exports nothing at
// all) — the layout below assumes that shape rather than a general graph,
// since building for a topology this codebase doesn't have would just be
// unjustified complexity.
const REFRESH_MS = 15000

const accounts = ref([])
const edges = ref([])
const loading = ref(true)
const errorMsg = ref('')
const selectedKey = ref(null)

async function refresh() {
  try {
    const [accs, edgeList] = await Promise.all([listAccounts(), getAccountsTopology()])
    accounts.value = accs
    edges.value = edgeList
    errorMsg.value = ''
  } catch (err) {
    errorMsg.value = err.message || 'Failed to load topology'
  } finally {
    loading.value = false
  }
}

let timer = null
onMounted(() => {
  refresh()
  timer = setInterval(refresh, REFRESH_MS)
})
onUnmounted(() => clearInterval(timer))

// ── Group edges into one line per (from, to) account pair ──────────────────
// A tenant imports ~7 individual subjects from PLATFORM; drawing 7
// overlapping animated lines per tenant would be noise, not information —
// one line per pair, weighted by how many subjects it carries, with the
// subject list available on click.
const edgeGroups = computed(() => {
  const byKey = new Map()
  for (const e of edges.value) {
    const key = `${e.fromAccount}→${e.toAccount}`
    if (!byKey.has(key)) byKey.set(key, { key, from: e.fromAccount, to: e.toAccount, items: [] })
    byKey.get(key).items.push(e)
  }
  return [...byKey.values()]
})

const selectedGroup = computed(() => edgeGroups.value.find((g) => g.key === selectedKey.value) ?? null)

function selectGroup(key) {
  selectedKey.value = selectedKey.value === key ? null : key
}

// ── Layout: hub (the account other accounts import from most) at center,
// every other known account arranged in a circle around it. An account
// with no edges at all (SYS today) still gets a node — dimmed — so its
// absence from the graph reads as "verified: nothing to show", not as a
// loading gap.
const W = 640
const H = 380
const CENTER = { x: W / 2, y: H / 2 - 10 }
const RADIUS = 145

const layout = computed(() => {
  const names = accounts.value.map((a) => a.name)
  if (names.length === 0) return { hub: null, spokes: [], positions: {} }

  const outDegree = new Map(names.map((n) => [n, 0]))
  for (const g of edgeGroups.value) outDegree.set(g.from, (outDegree.get(g.from) ?? 0) + 1)
  let hub = names[0]
  for (const n of names) if ((outDegree.get(n) ?? 0) > (outDegree.get(hub) ?? 0)) hub = n

  const spokes = names.filter((n) => n !== hub)
  const positions = { [hub]: { ...CENTER } }
  spokes.forEach((name, i) => {
    const angle = (2 * Math.PI * i) / spokes.length - Math.PI / 2
    positions[name] = {
      x: CENTER.x + RADIUS * Math.cos(angle),
      y: CENTER.y + RADIUS * Math.sin(angle),
    }
  })
  return { hub, spokes, positions }
})

function isConnected(name) {
  return edgeGroups.value.some((g) => g.from === name || g.to === name)
}

function nodeRadius(name) {
  return name === layout.value.hub ? 26 : 20
}

// Curve each line outward slightly rather than straight through the
// center — reads better when several spokes share the hub, and gives the
// flowing dash animation a visible direction of travel rather than a
// dead-straight line that looks static even while animating. Endpoints are
// trimmed back by each node's radius (plus a gap on the arrow end) so the
// line — and its arrowhead — stop clear of the circle instead of
// disappearing under it.
function pathFor(from, to) {
  const p1c = layout.value.positions[from]
  const p2c = layout.value.positions[to]
  if (!p1c || !p2c) return ''
  const dx0 = p2c.x - p1c.x
  const dy0 = p2c.y - p1c.y
  const len0 = Math.hypot(dx0, dy0) || 1
  const ux = dx0 / len0
  const uy = dy0 / len0
  const p1 = { x: p1c.x + ux * nodeRadius(from), y: p1c.y + uy * nodeRadius(from) }
  const p2 = { x: p2c.x - ux * (nodeRadius(to) + 5), y: p2c.y - uy * (nodeRadius(to) + 5) }

  const mx = (p1.x + p2.x) / 2
  const my = (p1.y + p2.y) / 2
  const dx = p2.x - p1.x
  const dy = p2.y - p1.y
  const len = Math.hypot(dx, dy) || 1
  const bow = 14
  const cx = mx + (-dy / len) * bow
  const cy = my + (dx / len) * bow
  return `M ${p1.x} ${p1.y} Q ${cx} ${cy} ${p2.x} ${p2.y}`
}

// service imports (rpc.* request/reply) read as the primary traffic —
// accent blue; stream imports (notify.*/evt.* broadcast) get the same
// green already used for "active" elsewhere in this app, so the color
// itself carries type information instead of every line looking identical.
function isStreamOnly(group) {
  return group.items.some((e) => e.type === 'stream') && !group.items.some((e) => e.type === 'service')
}
function groupColor(group) {
  return isStreamOnly(group) ? 'var(--p-green-500, #22c55e)' : 'var(--lab-accent)'
}
function markerFor(group) {
  return isStreamOnly(group) ? 'url(#topo-arrow-stream)' : 'url(#topo-arrow-service)'
}
function strokeWidth(group) {
  return Math.min(1.25 + group.items.length * 0.18, 2.75)
}

// ── Resizable splitter between the graph and the detail list ───────────────
const detailWidth = ref(320)
const bodyEl = ref(null)
let dragging = false

function startResize(e) {
  dragging = true
  window.addEventListener('mousemove', onResize)
  window.addEventListener('mouseup', stopResize)
  e.preventDefault()
}
function onResize(e) {
  if (!dragging || !bodyEl.value) return
  const rect = bodyEl.value.getBoundingClientRect()
  detailWidth.value = Math.min(Math.max(rect.right - e.clientX, 220), 520)
}
function stopResize() {
  dragging = false
  window.removeEventListener('mousemove', onResize)
  window.removeEventListener('mouseup', stopResize)
}
onUnmounted(stopResize)
</script>

<template>
  <div class="lab-panel topology-panel">
    <div class="panel-header">
      <span class="panel-title">Topology</span>
      <div class="header-actions">
        <Button icon="pi pi-refresh" text rounded size="small" :loading="loading" aria-label="Refresh" @click="refresh" />
      </div>
    </div>

    <p class="lab-muted description">
      Live export/import edges between accounts, read from each account's current resolver JWT — not a snapshot of the
      bootstrap script. Line thickness reflects how many subjects a pair shares; click a line for the list.
    </p>

    <p v-if="errorMsg" class="error-text">{{ errorMsg }}</p>

    <div class="topology-body" ref="bodyEl">
      <svg :viewBox="`0 0 ${W} ${H}`" class="topology-svg" role="img" aria-label="Account export and import graph">
        <defs>
          <marker id="topo-arrow-service" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9" markerHeight="9" markerUnits="userSpaceOnUse" orient="auto-start-reverse">
            <path d="M0 0L10 5L0 10z" fill="var(--lab-accent)" />
          </marker>
          <marker id="topo-arrow-stream" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9" markerHeight="9" markerUnits="userSpaceOnUse" orient="auto-start-reverse">
            <path d="M0 0L10 5L0 10z" fill="var(--p-green-500, #22c55e)" />
          </marker>
        </defs>

        <path
          v-for="g in edgeGroups"
          :key="g.key"
          :d="pathFor(g.from, g.to)"
          fill="none"
          :stroke="groupColor(g)"
          :stroke-width="strokeWidth(g)"
          stroke-linecap="round"
          :marker-end="markerFor(g)"
          class="topo-edge"
          :class="{ selected: selectedKey === g.key }"
          @click="selectGroup(g.key)"
        />

        <g v-for="name in accounts.map((a) => a.name)" :key="name" class="topo-node" :class="{ hub: name === layout.hub, dim: !isConnected(name) && name !== layout.hub }">
          <circle
            :cx="layout.positions[name]?.x"
            :cy="layout.positions[name]?.y"
            :r="name === layout.hub ? 26 : 20"
          />
          <text :x="layout.positions[name]?.x" :y="(layout.positions[name]?.y ?? 0) + nodeRadius(name) + 26" text-anchor="middle" class="topo-label">
            {{ name }}
          </text>
        </g>
      </svg>

      <div class="resize-handle" @mousedown="startResize" role="separator" aria-orientation="vertical" aria-label="Resize detail panel" />

      <div class="topology-detail" :style="{ flexBasis: detailWidth + 'px' }">
        <template v-if="selectedGroup">
          <div class="detail-header">
            <span class="detail-title">{{ selectedGroup.from }} <i class="pi pi-arrow-right" /> {{ selectedGroup.to }}</span>
            <Button icon="pi pi-times" text rounded size="small" aria-label="Close" @click="selectedKey = null" />
          </div>
          <div v-for="(e, i) in selectedGroup.items" :key="i" class="detail-row">
            <SubjectPath :subject="e.subject" />
            <Tag :value="e.type" :severity="e.type === 'stream' ? 'success' : 'info'" class="detail-type" />
          </div>
        </template>
        <template v-else-if="accounts.length && edgeGroups.length === 0">
          <span class="lab-muted">No cross-account imports currently provisioned.</span>
        </template>
        <template v-else-if="!loading">
          <span class="lab-muted">Click a line to see the subjects it carries.</span>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.topology-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.description {
  margin: 0;
  font-size: 0.85rem;
}
.error-text {
  margin: 0;
  color: var(--p-red-400, #f87171);
  font-size: 0.85rem;
}

.topology-body {
  display: flex;
  gap: 0.75rem;
  align-items: stretch;
}
.topology-svg {
  flex: 1 1 auto;
  min-width: 0;
  background: var(--lab-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
}

.topo-edge {
  cursor: pointer;
  stroke-dasharray: 4 4;
  opacity: 0.65;
  transition: opacity 0.15s ease;
}
.topo-edge:hover {
  opacity: 1;
}
.topo-edge.selected {
  opacity: 1;
}
@media (prefers-reduced-motion: no-preference) {
  .topo-edge {
    animation: topo-flow 1.1s linear infinite;
  }
}
@keyframes topo-flow {
  to {
    stroke-dashoffset: -16;
  }
}

.topo-node circle {
  fill: var(--lab-panel-bg);
  stroke: var(--lab-panel-border);
  stroke-width: 1.5;
}
.topo-node.hub circle {
  fill: color-mix(in srgb, var(--lab-accent) 16%, var(--lab-panel-bg));
  stroke: var(--lab-accent);
  stroke-width: 2;
}
.topo-node.dim circle {
  opacity: 0.45;
}
.topo-node.dim .topo-label {
  opacity: 0.5;
}
.topo-label {
  fill: var(--p-text-color);
  font-size: 12px;
  font-weight: 500;
}

.resize-handle {
  flex: 0 0 9px;
  cursor: col-resize;
  position: relative;
}
.resize-handle::after {
  content: '';
  position: absolute;
  left: 3px;
  right: 3px;
  top: 8%;
  bottom: 8%;
  border-radius: 2px;
  background: var(--lab-panel-border);
}
.resize-handle:hover::after,
.resize-handle:active::after {
  background: var(--lab-accent);
}

.topology-detail {
  flex: 0 0 auto;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 0.6rem;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  font-size: 0.8rem;
}
.detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.4rem;
  margin-bottom: 0.2rem;
}
.detail-title {
  font-weight: 600;
  font-size: 0.8rem;
}
.detail-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.4rem;
  padding: 0.15rem 0;
  border-bottom: 1px solid var(--lab-panel-border);
}
.detail-row:last-child {
  border-bottom: none;
}
.detail-type {
  font-size: 0.65rem;
  flex-shrink: 0;
}
</style>
