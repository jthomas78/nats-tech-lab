<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import { getNatsConnections, listAccounts } from '../api'
import { compactCount, exactCount } from '../format'
import { useDeferredLoading } from '../useDeferredLoading'
import NKey from './NKey.vue'

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
// Same 300ms mask tail UsersPanel hit — see useDeferredLoading.
const overlayLoading = useDeferredLoading(loading)
const errorMsg = ref('')

// /connz's paging envelope, passed straight through by the backend. `limit`
// is the page SIZE the server applied (its default is 1024) — NOT
// max_connections: 1024 connections is not a "full" server, it's just where
// /connz stops filling one response. The panel renders it to answer one
// question — "is the list below every connection, or one page of several?" —
// which is unanswerable from the rows alone.
const page = ref({ numConnections: 0, total: 0, offset: 0, limit: 0 })

// The server's own ceilings, read from /varz. maxConnections IS a cap — the
// server refuses the next connection past it — so unlike page.limit above it
// earns "N of max" framing. Zero means /varz didn't answer (a secondary read
// the backend absorbs), and the panel then draws no capacity rail rather than
// implying the server is unlimited.
const serverLimits = ref({ maxConnections: 0 })

// accounts-service's own account list (name ↔ publicKey) is the naming
// AUTHORITY here, not a fallback: accounts-service's whole job is knowing
// what an account is called, so whenever it's reachable its name wins —
// resolveLabel() checks it first. row.tenantLabel is observability-service's
// own server-side resolution (AccountsClient.Labels(), Phase 30d — see § 11
// of ARCHITECTURE-COMMUNICATIONS.md, and that section's own Phase 30d
// amendment) and only steps in when accounts-service's list doesn't cover a
// row. Historical note: before Phase 30, this backend-side label came from
// shipping-service independently matching live connections it held
// (tenantLabelsByAccount()), which stayed resolvable even if
// accounts-service itself was down. That resilience is gone post-Phase-30 —
// observability-service's own resolver now calls accounts-service too, so
// both tiers degrade together, not independently. resolveLabel() itself is
// unaffected (still checks accountNameByKey first, falls back to
// row.tenantLabel), but if accounts-service is unreachable, neither tier
// resolves anything and every row falls back to its raw NKey.
const accounts = ref([])
const accountNameByKey = computed(() =>
  Object.fromEntries(accounts.value.map((a) => [a.publicKey, a.name])),
)
function resolveLabel(row) {
  return accountNameByKey.value[row.account] || row.tenantLabel || null
}

async function refresh() {
  // Both calls go out together. The account list is only a naming lookup for
  // rows the connection list supplies, so nothing here depends on ordering —
  // awaiting one after the other just doubled the panel's load for no reason.
  //
  // Each keeps its own catch, so a failure on either side stays scoped to what
  // it feeds: a dead /connz is the error line, a dead account list is rows
  // falling back to their raw NKey. Promise.all over already-caught promises
  // never rejects, so there is no outer failure path left to handle.
  await Promise.all([
    getNatsConnections()
      .then((res) => {
        connections.value = res?.connections ?? []
        page.value = res?.page ?? { numConnections: 0, total: 0, offset: 0, limit: 0 }
        serverLimits.value = res?.server ?? { maxConnections: 0 }
        errorMsg.value = ''
      })
      .catch((err) => {
        errorMsg.value = err.message || 'Failed to load connections'
      }),
    listAccounts()
      .then((res) => {
        accounts.value = res
      })
      .catch(() => {
        // best-effort — see accounts/accountNameByKey doc comment above
      }),
  ])
  loading.value = false
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

// ── Total against the server's ceiling ───────────────────────────────────
// The Total card carries one ratio — connections against max_connections
// (/varz) — with a bar under it. Only that one, because it's the only real
// ratio available here: the server refuses clients past max_connections, so
// "N / max" is true. /connz's `limit` is a page SIZE (how many rows one
// response carries), so it never gets the same framing — it would read as
// capacity — and it earns no permanent line either: on a server under 1,024
// connections it would say "nothing hidden" forever. It surfaces only in the
// one state where it changes what the table below means (see `truncated`).
const pageLimit = computed(() => page.value.limit || 0)
// Every connection the server reported, which is what capacity measures — not
// the row count, which is only this page.
const pageTotal = computed(() => page.value.total || connections.value.length)
const shownCount = computed(() => page.value.numConnections || connections.value.length)

const maxConnections = computed(() => serverLimits.value.maxConnections || 0)
// One string, one type treatment — the ceiling is not set a size below the
// count. Both halves are the same kind of number, so the row shows them the
// same way (as the Msgs card already showed its pair).
const totalDisplay = computed(() =>
  maxConnections.value
    ? `${compactCount(pageTotal.value)} / ${compactCount(maxConnections.value)}`
    : compactCount(pageTotal.value),
)
const capacityFill = computed(() => {
  if (!maxConnections.value) return 0
  const pct = (pageTotal.value / maxConnections.value) * 100
  return Math.round(Math.min(100, pct) * 100) / 100
})
// 80% is where AccountsPanel's usage meters turn amber — same threshold, since
// this is the same kind of reading pressed against a configured limit.
const capacityHot = computed(
  () => maxConnections.value > 0 && pageTotal.value / maxConnections.value >= 0.8,
)
const capacityTitle = computed(
  () =>
    `The server accepts at most ${maxConnections.value.toLocaleString()} client connections (max_connections, reported by /varz) and refuses the next one past that.`,
)

// ── Paged reading (only surfaced when it changes what the table means) ──
const truncated = computed(
  () => pageLimit.value > 0 && page.value.offset + shownCount.value < pageTotal.value,
)
const pageCount = computed(() =>
  pageLimit.value ? Math.max(1, Math.ceil(pageTotal.value / pageLimit.value)) : 1,
)
const pageIndex = computed(() =>
  pageLimit.value ? Math.floor((page.value.offset || 0) / pageLimit.value) + 1 : 1,
)
const pagedNote = computed(() =>
  truncated.value
    ? `${shownCount.value.toLocaleString()} of ${pageTotal.value.toLocaleString()} shown · page ${pageIndex.value} of ${pageCount.value}`
    : '',
)
const pagedTitle = computed(
  () =>
    `/connz returns at most ${pageLimit.value.toLocaleString()} connections per request, so the table below is one page of ${pageCount.value}. That page size is not a limit on connections.`,
)

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
    (row.user || '').toLowerCase().includes(q) ||
    (row.subscriptionsList || []).some((s) => s.toLowerCase().includes(q))
  )
}
const rows = computed(() => connections.value.filter(rowMatches).sort((a, b) => a.cid - b.cid))

// ── Selection / detail pane ─────────────────────────────────────────────
const selectedCid = ref(null)
const selectedRow = computed(() => connections.value.find((c) => c.cid === selectedCid.value) || null)

// Always alphabetical. /connz returns subscriptions in its own order, so the
// same connection reorders between refreshes and the pane reads as if it were
// changing when nothing has. Sorting also groups the subject families for
// free, because the family is the leading token — which is why the pane needs
// no Family column to make the grouping legible. Sorted in the browser: the
// wire format is untouched.
const sortedSubscriptions = computed(() => [...(selectedRow.value?.subscriptionsList || [])].sort())
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
// itself is unreachable. The raw key is rendered through <NKey> (BR-061)
// rather than truncated locally: this cell used to carry a `slice(0, 10)…` of
// its own AND the full 56 characters on a `title`, which is exactly the
// pattern that rule exists to end.

// ── Credential column ────────────────────────────────────────────────────
// row.user is the credential's NAME (the `name` claim decoded from the
// connection's user JWT by observability-service, falling back to the
// account's name_tag); row.userKey is its public NKey — the only stable
// identity, since two distinct users can carry the same name.
//
// credentialDiverges() drives the amber highlight, and it is NOT an error
// state. Under the credential naming scheme a dedicated credential is
// named for its holder, spelled exactly as that process's nats.Name() —
// so an unhighlighted row is the healthy case, and a highlighted one
// means either a deliberately SHARED credential (platform.creds, held by
// refdata-service and accounts-service alike; acme.creds, held by all
// four ACME-side services) or the wrong creds file mounted. Those two are
// worth telling apart by eye, which is the whole reason the column earns
// its width over the Name column beside it.
function credentialDiverges(row) {
  if (!row.user || !row.name) return false
  return row.user !== row.name
}
// The user NKey used to live on this cell's `title` (BR-058). It now renders
// in the cell as an elided token — BR-061's same-task amendment to BR-058 —
// the same fact relocated, no longer 56 characters deep in a hover. It stays
// a secondary value either way: for the ephemeral browser credentials
// (accounts-service's auth/token.go mintUserToken) it is regenerated per
// session, so it churns on every browser reconnect.
</script>

<template>
  <div class="conn-panel">
    <div class="summary-row">
      <div class="summary-card">
        <div class="summary-label">Total</div>
        <div class="summary-value" :title="maxConnections ? capacityTitle : null">{{ totalDisplay }}</div>
        <div v-if="maxConnections" class="gauge" :class="{ hot: capacityHot }" :title="capacityTitle">
          <div class="gauge-rail"><div class="gauge-fill" :style="{ width: capacityFill + '%' }" /></div>
        </div>
        <div v-if="truncated" class="paged-note" :title="pagedTitle">{{ pagedNote }}</div>
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
        <div
          class="summary-value"
          :title="`${exactCount(totalInMsgs)} in / ${exactCount(totalOutMsgs)} out`"
        >{{ compactCount(totalInMsgs) }} / {{ compactCount(totalOutMsgs) }}</div>
      </div>
    </div>

    <div class="conn-toolbar">
      <span class="search-box">
        <i class="pi pi-search" />
        <input v-model="searchText" type="text" placeholder="filter by name, account, credential, ip, or subscription subject" />
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
      :loading="overlayLoading"
      size="small"
      scrollable
      scroll-height="flex"
      class="conn-table"
      data-key="cid"
      selectionMode="single"
      :metaKeySelection="false"
      :selection="selectedRow"
      @row-click="selectRow($event.data)"
    >
      <template #empty>
        <span class="lab-muted">No connections found.</span>
      </template>
      <Column header="Name" style="width:195px">
        <template #body="{ data }">
          <span :class="{ 'lab-muted': !data.name }" class="conn-name">{{ data.name || '(unnamed)' }}</span>
        </template>
      </Column>
      <Column header="Type" style="width:70px">
        <template #body="{ data }">
          <span class="type-badge" :class="data.type">{{ data.type }}</span>
        </template>
      </Column>
      <Column header="Lang" style="width:52px">
        <template #body="{ data }">{{ data.lang || '—' }}</template>
      </Column>
      <Column header="Account" bodyClass="pair-cell" style="width:190px">
        <template #body="{ data }">
          <!-- Label *and* key, the same pairing the Credential cell uses. The
               label alone answered "whose account", but two tenants' rows were
               then indistinguishable from a row whose label had resolved to
               the wrong thing; the key is what settles it. -->
          <span v-if="resolveLabel(data)" class="tenant-label">{{ resolveLabel(data) }}</span>
          <NKey :value="data.account" class="cell-nkey" />
        </template>
      </Column>
      <Column header="Credential" bodyClass="pair-cell" style="width:225px">
        <template #body="{ data }">
          <template v-if="data.user">
            <code class="cred" :class="{ diverged: credentialDiverges(data) }">{{ data.user }}</code>
            <NKey v-if="data.userKey" :value="data.userKey" class="cell-nkey" />
          </template>
          <span v-else class="lab-muted">—</span>
        </template>
      </Column>
      <Column header="Host" style="width:120px">
        <template #body="{ data }">
          <code class="acct">{{ data.ip }}:{{ data.port }}</code>
        </template>
      </Column>
      <Column header="RTT" style="width:75px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.rtt || '—' }}</template>
      </Column>
      <Column header="Uptime" style="width:65px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.uptime || '—' }}</template>
      </Column>
      <Column header="Idle" style="width:60px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.idle || '—' }}</template>
      </Column>
      <Column header="In" style="width:55px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.inMsgs?.toLocaleString() ?? 0 }}</template>
      </Column>
      <Column header="Out" style="width:55px" bodyClass="num-cell">
        <template #body="{ data }">{{ data.outMsgs?.toLocaleString() ?? 0 }}</template>
      </Column>
      <Column header="Subs" style="width:50px" bodyClass="num-cell">
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
              <!-- Account and Account NKey are two rows, not one, to match the
                   Credential / User NKey pair below: in both, the friendly
                   value and the raw NKey behind it are separate facts. The
                   table column still collapses them (label, or a truncated
                   key when none resolved) because it has one cell to work
                   with; the pane doesn't, so it doesn't. -->
              <div class="row">
                <span class="k">Account</span>
                <span class="v">
                  <span v-if="resolveLabel(selectedRow)" class="tenant-label">{{ resolveLabel(selectedRow) }}</span>
                  <span v-else class="lab-muted">—</span>
                </span>
              </div>
              <div class="row">
                <span class="k">Account NKey</span>
                <span class="v"><NKey :value="selectedRow.account" copyable /></span>
              </div>
              <div class="row">
                <span class="k">Credential</span>
                <span class="v">
                  <code v-if="selectedRow.user" class="cred" :class="{ diverged: credentialDiverges(selectedRow) }">{{ selectedRow.user }}</code>
                  <span v-else class="lab-muted">—</span>
                </span>
              </div>
              <div class="row">
                <span class="k">User NKey</span>
                <span class="v"><NKey :value="selectedRow.userKey" copyable /></span>
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
            <!-- A table in the Users panel's `.claims` shape, not a chip
                 cloud: two adjacent detail panes were drawing a list of
                 subjects two different ways. One column, because the family
                 IS the leading token (CLAUDE.md § "Subject families") — a
                 Family gutter would only restate the characters beside it. -->
            <table v-if="sortedSubscriptions.length" class="claims subs-table">
              <thead><tr><th>Subject</th></tr></thead>
              <tbody>
                <tr v-for="s in sortedSubscriptions" :key="s"><td><code>{{ s }}</code></td></tr>
              </tbody>
            </table>
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
  /* Cards wrap to a second row rather than squeezing below the width a value
     needs. A fixed 4-column grid gave ~94px of text room at a 760px viewport,
     where "20 / 65,536" needs ~102px at the row's one value size — that shortfall
     is what the old smaller-font-on-some-cards treatment was papering over.
     165px is the widest realistic pair ("54,120 / 65,536", ~141px) plus padding;
     the min() keeps very narrow viewports from overflowing instead of wrapping. */
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
/* Every value in this row, without exception: same size, weight and color,
   whether the card holds one number or a pair. Counters that outgrow the card
   shorten (compactCount) instead of shrinking the type — see ../format.js. */
.summary-value {
  font-size: 20px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  margin-top: 2px;
}

/* ── capacity bar: connections against max_connections ── */
.gauge {
  margin-top: 6px;
  cursor: help;
}
.gauge-rail {
  height: 2px;
  border-radius: 1px;
  background: var(--lab-panel-border);
}
.gauge-fill {
  height: 100%;
  /* A fraction of a percent on a ~110px card is sub-pixel — hold the fill at a
     legible tick so "barely any of it used" reads as a mark on the rail rather
     than an empty rail. */
  min-width: 4px;
  border-radius: 1px;
  background: var(--lab-accent);
}
/* Same amber the Accounts panel uses for a usage threshold — this is that same
   signal (a reading pressed against a limit), so it adds no new color. */
.gauge.hot .gauge-fill {
  background: var(--p-amber-400, #fbbf24);
}

/* Appears only when /connz paged, i.e. when the table below is partial — never
   in the steady state, where it would say "nothing hidden" forever. */
.paged-note {
  margin-top: 5px;
  font-size: 10px;
  line-height: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--p-amber-400, #fbbf24);
  cursor: help;
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

/* The selected row keeps the accent tint and the inset bar for as long as the
   detail pane below is open — it is the pane's anchor, not a hover state, so
   it must survive the pointer leaving the table. Explicit rather than left to
   Aura's default highlight, which is a flat fill with no left marker and does
   not read as "this is the row the pane is showing". */
.conn-table :deep(.p-datatable-tbody > tr.p-datatable-row-selected > td) {
  background: rgba(0, 111, 255, 0.08);
}
.conn-table :deep(.p-datatable-tbody > tr.p-datatable-row-selected > td:first-child) {
  box-shadow: inset 2px 0 0 var(--lab-accent, #006fff);
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
/* The Credential column. .diverged is a signal, not an error — see
   credentialDiverges() above for what it means and why it isn't red. */
.cred {
  font-size: 11px;
  color: var(--p-text-color);
}
.cred.diverged {
  color: #f0b429;
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
/* Deliberately a copy of UsersPanel's `.claims` rather than an import: the two
   panels have scoped styles and no shared stylesheet to hang this on. If a
   third panel needs it, it moves to `shared/unifi-theme/` — two is not yet a
   pattern. Same density, same header treatment, so the two adjacent detail
   panes read as one design. */
.claims {
  width: 100%;
  border-collapse: collapse;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}
.claims th {
  text-align: left;
  font-weight: 600;
  font-family: inherit;
  color: var(--p-text-disabled-color);
  border-bottom: 1px solid var(--lab-panel-border);
  padding: 1px 6px 2px;
}
.claims td {
  padding: 1px 6px;
  overflow-wrap: anywhere;
}
.claims tbody tr:nth-child(even) {
  background: rgba(255, 255, 255, 0.02);
}
/* The key sits *beside* the value it identifies, not under it: stacking made
   every row two lines tall to carry one short token, and the pair reads as one
   value when it stays on one line. .pair-cell is what keeps it that way — the
   two columns carrying a pair are sized to fit both halves and refuse to wrap
   between them, paid for out of the widths of the columns around them. Every
   column here is a fixed width on purpose: leaving one on `min-width` makes it
   absorb all the table's slack, which is where the dead space beside Name came
   from. Sized against the 1920px design viewport (CLAUDE.md § Frontend Design
   System), not against the preview pane. */
.cell-nkey {
  margin-left: 6px;
}
:deep(td.pair-cell) {
  white-space: nowrap;
}
.no-headers {
  font-size: 11px;
}
</style>
