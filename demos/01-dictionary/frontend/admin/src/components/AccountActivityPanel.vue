<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'

import { getNatsAccountActivity } from '../api'
import { compactCount, exactCount } from '../format'

// Account Activity panel (Phase 27) — per-account traffic + health from the
// NATS server's own /accstatz, proxied by dictionary/internal/rest/nats_ops.go
// (BUSINESS_RULES-SHIPPING.md BR-034). Complements Connections (every raw
// socket) and Services (every registered endpoint) with the account-level
// rollup; row shape is deliberately ServicesPanel's .svc-card pattern
// (dot · name · inline stat pairs · chevron), not a new one, since accstatz is
// the same shape of data — a handful of named things, each with a few live
// counters, worth expanding for detail.
const REFRESH_MS = 10000

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

let timer = null
onMounted(() => {
  refresh()
  timer = setInterval(refresh, REFRESH_MS)
})
onUnmounted(() => clearInterval(timer))

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
      </div>
      <div class="summary-card">
        <div class="summary-label">Subscriptions</div>
        <div class="summary-value">{{ compactCount(totals.subscriptions) }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Msgs In / Out</div>
        <div
          class="summary-value"
          :title="`${exactCount(totals.inMsgs)} in / ${exactCount(totals.outMsgs)} out`"
        >{{ compactCount(totals.inMsgs) }} / {{ compactCount(totals.outMsgs) }}</div>
      </div>
    </div>

    <!-- Slow consumers get no routine tile — this line exists only while the
         exceptional state is true, same mechanism as ConnectionsPanel's
         pagedNote. A permanent "0 slow" card would compete with real numbers
         every single day for a fact that matters on approximately none of them. -->
    <p v-if="slowAccounts.length" class="alarm-banner">
      ⚠ {{ totals.slowConsumers }} slow {{ totals.slowConsumers === 1 ? 'consumer' : 'consumers' }}
      across {{ slowAccounts.length }} {{ slowAccounts.length === 1 ? 'account' : 'accounts' }} —
      see the flagged {{ slowAccounts.length === 1 ? 'row' : 'rows' }} below.
    </p>

    <p v-if="loading" class="lab-muted loading-line">
      <span class="spinner" aria-hidden="true" />
      Loading account activity…
    </p>
    <p v-else-if="errorMsg" class="err-line">{{ errorMsg }}</p>
    <p v-else-if="!accounts.length" class="lab-muted empty-line">
      No accounts reported by the NATS server.
    </p>

    <div class="acct-scroll">
      <div
        v-for="acct in accounts"
        :key="acct.account"
        class="acct-card"
        :class="{ expanded: expanded.has(acct.account), crit: acct.slowConsumers > 0 }"
      >
        <button type="button" class="acct-head" @click="toggle(acct.account)">
          <span class="dot" :class="acct.slowConsumers > 0 ? 'crit' : 'ok'" />
          <span class="acct-name" :title="rawFallback(acct)">{{ displayName(acct) }}</span>
          <span v-if="isReserved(displayName(acct))" class="acct-tag">reserved</span>
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
            <span v-if="acct.slowConsumers > 0" class="stat crit">
              <b>{{ acct.slowConsumers }}</b><label>slow</label>
            </span>
            <span v-else class="stat"><b>{{ compactCount(acct.subscriptions) }}</b><label>subs</label></span>
          </span>
          <span class="chevron">▾</span>
        </button>

        <div v-if="expanded.has(acct.account)" class="detail">
          <div class="d-cell">
            <label>Connections</label>
            <div class="v">{{ exactCount(acct.connections) }}</div>
          </div>
          <div class="d-cell">
            <label>Subscriptions</label>
            <div class="v">{{ exactCount(acct.subscriptions) }}</div>
          </div>
          <div class="d-cell">
            <label>Received (in)</label>
            <div class="v">{{ exactCount(acct.inMsgs) }} msgs · {{ exactCount(acct.inBytes) }} B</div>
          </div>
          <div class="d-cell">
            <label>Sent (out)</label>
            <div class="v">{{ exactCount(acct.outMsgs) }} msgs · {{ exactCount(acct.outBytes) }} B</div>
          </div>
          <p v-if="acct.slowConsumers > 0" class="alarm-line">
            ⚠ <b>{{ acct.slowConsumers }} slow {{ acct.slowConsumers === 1 ? 'consumer' : 'consumers' }}</b>
            on this account right now — a subscriber isn't draining fast enough and the server is at risk
            of dropping its messages. Check connections on {{ displayName(acct) }} in the Connections panel.
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

/* ── expansion detail ── */
.detail {
  padding: 10px 14px 12px 34px;
  background: var(--lab-nested-bg);
  border-top: 1px solid var(--lab-panel-border);
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px 14px;
}
.d-cell label {
  display: block;
  font-size: 9px;
  color: var(--p-text-disabled-color);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  margin-bottom: 3px;
}
.d-cell .v {
  font-size: 13px;
  font-variant-numeric: tabular-nums;
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
