<script setup>
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'

import { getKvBucketEntries, listKVBuckets } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'
import { parseKvNotifySubject } from '../nats/kvNotifySubject.js'

// KV inspector: every registered KV bucket across every NATS account this
// backend reaches (listKVBuckets — not scoped to the topbar's tenant
// selector, which only ever showed the active tenant's 3-4 buckets and hid
// PLATFORM's refdata-service buckets entirely), grouped by account in a
// left rail. Selected bucket's current contents + live update feed on the
// right.
//
// Bucket names collide across accounts (every tenant provisions its own
// ships/container/meta), so the rail keys selection on {account, bucket},
// not bucket alone — see listKVBuckets' doc comment server-side.
//
// Live "recent updates" only works for the account the browser's own
// tenant NATS connection is currently authenticated as: NATS enforces
// account isolation at the server, so a browser connected to ACME's
// account cannot subscribe to GLOBEX's or PLATFORM's subjects, full stop —
// there is no cross-account workaround, nor should there be. Buckets in any
// other account still get a contents snapshot (that's backend-mediated,
// not subject to the browser's own account boundary); the live feed panel
// says why it's unavailable instead of just sitting empty. PLATFORM
// buckets never get a live feed at all yet, for a different reason:
// refdata-service doesn't publish notify.*.kv.{bucket}.{key}.changed for
// its own writes the way shipping-service's kvstore.Store.EnableNotify
// does — see liveUnavailableReason below. That's unrelated to the rail's
// account-dot (see .account-dot below), which reflects the account's own
// active/suspended lifecycle status from accounts-service, not anything
// about the browser's connection or live-feed eligibility.
const REFRESH_MS = 15000
const FEED_CAP = 40

const OP_SEVERITY = { PUT: 'success', DEL: 'warn', PURGE: 'danger' }

// ── Bucket rail ───────────────────────────────────────────────────────────────
const buckets = ref([]) // [{ bucket, account, values, history, bytes, ttlSeconds }]
// Accounts is the authoritative account list (every account this backend
// knows about, including ones whose buckets couldn't be listed — e.g. a
// suspended tenant, whose cross-account $JS.API access always fails). The
// rail is built from this, not from `buckets`, so a suspended account with
// zero listable buckets still gets a dimmed group header instead of
// disappearing entirely.
const accounts = ref([]) // [{ name, status }]
const activeAccount = ref(null)
const activeBucket = ref(null)
const bucketFilter = ref('')

// Which account groups are collapsed — absence means expanded, so every
// account starts open (the whole point of this rail is to see everything
// at once; collapsing is an opt-in way to tuck an account away, not a
// default that hides buckets from view).
const collapsedAccounts = reactive(new Set())
function toggleAccount(account) {
  if (collapsedAccounts.has(account)) collapsedAccounts.delete(account)
  else collapsedAccounts.add(account)
}

const filteredBuckets = computed(() => {
  const q = bucketFilter.value.trim().toLowerCase()
  const all = [...buckets.value].sort((a, b) => a.bucket.localeCompare(b.bucket))
  return q
    ? all.filter((b) => b.bucket.toLowerCase().includes(q) || b.account.toLowerCase().includes(q))
    : all
})

// Grouped by account (alphabetical, matching the order listKVBuckets
// already returns) so the rail reads as "every account, every bucket" —
// still a flat list within each account for now; collapsing PLATFORM's
// larger group by refdata context is a deliberate follow-up, not done here.
//
// Built from `accounts` (every known account), not derived purely from
// `filteredBuckets` — otherwise a suspended account with zero listable
// buckets would have no group at all. When a search filter is active,
// accounts with no matching buckets are still dropped, matching the filter's
// own intent (an account name match already pulls its whole bucket list
// through, see filteredBuckets above).
const groupedByAccount = computed(() => {
  const byAccount = new Map()
  for (const b of filteredBuckets.value) {
    if (!byAccount.has(b.account)) byAccount.set(b.account, [])
    byAccount.get(b.account).push(b)
  }
  const filtering = bucketFilter.value.trim().length > 0
  return accounts.value
    .filter((a) => !filtering || byAccount.has(a.name))
    .map((a) => [a.name, a.status, byAccount.get(a.name) ?? []])
})

const hasAccounts = computed(() => accounts.value.length > 0)

async function refreshBuckets() {
  let res
  try {
    res = await listKVBuckets()
  } catch {
    return // best-effort; keep last known
  }
  const list = res?.buckets ?? []
  buckets.value = list
  accounts.value = res?.accounts ?? []
  const stillExists = list.some((b) => b.account === activeAccount.value && b.bucket === activeBucket.value)
  if (!activeBucket.value || !stillExists) {
    // Prefer the ship read model as the opening view, else the first bucket.
    const first = list.find((b) => b.bucket === 'ships') ?? list[0]
    activeAccount.value = first?.account ?? null
    activeBucket.value = first?.bucket ?? null
  }
}

const activeStatus = computed(() =>
  buckets.value.find((b) => b.account === activeAccount.value && b.bucket === activeBucket.value),
)

function selectBucket(account, bucket) {
  activeAccount.value = account
  activeBucket.value = bucket
}

// ── Selected bucket: contents snapshot + live feed ─────────────────────────────
const { connected: tenantConnected, tenant, subscribe } = useNatsConnection()
const entries = reactive(new Map()) // key → { key, value, revision, created }
const feed = ref([]) // live changes only, newest first
const loading = ref(false)
let unsubscribe = null

// null when the selected bucket's account IS the browser's currently
// connected tenant (live feed works normally); otherwise the reason it
// doesn't, shown in place of the feed.
const liveUnavailableReason = computed(() => {
  if (!activeAccount.value) return null
  if (activeAccount.value === 'platform') {
    return "refdata-service doesn't publish live KV change notifications yet — showing a point-in-time snapshot only."
  }
  if (activeAccount.value !== tenant.value) {
    return `Switch the topbar tenant to "${activeAccount.value}" to watch this account's live changes.`
  }
  return null
})

function resetBucketState() {
  entries.clear()
  feed.value = []
  loading.value = true
}

async function connectBucket(account, bucket) {
  disconnectBucket()
  resetBucketState()

  const canWatchLive = account === tenant.value && tenantConnected.value
  if (canWatchLive) {
    unsubscribe = subscribe(`notify.*.kv.${bucket}.>`, (value, subject) => {
      const parsed = parseKvNotifySubject(subject)
      if (!parsed) return
      const { key } = parsed
      const change = value === null
        ? { key, op: 'DEL' }
        : { key, op: 'PUT', value, revision: undefined, created: new Date().toISOString() }
      if (change.op === 'PUT') {
        entries.set(key, { key, value: change.value, revision: change.revision, created: change.created })
      } else {
        entries.delete(key)
      }
      feed.value = [{ ...change, at: new Date().toLocaleTimeString() }, ...feed.value].slice(0, FEED_CAP)
    })
  }

  try {
    const rows = await getKvBucketEntries(account, bucket)
    for (const row of rows ?? []) {
      entries.set(row.key, { key: row.key, value: row.value, revision: row.revision, created: row.created })
    }
  } catch {
    // best-effort snapshot — live feed above still works even if this fails
  } finally {
    loading.value = false
  }
}

function disconnectBucket() {
  unsubscribe?.()
  unsubscribe = null
}

let refreshTimer = null
onMounted(() => {
  refreshBuckets()
  refreshTimer = setInterval(refreshBuckets, REFRESH_MS)
})
onUnmounted(() => {
  clearInterval(refreshTimer)
  disconnectBucket()
})

// On tenant (re)connect: re-fetch the bucket list immediately (the rail no
// longer depends on this for WHICH buckets show, but a fresh connection is
// still worth a re-check) and retry the active bucket's subscription — its
// live-watch eligibility depends on which tenant the browser is now
// authenticated as.
watch([tenantConnected, tenant], ([isConnected]) => {
  if (!isConnected) return
  refreshBuckets()
  if (activeAccount.value && activeBucket.value) connectBucket(activeAccount.value, activeBucket.value)
})

// Switch the single live connection whenever the selected bucket changes.
watch([activeAccount, activeBucket], ([account, bucket]) => {
  if (account && bucket) connectBucket(account, bucket)
  else disconnectBucket()
})

// ── Contents table ─────────────────────────────────────────────────────────────
const keyFilter = ref('')
const rows = computed(() => {
  const q = keyFilter.value.trim()
  const all = [...entries.values()].sort((a, b) => a.key.localeCompare(b.key))
  return q ? all.filter((r) => r.key.includes(q)) : all
})

const expandedKey = ref(null)
function toggleRow(key) {
  expandedKey.value = expandedKey.value === key ? null : key
}

function valuePreview(value) {
  if (value === undefined || value === null) return '—'
  const str = JSON.stringify(value)
  return str.length > 90 ? str.slice(0, 90) + '…' : str
}
function valueFull(value) {
  return JSON.stringify(value, null, 2)
}
function formatTime(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
function formatBytes(n) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
function ttlLabel(seconds) {
  return seconds > 0 ? `${seconds}s` : 'no expiry'
}
</script>

<template>
  <div class="kv-inspector">
    <!-- Bucket rail, grouped by account -->
    <aside class="rail" aria-label="KV buckets, grouped by account">
      <div class="rail-summary">
        <strong>{{ buckets.length }}</strong> buckets
        <span class="lab-muted">· {{ groupedByAccount.length }} accounts</span>
      </div>
      <InputText v-model="bucketFilter" size="small" placeholder="filter buckets or accounts…" class="rail-filter" />

      <div v-for="[account, accountStatus, accountBuckets] in groupedByAccount" :key="account" class="account-group">
        <button
          type="button"
          class="account-head"
          :class="{ collapsed: collapsedAccounts.has(account) }"
          :aria-expanded="!collapsedAccounts.has(account)"
          @click="toggleAccount(account)"
        >
          <span class="caret">▶</span>
          <span class="account-dot" :class="{ ro: accountStatus !== 'active' }" :title="`account status: ${accountStatus}`"></span>
          <span class="account-name">{{ account }}</span>
          <span class="account-kind">{{ account === 'platform' ? 'read-only' : 'tenant' }}</span>
          <span class="account-count lab-muted">{{ accountBuckets.length }}</span>
        </button>
        <div v-if="!collapsedAccounts.has(account)" class="account-body">
          <button
            v-for="b in accountBuckets"
            :key="account + '::' + b.bucket"
            type="button"
            class="rail-item"
            :class="{ active: b.account === activeAccount && b.bucket === activeBucket }"
            @click="selectBucket(b.account, b.bucket)"
          >
            <code class="rail-name">{{ b.bucket }}</code>
            <span class="rail-count">{{ b.values }}</span>
          </button>
        </div>
      </div>

      <p v-if="!hasAccounts" class="lab-muted rail-empty">No KV buckets registered yet.</p>
      <p v-else-if="bucketFilter.trim() && !filteredBuckets.length" class="lab-muted rail-empty">No buckets match "{{ bucketFilter }}".</p>
    </aside>

    <!-- Detail -->
    <section v-if="activeBucket" class="detail">
      <header class="detail-head">
        <div class="detail-title">
          <code class="bucket-name">{{ activeBucket }}</code>
          <Tag severity="secondary" :value="activeAccount" />
          <Tag
            v-if="!liveUnavailableReason"
            :severity="tenantConnected ? 'success' : 'danger'"
            :value="tenantConnected ? 'watching' : 'off'"
          />
          <Tag v-else severity="secondary" value="snapshot only" />
        </div>
        <div class="detail-meta lab-muted">
          <span><strong>{{ entries.size }}</strong> keys</span>
          <span v-if="activeStatus">· <strong>{{ activeStatus.values }}</strong> revisions</span>
          <span v-if="activeStatus">· history {{ activeStatus.history }}</span>
          <span v-if="activeStatus">· {{ formatBytes(activeStatus.bytes) }}</span>
          <span v-if="activeStatus">· {{ ttlLabel(activeStatus.ttlSeconds) }}</span>
        </div>
      </header>

      <!-- Contents snapshot -->
      <div class="contents">
        <div class="contents-head">
          <h4>Contents</h4>
          <InputText v-model="keyFilter" size="small" placeholder="filter by key…" class="key-filter" />
        </div>
        <div class="table-scroll">
          <table class="kv-table">
            <thead>
              <tr>
                <th class="col-key">Key</th>
                <th class="col-val">Value</th>
                <th class="col-rev">Rev</th>
                <th class="col-time">Updated</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="loading">
                <td colspan="4" class="empty lab-muted">Loading bucket contents…</td>
              </tr>
              <tr v-else-if="!rows.length">
                <td colspan="4" class="empty lab-muted">
                  {{ keyFilter ? 'No keys match the filter.' : 'This bucket is empty.' }}
                </td>
              </tr>
              <tr v-for="row in rows" :key="row.key" class="kv-row" @click="toggleRow(row.key)">
                <td class="col-key"><code>{{ row.key }}</code></td>
                <td class="col-val">
                  <pre v-if="expandedKey === row.key" class="value-full">{{ valueFull(row.value) }}</pre>
                  <span v-else class="value-preview">{{ valuePreview(row.value) }}</span>
                </td>
                <td class="col-rev">r{{ row.revision }}</td>
                <td class="col-time">{{ formatTime(row.created) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Live update feed -->
      <div class="feed">
        <h4>Recent updates <span class="lab-muted feed-sub">— live KV changes since you opened this bucket</span></h4>
        <div class="feed-scroll">
          <p v-if="liveUnavailableReason" class="lab-muted feed-empty">{{ liveUnavailableReason }}</p>
          <p v-else-if="!feed.length" class="lab-muted feed-empty">
            No changes yet. Dispatch a command (or edit reference data) to watch it land here.
          </p>
          <ul v-else class="feed-list">
            <li v-for="(ev, i) in feed" :key="ev.revision + '-' + i" class="feed-row">
              <Tag :severity="OP_SEVERITY[ev.op] ?? 'info'" :value="ev.op" />
              <code class="feed-key">{{ ev.key }}</code>
              <span class="feed-rev lab-muted">r{{ ev.revision }}</span>
              <span class="feed-time lab-muted">{{ ev.at }}</span>
            </li>
          </ul>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.kv-inspector {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 0.75rem;
}
/* ── Rail ── */
/* 340px, not the old 240: refdata-service's longest bucket names
   (refdata-acme-atlantic-fleet-v1) were ellipsizing, which defeats the
   point of a panel whose job is telling near-identical names apart. */
.rail {
  width: 340px;
  flex-shrink: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding-right: 0.5rem;
  border-right: 1px solid var(--lab-panel-border);
}
.rail-summary {
  flex-shrink: 0;
  padding: 0 8px;
  font-size: 11px;
}
.rail-summary strong {
  font-variant-numeric: tabular-nums;
}
.rail-filter {
  flex-shrink: 0;
  width: 100%;
}
.account-group {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  overflow: hidden;
}
/* The account band is the rail's one piece of visual weight: it separates
   "which NATS account" (a hard, server-enforced boundary) from "which
   bucket" (a choice within it), so the two never read as one flat list. */
.account-head {
  all: unset;
  box-sizing: border-box;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 0.4rem;
  width: 100%;
  padding: 6px 9px;
  background: var(--lab-nested-bg);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-color);
}
.account-head:hover {
  background: var(--lab-nested-bg-hover);
}
.account-head:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
.caret {
  flex-shrink: 0;
  font-size: 8px;
  color: var(--p-text-muted-color);
  transform: rotate(90deg);
  transition: transform 0.12s ease;
}
.account-head.collapsed .caret {
  transform: rotate(0deg);
}
@media (prefers-reduced-motion: reduce) {
  .caret { transition: none; }
}
/* Green = accountStatus === 'active' (accounts-service's own lifecycle
   state, PLATFORM always active); gray = suspended. This is the account's
   own status, not anything about the browser's connection or live-feed
   eligibility (the detail pane's watching/snapshot-only tag, a separate
   concern — see liveUnavailableReason above) — a suspended tenant still
   lists its buckets here, just dimmed. */
.account-dot {
  flex-shrink: 0;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--p-green-500, #3ecb85);
}
.account-dot.ro {
  background: var(--p-text-disabled-color);
}
.account-kind {
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: uppercase;
  color: var(--p-text-muted-color);
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  padding: 0 4px;
}
.account-count {
  margin-left: auto;
  font-variant-numeric: tabular-nums;
  font-weight: 400;
  text-transform: none;
  letter-spacing: normal;
}
.account-body {
  display: flex;
  flex-direction: column;
}
.rail-item {
  all: unset;
  box-sizing: border-box;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 5px 9px 5px 22px;
  border-top: 1px solid var(--lab-panel-border);
  border-left: 2px solid transparent;
  font-size: 12px;
  color: var(--p-text-muted-color);
}
.rail-item:hover {
  background: var(--lab-nested-bg);
  color: var(--p-text-color);
}
.rail-item.active {
  background: var(--lab-nested-bg);
  border-left-color: var(--lab-accent);
  color: var(--p-text-color);
  font-weight: 600;
}
.rail-item:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
.rail-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rail-count {
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
  font-size: 11px;
  background: var(--lab-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 10px;
  padding: 0 7px;
  line-height: 17px;
}
.rail-empty {
  font-size: 12px;
  padding: 0 8px;
}
/* ── Detail ── */
.detail {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}
.detail-head {
  flex-shrink: 0;
}
.detail-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.bucket-name {
  font-size: 14px;
  font-weight: 600;
}
.detail-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  font-size: 11px;
  margin-top: 3px;
}
.detail-meta strong {
  color: var(--p-text-color);
  font-variant-numeric: tabular-nums;
}
/* ── Contents ── */
.contents {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.contents-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 0.4rem;
}
.contents-head h4,
.feed h4 {
  margin: 0;
  font-size: 12px;
  letter-spacing: 0.02em;
}
.key-filter {
  width: 220px;
}
.table-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
}
.kv-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.kv-table thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  text-align: left;
  font-weight: 600;
  font-size: 11px;
  color: var(--p-text-muted-color);
  background: var(--lab-panel-bg);
  padding: 6px 10px;
  border-bottom: 1px solid var(--lab-panel-border);
}
.kv-row {
  cursor: pointer;
  border-bottom: 1px solid var(--lab-panel-border);
}
.kv-row:hover {
  background: var(--lab-bg);
}
.kv-table td {
  padding: 5px 10px;
  vertical-align: top;
}
.col-key { width: 30%; }
.col-key code { font-size: 12px; word-break: break-all; }
.col-rev { width: 4rem; font-variant-numeric: tabular-nums; color: var(--p-text-muted-color); }
.col-time { width: 6rem; font-variant-numeric: tabular-nums; color: var(--p-text-muted-color); }
.value-preview {
  font-family: monospace;
  font-size: 11px;
  color: var(--p-text-muted-color);
  word-break: break-all;
}
.value-full {
  margin: 0;
  font-family: monospace;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--p-text-color);
}
.empty {
  text-align: center;
  padding: 1.25rem 0;
  font-size: 12px;
}
/* ── Feed ── */
.feed {
  flex-shrink: 0;
  height: 30%;
  min-height: 120px;
  display: flex;
  flex-direction: column;
}
.feed h4 {
  margin-bottom: 0.4rem;
}
.feed-sub {
  font-size: 11px;
  font-weight: 400;
}
.feed-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  padding: 0.4rem 0.5rem;
}
.feed-empty {
  font-size: 12px;
  margin: 0.25rem 0;
}
.feed-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.feed-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 12px;
  padding: 2px 0;
}
.feed-key {
  font-size: 12px;
  word-break: break-all;
}
.feed-rev,
.feed-time {
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  margin-left: auto;
}
.feed-time {
  margin-left: 0;
}
@media (max-width: 720px) {
  .kv-inspector {
    flex-direction: column;
  }
  .rail {
    width: auto;
    border-right: none;
    border-bottom: 1px solid var(--lab-panel-border);
  }
}
</style>
