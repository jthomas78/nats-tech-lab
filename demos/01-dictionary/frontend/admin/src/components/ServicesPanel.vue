<script setup>
import { computed, onMounted, onUnmounted, ref, unref } from 'vue'

import { compactCount, exactCount } from '../format'
import { listServices } from '../nats/serviceList.js'
import { usePlatformConnection } from '../nats/usePlatformConnection.js'

// Services panel (Phase 17c; NATS since Phase 54c, decision 17, BR-072) —
// every service registered via nats.go/micro, read from
// observability-service's live service list over the Admin UI's PLATFORM
// connection (api._platform.observability.service.list.v1). There is no
// REST fallback: GET /api/nats/services stays only until 54d retires it.
//
// The snapshot already did the discovery: PING/INFO/STATS in PLATFORM and in
// each tenant account (BR-AC31's monitor.{tenant}.srv.> import) inside one
// bounded window. So the panel reads three things straight off it:
//
//   - account — each card is one service name in one account. The same name
//     in two accounts is two cards (keyed by account + name).
//   - readiness — ready / not ready from shared/natsready, else unknown. A
//     reply means responding, not dependency-ready; the panel never guesses.
//   - complete — false when an account failed, the tenant list failed or the
//     reply was truncated. The panel shows the errors; a failed discovery is
//     never shown as an empty success.
//
// Known gap (not a bug): accounts-service registers on the SYS account, which
// the discovery does not query.
const REFRESH_MS = 10000

const { connected, request } = usePlatformConnection()

const snapshot = ref(null)
const loading = ref(true)
const errorMsg = ref('')
const expanded = ref(new Set())

const services = computed(() => snapshot.value?.services ?? [])

async function refresh() {
  if (unref(connected) === false) {
    errorMsg.value = 'Not connected to NATS — the service list is asked over the PLATFORM connection.'
    loading.value = false
    return
  }
  try {
    snapshot.value = await listServices(request)
    errorMsg.value = ''
    // Deliberately no auto-expand: refresh() is on a REFRESH_MS poll, so an
    // auto-expand re-opened a card the user had just collapsed. Expansion is
    // the user's to control.
  } catch (err) {
    // Keep the last snapshot; say this request failed.
    errorMsg.value = `Service list request failed: ${err?.message || err}`
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

function cardKey(svc) {
  return `${svc.account}/${svc.name}`
}

function toggle(key) {
  const next = new Set(expanded.value)
  next.has(key) ? next.delete(key) : next.add(key)
  expanded.value = next
}

// ── Discovery provenance ─────────────────────────────────────────────────
const failedAccounts = computed(() => (snapshot.value?.accounts ?? []).filter((a) => a.status === 'error'))
const partial = computed(() => snapshot.value != null && snapshot.value.complete === false)

const READINESS = {
  ready: { label: 'ready', dot: 'ok', title: 'natsready says its dependencies are ready.' },
  'not-ready': { label: 'not ready', dot: 'bad', title: 'natsready says a dependency is not ready.' },
}
const UNKNOWN = {
  label: 'readiness unknown',
  dot: 'unknown',
  title: 'It answered discovery, so it is responding. It has no natsready responder here, so its dependencies are not known.',
}
function readiness(svc) {
  return READINESS[svc.readiness] ?? UNKNOWN
}

function versions(svc) {
  return [...new Set(svc.instances.map((i) => i.version).filter(Boolean))].map((v) => `v${v}`).join(', ')
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
        <div
          class="summary-value"
          :title="`${exactCount(totals.requests)} requests / ${exactCount(totals.errors)} errors`"
        >{{ compactCount(totals.requests) }} /
          <span :class="{ errv: totals.errors > 0 }">{{ compactCount(totals.errors) }}</span>
        </div>
      </div>
    </div>

    <p v-if="loading" class="lab-muted loading-line">
      <span class="spinner" aria-hidden="true" />
      Discovering services — this asks every micro-registered instance in
      PLATFORM and each tenant account, and waits one window for replies…
    </p>
    <p v-if="errorMsg" class="err-line">{{ errorMsg }}</p>
    <p v-if="snapshot" class="lab-muted observed-line" data-testid="svc-observed">
      Observed {{ formatTime(snapshot.observedAt) }} in a {{ snapshot.windowMs }} ms window
      across {{ (snapshot.accounts ?? []).length }} account(s). A reply means responding;
      no reply means not observed in the window, not dead.
    </p>
    <div v-if="partial" class="partial-line" data-testid="svc-partial">
      <strong>Partial result.</strong> These are the services that answered — not all of them.
      <ul>
        <li v-for="e in snapshot.errors ?? []" :key="e">{{ e }}</li>
        <li v-for="a in failedAccounts" :key="a.name">{{ a.name }}: {{ a.error || 'discovery failed' }}</li>
      </ul>
    </div>
    <p v-if="snapshot && !partial && !services.length" class="lab-muted empty-line">
      No micro service answered within the {{ snapshot.windowMs }} ms window, in PLATFORM or any tenant account.
    </p>

    <div class="svc-scroll">
      <div
        v-for="svc in services"
        :key="cardKey(svc)"
        class="svc-card"
        :class="{ expanded: expanded.has(cardKey(svc)) }"
      >
        <button type="button" class="svc-head" @click="toggle(cardKey(svc))">
          <span class="dot" :class="readiness(svc).dot" />
          <span class="svc-name">{{ svc.name }}</span>
          <span class="svc-version">{{ versions(svc) }}</span>
          <span class="account-tag" :title="`Found in the ${svc.account} account`">{{ svc.account }}</span>
          <span
            class="readiness-tag"
            :class="readiness(svc).dot"
            :title="readiness(svc).title"
            data-testid="svc-readiness"
          >{{ readiness(svc).label }}</span>
          <span class="svc-meta">
            <span class="stat"><b>{{ svc.instances.length }}</b><label>instances</label></span>
            <span class="stat"><b>{{ svc.instances.reduce((n, i) => n + i.endpoints.length, 0) }}</b><label>endpoints</label></span>
          </span>
          <span class="chevron">▾</span>
        </button>

        <div v-if="expanded.has(cardKey(svc))" class="instances">
          <div v-for="inst in svc.instances" :key="inst.id" class="instance">
            <div class="instance-head">
              <span class="dot" :class="readiness(svc).dot" />
              <span>Instance</span>
              <span v-if="inst.metadata?.tenant" class="tenant-tag">{{ inst.metadata.tenant }}</span>
              <code class="inst-id" :title="inst.id">{{ inst.id }}</code>
              <span class="instance-meta lab-muted">v{{ inst.version || '?' }} · started {{ formatTime(inst.started) }}</span>
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
  /* Wrap rather than squeeze a card below the width its value needs — see the
     same rule in ConnectionsPanel for the measurements behind 165px. */
  grid-template-columns: repeat(auto-fit, minmax(min(165px, 100%), 1fr));
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
/* One size for every value in the row, pairs included — long counters shorten
   via compactCount rather than dropping a type size (../format.js). */
.summary-value {
  font-size: 20px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  margin-top: 2px;
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
.dot.bad {
  background: #e5484d;
}
/* Unknown is a hollow ring, not grey fill: "responding, dependencies not
   known" must not read as a muted "ok". */
.dot.unknown {
  background: transparent;
  box-shadow: inset 0 0 0 1.5px var(--p-text-disabled-color);
}
.observed-line {
  flex: none;
  margin: 0;
  font-size: 11px;
}
/* Phase 54c: a partial discovery uses the lab's warning accent (#9a7b1e),
   the same one Pulse's unavailable notice uses. */
.partial-line {
  flex: none;
  font-size: 12px;
  border: 1px solid rgba(154, 123, 30, 0.6);
  border-left: 3px solid #9a7b1e;
  background: rgba(154, 123, 30, 0.08);
  border-radius: 4px;
  padding: 6px 10px;
}
.partial-line ul {
  margin: 4px 0 0;
  padding-left: 1.1rem;
}
.account-tag {
  font-size: 11px;
  font-weight: 600;
  color: var(--p-text-muted-color);
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  padding: 0 6px;
}
.readiness-tag {
  font-size: 11px;
  border-radius: 3px;
  padding: 0 6px;
  color: var(--p-text-muted-color);
}
.readiness-tag.ok {
  color: #2fbf71;
  background: rgba(47, 191, 113, 0.1);
}
.readiness-tag.bad {
  color: #e5484d;
  background: rgba(229, 72, 77, 0.1);
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
