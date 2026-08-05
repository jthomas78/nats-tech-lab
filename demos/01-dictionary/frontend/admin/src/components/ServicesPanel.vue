<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'

import { getNatsServices } from '../api'

// Services panel (Phase 17c) — every service registered via nats.go/micro
// (see ARCHITECTURE-COMMUNICATIONS.md §4), discovered by broadcasting
// $SRV.STATS and collecting replies (the same protocol `nats micro stats`
// uses). Complements the Connections panel: that shows every raw
// subscription a process holds; this shows the endpoints a service
// deliberately *offers* plus their request/error/latency counters.
//
// Known gap (not a bug): accounts-service registers on the SYS account it
// already holds for JWT operations, which this panel's query connections
// (PLATFORM + the active tenant) can't see across the account boundary — see
// the backend's nats_ops.go package doc and Main-POC-Plan.md Phase 17c.
const REFRESH_MS = 10000

const services = ref([])
const loading = ref(true)
const errorMsg = ref('')
const expanded = ref(new Set())

async function refresh() {
  try {
    const res = await getNatsServices()
    services.value = res?.services ?? []
    errorMsg.value = ''
    // Auto-expand the first load so the panel isn't just a wall of collapsed bars.
    if (expanded.value.size === 0 && services.value.length) {
      expanded.value = new Set([services.value[0].name])
    }
  } catch (err) {
    errorMsg.value = err.message || 'Failed to load services'
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

function toggle(name) {
  const next = new Set(expanded.value)
  next.has(name) ? next.delete(name) : next.add(name)
  expanded.value = next
}

// ── Summary ──────────────────────────────────────────────────────────────
const instanceCount = computed(() => services.value.reduce((sum, s) => sum + s.instances.length, 0))
const endpointCount = computed(() =>
  services.value.reduce((sum, s) => sum + s.instances.reduce((n, i) => n + i.endpoints.length, 0), 0),
)
const totals = computed(() => {
  let requests = 0
  let errors = 0
  for (const s of services.value) {
    for (const i of s.instances) {
      for (const e of i.endpoints) {
        requests += e.numRequests
        errors += e.numErrors
      }
    }
  }
  return { requests, errors }
})

// ── Formatting ───────────────────────────────────────────────────────────
function formatTime(iso) {
  if (!iso) return '—'
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
function maxRequests(instance) {
  return Math.max(1, ...instance.endpoints.map((e) => e.numRequests))
}
function volumePct(e, instance) {
  return Math.round((e.numRequests / maxRequests(instance)) * 100)
}
</script>

<template>
  <div class="svc-panel">
    <div class="summary-row">
      <div class="summary-card">
        <div class="summary-label">Services</div>
        <div class="summary-value">{{ services.length }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Instances</div>
        <div class="summary-value">{{ instanceCount }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Endpoints</div>
        <div class="summary-value">{{ endpointCount }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Requests / Errors</div>
        <div class="summary-value small">
          {{ totals.requests.toLocaleString() }} /
          <span :class="{ errv: totals.errors > 0 }">{{ totals.errors.toLocaleString() }}</span>
        </div>
      </div>
    </div>

    <p v-if="loading" class="lab-muted loading-line">
      <span class="spinner" aria-hidden="true" />
      Discovering services — this queries every micro-registered instance
      and waits for replies, so it takes a moment…
    </p>
    <p v-else-if="errorMsg" class="err-line">{{ errorMsg }}</p>
    <p v-else-if="!services.length" class="lab-muted empty-line">
      No micro-registered services discovered on the PLATFORM or active-tenant account.
    </p>

    <div class="svc-scroll">
      <div v-for="svc in services" :key="svc.name" class="svc-card" :class="{ expanded: expanded.has(svc.name) }">
        <button type="button" class="svc-head" @click="toggle(svc.name)">
          <span class="dot ok" />
          <span class="svc-name">{{ svc.name }}</span>
          <span class="svc-version">v{{ svc.version }}</span>
          <span class="svc-meta">
            <span class="stat"><b>{{ svc.instances.length }}</b><label>instances</label></span>
            <span class="stat"><b>{{ svc.instances.reduce((n, i) => n + i.endpoints.length, 0) }}</b><label>endpoints</label></span>
          </span>
          <span class="chevron">▾</span>
        </button>

        <div v-if="expanded.has(svc.name)" class="instances">
          <div v-for="inst in svc.instances" :key="inst.id" class="instance">
            <div class="instance-head">
              <span class="dot ok" />
              <span>Instance</span>
              <span v-if="inst.metadata?.tenant" class="tenant-tag">{{ inst.metadata.tenant }}</span>
              <code class="inst-id" :title="inst.id">{{ inst.id }}</code>
              <span class="instance-meta lab-muted">started {{ formatTime(inst.started) }}</span>
            </div>
            <table class="endpoints-table">
              <thead>
                <tr>
                  <th>Endpoint</th>
                  <th>Subject</th>
                  <th>Queue</th>
                  <th class="right">Requests</th>
                  <th class="right">Errors</th>
                  <th class="right">Avg latency</th>
                  <th style="width:60px" />
                </tr>
              </thead>
              <tbody>
                <tr v-for="ep in inst.endpoints" :key="ep.name">
                  <td><strong>{{ ep.name }}</strong></td>
                  <td><code>{{ ep.subject }}</code></td>
                  <td><code>{{ ep.queueGroup || '—' }}</code></td>
                  <td class="right">{{ ep.numRequests }}</td>
                  <td class="right" :class="{ errv: ep.numErrors > 0 }">{{ ep.numErrors }}</td>
                  <td class="right">{{ ep.numRequests ? `${ep.averageProcessingTimeMs} ms` : '—' }}</td>
                  <td>
                    <span v-if="ep.numRequests" class="vol-bar" :style="{ width: Math.max(4, volumePct(ep, inst) * 0.5) + 'px' }" />
                  </td>
                </tr>
                <tr v-if="!inst.endpoints.length">
                  <td colspan="7" class="lab-muted no-endpoints">registration only — no endpoints</td>
                </tr>
              </tbody>
            </table>
            <p v-if="inst.endpoints.some((e) => e.lastError)" class="last-errors">
              <span v-for="ep in inst.endpoints.filter((e) => e.lastError)" :key="ep.name" class="last-error">
                <code>{{ ep.name }}</code>: {{ ep.lastError }}
              </span>
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.svc-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

/* ── summary cards (mirrors ConnectionsPanel's) ── */
.summary-row {
  flex: none;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.5rem;
}
.summary-card {
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 0.5rem 0.65rem;
}
.summary-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
}
.summary-value {
  font-size: 20px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  margin-top: 2px;
}
.summary-value.small {
  font-size: 15px;
}
.err-line {
  flex: none;
  margin: 0;
  font-size: 12px;
  color: #e5484d;
}
.empty-line {
  flex: none;
  margin: 0;
  font-size: 12px;
}
.loading-line {
  flex: none;
  margin: 0;
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.spinner {
  flex-shrink: 0;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid var(--lab-panel-border);
  border-top-color: var(--lab-accent);
  animation: spin 0.7s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .spinner {
    animation: none;
  }
}

/* ── card list ── */
.svc-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.svc-card {
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  overflow: hidden;
  flex: none;
}
.svc-head {
  all: unset;
  box-sizing: border-box;
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  cursor: pointer;
}
.svc-head:hover {
  background: rgba(255, 255, 255, 0.03);
}
.svc-head:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dot.ok {
  background: #2fbf71;
}
.svc-name {
  font-weight: 600;
  font-size: 13px;
}
.svc-version {
  font-size: 11px;
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
}
.svc-meta {
  margin-left: auto;
  display: flex;
  gap: 18px;
}
.stat {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  line-height: 1.15;
}
.stat b {
  font-size: 14px;
  font-variant-numeric: tabular-nums;
}
.stat label {
  font-size: 9px;
  color: var(--p-text-disabled-color);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.chevron {
  color: var(--p-text-disabled-color);
  font-size: 11px;
  transition: transform 0.15s;
}
.svc-card.expanded .chevron {
  transform: rotate(180deg);
}

/* ── instances / endpoints ──
   Indent + pin-line matches AccountsPanel.vue's .bu-expansion (Business
   Units nested under an account row) — same 1.1rem pin-line offset,
   2.75rem content indent, and --lab-nested-bg zone, so the two nested
   "child rows under a parent" patterns in this admin app read as one
   visual language rather than two independent ones. */
.instances {
  position: relative;
  padding-left: 2.75rem;
  border-top: 1px solid var(--lab-panel-border);
  background: linear-gradient(to right, var(--lab-panel-bg) 1.1rem, var(--lab-nested-bg) 1.1rem);
}
.instances::before {
  content: '';
  position: absolute;
  left: 1.1rem;
  top: 0;
  bottom: 0.25rem;
  width: 2px;
  background: rgba(0, 111, 255, 0.35);
  border-radius: 1px;
}
.instance + .instance {
  border-top: 1px solid var(--lab-panel-border);
}
.instance-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  background: var(--lab-bg);
  font-size: 11px;
}
.inst-id {
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.tenant-tag {
  font-size: 11px;
  font-weight: 600;
  color: var(--lab-accent);
  background: rgba(0, 111, 255, 0.1);
  border-radius: 3px;
  padding: 1px 6px;
}
.instance-meta {
  margin-left: auto;
  font-size: 11px;
}
.endpoints-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.endpoints-table thead th {
  text-align: left;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--p-text-disabled-color);
  padding: 5px 12px;
  border-bottom: 1px solid var(--lab-panel-border);
}
.endpoints-table thead th.right {
  text-align: right;
}
.endpoints-table tbody td {
  padding: 4px 12px;
  border-bottom: 1px solid var(--lab-panel-border);
}
.endpoints-table tbody tr:last-child td {
  border-bottom: none;
}
.endpoints-table td.right {
  text-align: right;
}
.endpoints-table code {
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.errv {
  color: #e5484d;
}
.vol-bar {
  display: inline-block;
  height: 4px;
  border-radius: 2px;
  background: var(--lab-accent);
}
.no-endpoints {
  font-size: 11px;
  text-align: center;
  padding: 6px 0;
}
.last-errors {
  margin: 0;
  padding: 4px 12px 6px;
  font-size: 11px;
  color: #e5484d;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.last-error code {
  color: inherit;
  font-size: 11px;
}
</style>
