<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import { getNatsConnections, listAccounts } from '../api'

// Connections panel (Phase 17c) — every active NATS connection, proxied
// from the server's own /connz monitoring endpoint. Distinct from the
// Request/Reply panel: that traces individual rpc.*/api.* calls over time;
// this is "what's attached to the server right now" — server-wide, not
// scoped to the active tenant (a connz snapshot spans every account).
//
// A plain REST poll, not SSE: /connz is a single request/reply snapshot,
// not something the server can push changes for.
const REFRESH_MS = 10000

const connections = ref([])
const loading = ref(true)
const errorMsg = ref('')

// accounts-service's own account list (name ↔ publicKey) is the naming
// AUTHORITY here, not a fallback: accounts-service's whole job is knowing
// what an account is called, so whenever it's reachable its name wins —
// resolveLabel() checks it first. nats_ops.go's tenantLabelsByAccount()
// (row.tenantLabel) only steps in when accounts-service's list doesn't
// cover a row, which happens in exactly one situation: accounts-service
// itself is unreachable/erroring, so `accounts` below is empty or stale.
// tenantLabelsByAccount() can resolve PLATFORM/acme/globex independently
// (shipping-service already holds live connections on those, § 11 of
// ARCHITECTURE-COMMUNICATIONS.md) — so the panel degrades to "still
// mostly named" rather than "all raw NKeys" if accounts-service is down.
// It can never resolve SYS on its own (accounts-service's own connection —
// shipping-service holds no connection on that account by design), so
// that row specifically depends on accounts-service being reachable.
const accounts = ref([])
const accountNameByKey = computed(() =>
  Object.fromEntries(accounts.value.map((a) => [a.publicKey, a.name])),
)
function resolveLabel(row) {
  return accountNameByKey.value[row.account] || row.tenantLabel || null
}

async function refresh() {
  try {
    const res = await getNatsConnections()
    connections.value = res?.connections ?? []
    errorMsg.value = ''
  } catch (err) {
    errorMsg.value = err.message || 'Failed to load connections'
  } finally {
    loading.value = false
  }
  try {
    accounts.value = await listAccounts()
  } catch {
    // best-effort — see accounts/accountNameByKey doc comment above
  }
}

let timer = null
onMounted(() => {
  refresh()
  timer = setInterval(refresh, REFRESH_MS)
})
onUnmounted(() => clearInterval(timer))

// ── Summary ──────────────────────────────────────────────────────────────
const natsCount = computed(() => connections.value.filter((c) => c.type === 'nats').length)
const wsCount = computed(() => connections.value.filter((c) => c.type === 'websocket').length)
const totalInMsgs = computed(() => connections.value.reduce((sum, c) => sum + (c.inMsgs || 0), 0))
const totalOutMsgs = computed(() => connections.value.reduce((sum, c) => sum + (c.outMsgs || 0), 0))

// ── Filter ───────────────────────────────────────────────────────────────
const searchText = ref('')
const typeOn = ref('all') // 'all' | 'nats' | 'websocket'

function rowMatches(row) {
  if (typeOn.value !== 'all' && row.type !== typeOn.value) return false
  if (!searchText.value) return true
  const q = searchText.value.toLowerCase()
  return (
    (row.name || '').toLowerCase().includes(q) ||
    (row.account || '').toLowerCase().includes(q) ||
    (resolveLabel(row) || '').toLowerCase().includes(q) ||
    (row.ip || '').toLowerCase().includes(q) ||
    (row.subscriptionsList || []).some((s) => s.toLowerCase().includes(q))
  )
}
const rows = computed(() => connections.value.filter(rowMatches).sort((a, b) => a.cid - b.cid))

// ── Selection / detail pane ─────────────────────────────────────────────
const selectedCid = ref(null)
const selectedRow = computed(() => connections.value.find((c) => c.cid === selectedCid.value) || null)
function selectRow(row) {
  const selection = window.getSelection()
  if (selection && selection.toString().length > 0) return
  selectedCid.value = selectedCid.value === row.cid ? null : row.cid
}
function closeDetail() {
  selectedCid.value = null
}

// ── Formatting ───────────────────────────────────────────────────────────
function formatTime(iso) {
  if (!iso) return '—'
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
// Account is a raw NATS account NKey (public identifier) — rendered (as a
// monospace code snippet, see the template) only when neither resolver
// could put a friendly name on this connection (rendered as a colored tag
// instead — the two need different markup, not just different text, so
// there's no single "accountLabel" string helper). resolveLabel() above
// tries accountNameByKey first (accounts-service's account list — the
// naming authority, see that computed's doc comment), then falls back to
// the backend's tenantLabel (nats_ops.go's tenantLabelsByAccount, resolved
// from shipping-service's own connection set) only if accounts-service's
// list didn't cover this row — which in practice means accounts-service
// itself is unreachable. Truncated the way most NATS admin tooling
// displays account identifiers.
function shortAccount(acc) {
  if (!acc) return '—'
  return acc.length > 12 ? `${acc.slice(0, 10)}…` : acc
}
</script>

<template>
  <div class="conn-panel">
    <div class="summary-row">
      <div class="summary-card">
        <div class="summary-label">Total</div>
        <div class="summary-value">{{ connections.length }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">TCP (nats)</div>
        <div class="summary-value">{{ natsCount }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">WebSocket</div>
        <div class="summary-value">{{ wsCount }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Msgs in / out</div>
        <div class="summary-value small">{{ totalInMsgs.toLocaleString() }} / {{ totalOutMsgs.toLocaleString() }}</div>
      </div>
    </div>

    <div class="conn-toolbar">
      <span class="search-box">
        <i class="pi pi-search" />
        <input v-model="searchText" type="text" placeholder="filter by name, account, ip, or subscription subject" />
      </span>
      <button
        v-for="opt in ['all', 'nats', 'websocket']"
        :key="opt"
        type="button"
        class="chip"
        :class="{ on: typeOn === opt }"
        @click="typeOn = opt"
      >{{ opt }}</button>
    </div>

    <p v-if="errorMsg" class="err-line">{{ errorMsg }}</p>

    <DataTable
      :value="rows"
      :loading="loading"
      size="small"
      scrollable
      scroll-height="flex"
      class="conn-table"
      data-key="cid"
      selectionMode="single"
      :metaKeySelection="false"
      @row-click="selectRow($event.data)"
    >
      <template #empty>
        <span class="lab-muted">No connections found.</span>
      </template>
      <Column header="Name" style="min-width:160px">
        <template #body="{ data }">
          <span :class="{ 'lab-muted': !data.name }" class="conn-name">{{ data.name || '(unnamed)' }}</span>
        </template>
      </Column>
      <Column header="Type" style="width:90px">
        <template #body="{ data }">
          <span class="type-badge" :class="data.type">{{ data.type }}</span>
        </template>
      </Column>
      <Column header="Lang" style="width:80px">
        <template #body="{ data }">{{ data.lang || '—' }}</template>
      </Column>
      <Column header="Account" style="width:110px">
        <template #body="{ data }">
          <span v-if="resolveLabel(data)" class="tenant-label" :title="data.account">{{ resolveLabel(data) }}</span>
          <code v-else class="acct" :title="data.account">{{ shortAccount(data.account) }}</code>
        </template>
      </Column>
      <Column header="RTT" style="width:70px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.rtt || '—' }}</template>
      </Column>
      <Column header="Uptime" style="width:80px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.uptime || '—' }}</template>
      </Column>
      <Column header="Idle" style="width:80px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.idle || '—' }}</template>
      </Column>
      <Column header="In" style="width:70px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.inMsgs?.toLocaleString() ?? 0 }}</template>
      </Column>
      <Column header="Out" style="width:70px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.outMsgs?.toLocaleString() ?? 0 }}</template>
      </Column>
      <Column header="Subs" style="width:60px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.subscriptions ?? 0 }}</template>
      </Column>
    </DataTable>

    <section v-if="selectedRow" class="detail">
      <div class="detail-head">
        <span class="type-badge" :class="selectedRow.type">{{ selectedRow.type }}</span>
        <span class="conn-name">{{ selectedRow.name || '(unnamed)' }}</span>
        <span class="meta lab-muted">
          cid {{ selectedRow.cid }} · {{ selectedRow.ip }}:{{ selectedRow.port }} ·
          {{ selectedRow.lang }} {{ selectedRow.version }}
        </span>
        <span class="close" title="Close" @click="closeDetail">✕</span>
      </div>
      <div class="panes">
        <div class="pane">
          <div class="pane-title">Connection</div>
          <div class="pane-body">
            <div class="kv">
              <div class="row"><span class="k">CID</span><span class="v">{{ selectedRow.cid }}</span></div>
              <div class="row"><span class="k">IP</span><span class="v">{{ selectedRow.ip }}:{{ selectedRow.port }}</span></div>
              <div class="row">
                <span class="k">Account</span>
                <span class="v">
                  <span v-if="resolveLabel(selectedRow)" class="tenant-label">{{ resolveLabel(selectedRow) }}</span>
                  {{ selectedRow.account || '—' }}
                </span>
              </div>
              <div class="row"><span class="k">Started</span><span class="v">{{ formatTime(selectedRow.start) }}</span></div>
              <div class="row"><span class="k">Uptime</span><span class="v">{{ selectedRow.uptime || '—' }}</span></div>
              <div class="row"><span class="k">RTT</span><span class="v">{{ selectedRow.rtt || '—' }}</span></div>
              <div class="row"><span class="k">Last activity</span><span class="v">{{ formatTime(selectedRow.lastActivity) }}</span></div>
              <div class="row"><span class="k">Idle</span><span class="v">{{ selectedRow.idle || '—' }}</span></div>
            </div>
          </div>
        </div>
        <div class="pane">
          <div class="pane-title">
            Subscriptions <span class="count">({{ selectedRow.subscriptions ?? 0 }})</span>
          </div>
          <div class="pane-body">
            <div v-if="selectedRow.subscriptionsList?.length" class="subs-list">
              <code v-for="s in selectedRow.subscriptionsList" :key="s" class="sub-item">{{ s }}</code>
            </div>
            <span v-else class="lab-muted no-headers">no subscriptions</span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.conn-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

/* ── summary cards ── */
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

/* ── toolbar (mirrors RpcPanel's) ── */
.conn-toolbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.search-box {
  flex: 1;
  min-width: 160px;
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--lab-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.search-box input {
  flex: 1;
  min-width: 0;
  background: none;
  border: none;
  outline: none;
  color: var(--p-text-color);
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  line-height: 16px;
  padding: 1px 8px;
  border-radius: 3px;
  border: 1px solid var(--lab-panel-border);
  color: var(--p-text-muted-color);
  cursor: pointer;
  background: transparent;
  font-family: inherit;
}
.chip.on {
  border-color: var(--lab-accent);
  color: var(--lab-accent);
  background: rgba(0, 111, 255, 0.1);
}
.err-line {
  flex: none;
  margin: 0;
  font-size: 12px;
  color: #e5484d;
}

/* ── table ── */
.conn-table {
  flex: 1;
  min-height: 0;
}
.conn-table :deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
.conn-table :deep(.p-datatable-tbody > tr > td) {
  padding-top: 3px;
  padding-bottom: 3px;
}
.conn-table :deep(.num-cell) {
  font-variant-numeric: tabular-nums;
  color: var(--p-text-muted-color);
}
.conn-name {
  font-weight: 500;
}
.acct {
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.tenant-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--lab-accent);
  background: rgba(0, 111, 255, 0.1);
  border-radius: 3px;
  padding: 1px 6px;
}
.type-badge {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  font-weight: 700;
  border-radius: 3px;
  padding: 0 5px;
  line-height: 15px;
  display: inline-block;
}
.type-badge.nats {
  color: #4cc2ff;
  background: rgba(56, 178, 255, 0.12);
}
.type-badge.websocket {
  color: #b18cff;
  background: rgba(148, 101, 255, 0.13);
}

/* ── detail split ── */
.detail {
  flex: none;
  height: 40%;
  min-height: 200px;
  display: flex;
  flex-direction: column;
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
}
.detail-head {
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 10px;
  border-bottom: 1px solid var(--lab-panel-border);
}
.meta {
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.detail-head .close {
  margin-left: auto;
  color: var(--p-text-disabled-color);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 2px 4px;
  border-radius: 3px;
}
.detail-head .close:hover {
  color: var(--p-text-color);
  background: rgba(255, 255, 255, 0.05);
}
.panes {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
}
.pane {
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.pane + .pane {
  border-left: 1px solid var(--lab-panel-border);
}
.pane-title {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
  padding: 5px 10px 3px;
}
.pane-title .count {
  color: var(--p-text-disabled-color);
  font-weight: 400;
  text-transform: none;
}
.pane-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 0 10px 8px;
}
.kv {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  overflow: hidden;
}
.kv .row {
  display: grid;
  grid-template-columns: 110px 1fr;
}
.kv .row:nth-child(odd) {
  background: rgba(255, 255, 255, 0.02);
}
.kv .k {
  color: var(--p-text-muted-color);
  padding: 1px 8px;
  border-right: 1px solid var(--lab-panel-border);
}
.kv .v {
  color: var(--p-text-color);
  padding: 1px 8px;
  overflow-wrap: anywhere;
}
.subs-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}
.sub-item {
  font-size: 11px;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  background: var(--lab-bg);
  border: 1px solid var(--lab-panel-border);
  padding: 2px 6px;
  border-radius: 3px;
  color: var(--p-text-muted-color);
}
.no-headers {
  font-size: 11px;
}
</style>
