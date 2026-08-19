<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'

import { getNatsAccountActivity, getNatsAccountActivityHistory } from '../api'
import { compactCount, exactCount } from '../format'

// Overview tab (Phase 45) — supersedes the old standalone Account Activity
// panel (BUSINESS_RULES-SHIPPING.md BR-034, now folded into BR-043/BR-044).
// The collapsed row keeps AccountActivityPanel's exact live-snapshot shape;
// what changes is the expand: instead of restating the same numbers in a
// flat grid, it swaps in two small trend charts sourced from
// observability-service's 60-minute ring buffer
// (GET /api/nats/account-activity/history). Row shape stays ServicesPanel's
// .svc-card pattern, unchanged from before.
const REFRESH_MS = 10000
const SEARCH_THRESHOLD = 3
const DURATIONS = ['5m', '30m', '1h']

const RESERVED_NAMES = new Set(['platform', 'sys'])
function isReserved(label) {
  return RESERVED_NAMES.has(label?.toLowerCase())
}

const accounts = ref([])
const loading = ref(true)
const errorMsg = ref('')
const expanded = ref(new Set())

async function refresh() {
  try {
    const res = await getNatsAccountActivity()
    accounts.value = res?.accounts ?? []
    errorMsg.value = ''
  } catch (err) {
    errorMsg.value = err.message || 'Failed to load account activity'
  } finally {
    loading.value = false
  }
}

// ── History (BR-043) — separate poll from the live snapshot above, since it
// hits a different route and re-fetches whenever the duration selector
// changes, not just on the fixed interval.
const duration = ref('30m')
const historyByAccount = ref(new Map())

async function refreshHistory() {
  try {
    const res = await getNatsAccountActivityHistory(duration.value)
    const next = new Map()
    for (const series of res?.accounts ?? []) next.set(series.account, series.buckets ?? [])
    historyByAccount.value = next
  } catch {
    // Best-effort — a failed history fetch costs the charts, never the live
    // snapshot rows above, same "secondary read" degrade this service's
    // other panels already use for /varz and tenant labels.
  }
}

let timer = null
let historyTimer = null
onMounted(() => {
  refresh()
  refreshHistory()
  timer = setInterval(refresh, REFRESH_MS)
  historyTimer = setInterval(refreshHistory, REFRESH_MS)
})
onUnmounted(() => {
  clearInterval(timer)
  clearInterval(historyTimer)
})
watch(duration, refreshHistory)

function toggle(account) {
  const next = new Set(expanded.value)
  next.has(account) ? next.delete(account) : next.add(account)
  expanded.value = next
}

// Matches ConnectionsPanel's raw-NKey fallback: truncated, full value in title.
function shortAccount(acc) {
  return acc.length > 12 ? `${acc.slice(0, 10)}…` : acc
}
function displayName(acct) {
  return acct.tenantLabel || shortAccount(acct.account)
}
function rawFallback(acct) {
  return acct.tenantLabel ? null : acct.account
}

// ── Summary ──────────────────────────────────────────────────────────────
const totals = computed(() => {
  let connections = 0
  let subscriptions = 0
  let inMsgs = 0
  let outMsgs = 0
  let slowConsumers = 0
  for (const a of accounts.value) {
    connections += a.connections
    subscriptions += a.subscriptions
    inMsgs += a.inMsgs
    outMsgs += a.outMsgs
    slowConsumers += a.slowConsumers
  }
  return { connections, subscriptions, inMsgs, outMsgs, slowConsumers }
})
const slowAccounts = computed(() => accounts.value.filter((a) => a.slowConsumers > 0))

// ── Search (BR-044) — shown only once there are more than 3 accounts; below
// that the list is short enough to just read, and a search box above 2-3
// rows is a control with nothing to do.
const searchQuery = ref('')
const showSearch = computed(() => accounts.value.length > SEARCH_THRESHOLD)
watch(showSearch, (visible) => {
  if (!visible) searchQuery.value = ''
})
const filteredAccounts = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!showSearch.value || !q) return accounts.value
  return accounts.value.filter((a) => displayName(a).toLowerCase().includes(q))
})

// ── Trend charts — a plain 0..100 x / 0..20 y viewBox polyline, the same
// normalize-to-viewbox approach PulsePanel's latency sparkline uses.
function sparklinePoints(values) {
  if (!values.length) return ''
  const min = Math.min(...values)
  const max = Math.max(...values)
  return values
    .map((v, i) => {
      const x = values.length > 1 ? (i / (values.length - 1)) * 100 : 0
      const y = max > min ? 20 - ((v - min) / (max - min)) * 18 : 10
      return `${x},${y}`
    })
    .join(' ')
}

// Fleet-level series is summed client-side from the per-account series
// rather than a separate fleet-aggregate endpoint (Phase 45 design decision).
const fleetBuckets = computed(() => {
  const byTs = new Map()
  for (const buckets of historyByAccount.value.values()) {
    for (const b of buckets) {
      const cur = byTs.get(b.ts) ?? { ts: b.ts, connections: 0, subscriptions: 0, inMsgsDelta: 0, outMsgsDelta: 0 }
      cur.connections += b.connections
      cur.subscriptions += b.subscriptions
      cur.inMsgsDelta += b.inMsgsDelta
      cur.outMsgsDelta += b.outMsgsDelta
      byTs.set(b.ts, cur)
    }
  }
  return Array.from(byTs.values()).sort((a, b) => new Date(a.ts) - new Date(b.ts))
})
const connectionsSpark = computed(() => sparklinePoints(fleetBuckets.value.map((b) => b.connections)))
const subscriptionsSpark = computed(() => sparklinePoints(fleetBuckets.value.map((b) => b.subscriptions)))
const msgsSpark = computed(() => sparklinePoints(fleetBuckets.value.map((b) => b.inMsgsDelta + b.outMsgsDelta)))

function accountBuckets(account) {
  return historyByAccount.value.get(account) ?? []
}
function connSubsChart(account) {
  const buckets = accountBuckets(account)
  return {
    connPoints: sparklinePoints(buckets.map((b) => b.connections)),
    subsPoints: sparklinePoints(buckets.map((b) => b.subscriptions)),
    lastConn: buckets.length ? buckets[buckets.length - 1].connections : 0,
    lastSubs: buckets.length ? buckets[buckets.length - 1].subscriptions : 0,
  }
}
function throughputBars(account) {
  const buckets = accountBuckets(account)
  const max = Math.max(1, ...buckets.map((b) => b.inBytesDelta), ...buckets.map((b) => b.outBytesDelta))
  return buckets.map((b) => ({
    inH: Math.max(1, (b.inBytesDelta / max) * 34),
    outH: Math.max(1, (b.outBytesDelta / max) * 34),
  }))
}
// Ties directly to the account's current slow-consumer alarm, not a
// scripted duration estimate — see Main-POC-Plan.md's Phase 45 design note.
function lagNote(acct) {
  return acct.slowConsumers > 0 ? 'inbound has outpaced outbound during this window' : null
}
</script>

<template>
  <div class="acct-panel">
    <div class="summary-row">
      <div class="summary-card">
        <div class="summary-label">Accounts</div>
        <div class="summary-value">{{ accounts.length }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Connections</div>
        <div class="summary-value">{{ compactCount(totals.connections) }}</div>
        <svg
          v-if="connectionsSpark"
          class="summary-spark"
          viewBox="0 0 100 20"
          preserveAspectRatio="none"
        ><polyline :points="connectionsSpark" /></svg>
      </div>
      <div class="summary-card">
        <div class="summary-label">Subscriptions</div>
        <div class="summary-value">{{ compactCount(totals.subscriptions) }}</div>
        <svg
          v-if="subscriptionsSpark"
          class="summary-spark"
          viewBox="0 0 100 20"
          preserveAspectRatio="none"
        ><polyline :points="subscriptionsSpark" /></svg>
      </div>
      <div class="summary-card">
        <div class="summary-label">Msgs In / Out</div>
        <div
          class="summary-value"
          :title="`${exactCount(totals.inMsgs)} in / ${exactCount(totals.outMsgs)} out`"
        >{{ compactCount(totals.inMsgs) }} / {{ compactCount(totals.outMsgs) }}</div>
        <svg
          v-if="msgsSpark"
          class="summary-spark"
          viewBox="0 0 100 20"
          preserveAspectRatio="none"
        ><polyline :points="msgsSpark" /></svg>
      </div>
    </div>

    <div class="toolbar-row">
      <!-- BR-043 — duration selector, defaults to 30m -->
      <div
        class="duration-select"
        role="group"
        aria-label="Trend window"
      >
        <button
          v-for="opt in DURATIONS"
          :key="opt"
          type="button"
          class="duration-btn"
          :class="{ active: duration === opt }"
          @click="duration = opt"
        >{{ opt }}</button>
      </div>

      <!-- BR-044 — shown only once there are more than 3 accounts -->
      <div
        v-if="showSearch"
        class="search-wrap"
      >
        <svg
          class="search-icon"
          viewBox="0 0 16 16"
          aria-hidden="true"
        >
          <circle
            cx="6.5"
            cy="6.5"
            r="4.5"
            fill="none"
            stroke="currentColor"
            stroke-width="1.4"
          />
          <line
            x1="9.8"
            y1="9.8"
            x2="14"
            y2="14"
            stroke="currentColor"
            stroke-width="1.4"
            stroke-linecap="round"
          />
        </svg>
        <input
          v-model="searchQuery"
          type="text"
          placeholder="Search accounts…"
          aria-label="Search accounts"
        >
        <span class="acct-count">
          {{ searchQuery.trim() ? `${filteredAccounts.length} of ${accounts.length} accounts` : `${accounts.length} accounts` }}
        </span>
      </div>
    </div>

    <!-- Slow consumers get no routine tile — this line exists only while the
         exceptional state is true, same mechanism as ConnectionsPanel's
         pagedNote. A permanent "0 slow" card would compete with real numbers
         every single day for a fact that matters on approximately none of them. -->
    <p
      v-if="slowAccounts.length"
      class="alarm-banner"
    >
      ⚠ {{ totals.slowConsumers }} slow {{ totals.slowConsumers === 1 ? 'consumer' : 'consumers' }}
      across {{ slowAccounts.length }} {{ slowAccounts.length === 1 ? 'account' : 'accounts' }} —
      see the flagged {{ slowAccounts.length === 1 ? 'row' : 'rows' }} below.
    </p>

    <p
      v-if="loading"
      class="lab-muted loading-line"
    >
      <span
        class="spinner"
        aria-hidden="true"
      />
      Loading account activity…
    </p>
    <p
      v-else-if="errorMsg"
      class="err-line"
    >{{ errorMsg }}</p>
    <p
      v-else-if="!accounts.length"
      class="lab-muted empty-line"
    >
      No accounts reported by the NATS server.
    </p>
    <p
      v-else-if="showSearch && searchQuery.trim() && !filteredAccounts.length"
      class="lab-muted empty-line"
    >
      No accounts match "{{ searchQuery.trim() }}".
    </p>

    <div class="acct-scroll">
      <div
        v-for="acct in filteredAccounts"
        :key="acct.account"
        class="acct-card"
        :class="{ expanded: expanded.has(acct.account), crit: acct.slowConsumers > 0 }"
      >
        <button
          type="button"
          class="acct-head"
          @click="toggle(acct.account)"
        >
          <span
            class="dot"
            :class="acct.slowConsumers > 0 ? 'crit' : 'ok'"
          />
          <span
            class="acct-name"
            :title="rawFallback(acct)"
          >{{ displayName(acct) }}</span>
          <span
            v-if="isReserved(displayName(acct))"
            class="acct-tag"
          >reserved</span>
          <span class="acct-meta">
            <span class="stat"><b>{{ compactCount(acct.connections) }}</b><label>conns</label></span>
            <span
              class="stat"
              :title="`${exactCount(acct.inMsgs)} in / ${exactCount(acct.outMsgs)} out`"
            ><b>{{ compactCount(acct.inMsgs) }} / {{ compactCount(acct.outMsgs) }}</b><label>msgs in/out</label></span>
            <span
              class="stat"
              :title="`${exactCount(acct.inBytes)} B in / ${exactCount(acct.outBytes)} B out`"
            ><b>{{ compactCount(acct.inBytes) }} / {{ compactCount(acct.outBytes) }}</b><label>bytes in/out</label></span>
            <span
              v-if="acct.slowConsumers > 0"
              class="stat crit"
            >
              <b>{{ acct.slowConsumers }}</b><label>slow</label>
            </span>
            <span
              v-else
              class="stat"
            ><b>{{ compactCount(acct.subscriptions) }}</b><label>subs</label></span>
          </span>
          <span class="chevron">▾</span>
        </button>

        <!-- Trend charts, not a restated number grid (the redundancy this
             tab replaced — see accounts_overview_pulse_design.md). -->
        <div
          v-if="expanded.has(acct.account)"
          class="detail"
        >
          <div class="chart-card">
            <div class="chart-title">Connections &amp; subscriptions</div>
            <svg
              class="chart-svg"
              viewBox="0 0 100 20"
              preserveAspectRatio="none"
            >
              <polyline
                class="line-conn"
                :points="connSubsChart(acct.account).connPoints"
              />
              <polyline
                class="line-subs"
                :points="connSubsChart(acct.account).subsPoints"
              />
            </svg>
            <div class="chart-caption">
              {{ exactCount(connSubsChart(acct.account).lastConn) }} conns ·
              {{ exactCount(connSubsChart(acct.account).lastSubs) }} subs (latest)
            </div>
          </div>
          <div class="chart-card">
            <div class="chart-title">Throughput</div>
            <div class="bar-row">
              <div
                v-for="(bar, i) in throughputBars(acct.account)"
                :key="i"
                class="bar-pair"
              >
                <span
                  class="bar in"
                  :style="{ height: `${bar.inH}px` }"
                />
                <span
                  class="bar out"
                  :style="{ height: `${bar.outH}px` }"
                />
              </div>
            </div>
            <div class="chart-caption">
              {{ exactCount(acct.inBytes) }} B in / {{ exactCount(acct.outBytes) }} B out (cumulative)
            </div>
          </div>
          <p
            v-if="lagNote(acct)"
            class="alarm-line"
          >
            ⚠ <b>{{ acct.slowConsumers }} slow {{ acct.slowConsumers === 1 ? 'consumer' : 'consumers' }}</b>
            on this account right now — {{ lagNote(acct) }}, and the server is at risk of dropping its
            messages. Check connections on {{ displayName(acct) }} in the Connections panel.
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.acct-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

/* ── summary cards (mirrors ConnectionsPanel's/ServicesPanel's) ── */
.summary-row {
  flex: none;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(165px, 100%), 1fr));
  gap: 0.5rem;
}
.summary-card {
  position: relative;
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 0.5rem 0.65rem;
  overflow: hidden;
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
.summary-spark {
  display: block;
  width: 100%;
  height: 16px;
  margin-top: 4px;
}
.summary-spark polyline {
  fill: none;
  stroke: var(--lab-accent);
  stroke-width: 1.5;
  vector-effect: non-scaling-stroke;
}

/* ── toolbar: duration selector + gated search ── */
.toolbar-row {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.duration-select {
  display: inline-flex;
  border: 1px solid var(--lab-panel-border);
  border-radius: 999px;
  padding: 2px;
  gap: 2px;
}
.duration-btn {
  all: unset;
  cursor: pointer;
  font-size: 11px;
  font-weight: 600;
  padding: 3px 10px;
  border-radius: 999px;
  color: var(--p-text-disabled-color);
}
.duration-btn.active {
  background: var(--lab-accent);
  color: var(--p-primary-contrast-color, #0b0e14);
}
.search-wrap {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 200px;
  max-width: 320px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 4px 8px;
}
.search-icon {
  width: 13px;
  height: 13px;
  flex-shrink: 0;
  color: var(--p-text-disabled-color);
}
.search-wrap input {
  all: unset;
  flex: 1;
  font-size: 12px;
  min-width: 0;
}
.acct-count {
  flex-shrink: 0;
  font-size: 10px;
  color: var(--p-text-disabled-color);
  white-space: nowrap;
}

.alarm-banner {
  flex: none;
  margin: 0;
  padding: 6px 10px;
  font-size: 11px;
  color: var(--p-red-400, #f87171);
  background: rgba(248, 113, 113, 0.1);
  border: 1px solid rgba(248, 113, 113, 0.35);
  border-radius: 4px;
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

/* ── card list (mirrors ServicesPanel's .svc-card) ── */
.acct-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.acct-card {
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  overflow: hidden;
  flex: none;
}
.acct-card.crit {
  border-color: rgba(248, 113, 113, 0.45);
}
.acct-head {
  all: unset;
  box-sizing: border-box;
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  cursor: pointer;
}
.acct-head:hover {
  background: rgba(255, 255, 255, 0.03);
}
.acct-head:focus-visible {
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
.dot.crit {
  background: #f87171;
  box-shadow: 0 0 0 3px rgba(248, 113, 113, 0.22);
}
.acct-name {
  font-weight: 600;
  font-size: 13px;
}
.acct-tag {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.03em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  padding: 0 5px;
  line-height: 15px;
}
.acct-meta {
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
.stat.crit b {
  color: #f87171;
}
.chevron {
  color: var(--p-text-disabled-color);
  font-size: 11px;
  transition: transform 0.15s;
}
.acct-card.expanded .chevron {
  transform: rotate(180deg);
}

/* ── expansion detail: trend charts, not a restated number grid ── */
.detail {
  padding: 10px 14px 12px 34px;
  background: var(--lab-nested-bg);
  border-top: 1px solid var(--lab-panel-border);
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px 14px;
}
.chart-card {
  min-width: 0;
}
.chart-title {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.03em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
  margin-bottom: 4px;
}
.chart-svg {
  display: block;
  width: 100%;
  height: 40px;
}
.line-conn {
  fill: none;
  stroke: var(--lab-accent);
  stroke-width: 1.5;
  vector-effect: non-scaling-stroke;
}
.line-subs {
  fill: none;
  stroke: var(--p-text-disabled-color);
  stroke-width: 1.5;
  stroke-dasharray: 3 2;
  vector-effect: non-scaling-stroke;
}
.chart-caption {
  font-size: 10px;
  color: var(--p-text-disabled-color);
  margin-top: 2px;
}
.bar-row {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  height: 40px;
}
.bar-pair {
  display: flex;
  align-items: flex-end;
  gap: 1px;
  flex: 1;
}
.bar {
  flex: 1;
  border-radius: 1px 1px 0 0;
}
.bar.in {
  background: var(--lab-accent);
}
.bar.out {
  background: var(--p-text-disabled-color);
}
.alarm-line {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 2px 0 0;
  font-size: 12px;
  color: #f87171;
  background: rgba(248, 113, 113, 0.12);
  border: 1px solid rgba(248, 113, 113, 0.35);
  border-radius: 4px;
  padding: 6px 10px;
}
.alarm-line b {
  font-variant-numeric: tabular-nums;
}
</style>
