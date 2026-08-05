<script setup>
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'

import { getKvBucketEntries, listKVBuckets } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'
import { parseKvNotifySubject } from '../nats/kvNotifySubject.js'

// KV inspector: every registered KV bucket in a left rail, the selected
// bucket's current contents + live update feed on the right. Phase 23: a
// one-shot GET /api/kv/buckets/{bucket}/entries fetches the current contents
// snapshot, then a notify.*.kv.{bucket}.> subscribe on the tenant NATS
// connection drives the "recent updates" feed — replacing the single
// SSE/WatchAll connection that used to serve both. Cross-context by design
// (matching the old SSE handler): the subscribe wildcards the context token
// since this panel inspects every business unit in the bucket, not just the
// one currently selected in the topbar. Only the selected bucket is
// subscribed at a time, mirroring the old "one connection at a time" intent.
const REFRESH_MS = 15000
const FEED_CAP = 40

// Known bucket-prefix families, longest-match first so "dict-a"/"dict-b" win
// over a bare "dict". Shipping buckets are one per family per tenant; their
// entries, rather than their names, carry the business-unit context prefix.
const FAMILIES = [
  { prefix: 'dict-a', label: 'Shape A — ship read model' },
  { prefix: 'dict-b', label: 'Shape B — ship cache' },
  { prefix: 'container', label: 'Container projection' },
  { prefix: 'meta', label: 'Meta lookup sets' },
  { prefix: 'refdata', label: 'Reference data (refdata-service)' },
]

const OP_SEVERITY = { PUT: 'success', DEL: 'warn', PURGE: 'danger' }

// ── Bucket rail ───────────────────────────────────────────────────────────────
const buckets = ref([]) // [{ bucket, values, history, bytes, ttlSeconds }]
const activeBucket = ref(null)
// Flat is the opening view: it's the literal bucket name as it exists in
// NATS, which is what you want when you're cross-referencing a `nats kv ls`
// or a curl against /api/kv/buckets. Grouped is the guided read for someone
// learning how the families map to the CQRS shapes.
const viewMode = ref('flat')

// All buckets, one level, raw name — no family shorthand.
const flatBuckets = computed(() => [...buckets.value].sort((a, b) => a.bucket.localeCompare(b.bucket)))

function familyOf(name) {
  const fam = FAMILIES.find((f) => name === f.prefix || name.startsWith(f.prefix + '-'))
  return fam ? fam.prefix : 'other'
}

function contextOf(name) {
  const fam = familyOf(name)
  if (fam === 'other') return name
  // The shipping buckets are now exactly their family names (dict-a, dict-b,
  // container, meta), not {family}-{context}. Keep a suffix readable for
  // other services' legacy/non-shipping buckets without inventing a context.
  return name === fam ? name : name.slice(fam.length + 1)
}

// Buckets grouped by family, in FAMILIES order, so the rail reads as the
// pipeline's KV layer rather than an alphabetical dump.
const groups = computed(() => {
  const order = [...FAMILIES.map((f) => f.prefix), 'other']
  const byFamily = {}
  for (const b of buckets.value) {
    const fam = familyOf(b.bucket)
    ;(byFamily[fam] ??= []).push(b)
  }
  return order
    .filter((fam) => byFamily[fam])
    .map((fam) => ({
      family: fam,
      label: FAMILIES.find((f) => f.prefix === fam)?.label ?? 'Other buckets',
      buckets: byFamily[fam].sort((a, b) => a.bucket.localeCompare(b.bucket)),
    }))
})

async function refreshBuckets() {
  let list
  try {
    const res = await listKVBuckets()
    list = res?.buckets ?? []
  } catch {
    return // best-effort; keep last known
  }
  buckets.value = list
  if (!activeBucket.value || !list.some((b) => b.bucket === activeBucket.value)) {
    // Prefer a ship read model as the opening view, else the first bucket.
    const first = list.find((b) => b.bucket === 'dict-a' || b.bucket.startsWith('dict-a-')) ?? list[0]
    activeBucket.value = first?.bucket ?? null
  }
}

const activeStatus = computed(() => buckets.value.find((b) => b.bucket === activeBucket.value))

// ── Selected bucket: contents snapshot + live feed ─────────────────────────────
const { connected: tenantConnected, subscribe } = useNatsConnection()
const entries = reactive(new Map()) // key → { key, value, revision, created }
const feed = ref([]) // live changes only, newest first
const loading = ref(false)
let unsubscribe = null

function resetBucketState() {
  entries.clear()
  feed.value = []
  loading.value = true
}

async function connectBucket(bucket) {
  disconnectBucket()
  resetBucketState()

  if (!tenantConnected.value) return // watch(tenantConnected) below retries once it's up

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

  try {
    const rows = await getKvBucketEntries(bucket)
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

// Retry the active bucket's subscription once the tenant connection comes up
// (covers the mount-order race: activeBucket can be set before connect()
// finishes authenticating).
watch(tenantConnected, (isConnected) => {
  if (isConnected && activeBucket.value) connectBucket(activeBucket.value)
})

// Switch the single live connection whenever the selected bucket changes.
watch(activeBucket, (bucket) => {
  if (bucket) connectBucket(bucket)
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
    <!-- Bucket rail -->
    <aside class="rail" aria-label="KV buckets">
      <div class="rail-mode-toggle" role="tablist" aria-label="Bucket list view">
        <button
          type="button"
          role="tab"
          class="rail-mode-btn"
          :class="{ active: viewMode === 'flat' }"
          :aria-selected="viewMode === 'flat'"
          @click="viewMode = 'flat'"
        >Flat</button>
        <button
          type="button"
          role="tab"
          class="rail-mode-btn"
          :class="{ active: viewMode === 'grouped' }"
          :aria-selected="viewMode === 'grouped'"
          @click="viewMode = 'grouped'"
        >Grouped</button>
      </div>

      <div v-if="viewMode === 'flat'" class="rail-group">
        <button
          v-for="b in flatBuckets"
          :key="b.bucket"
          type="button"
          class="rail-item"
          :class="{ active: b.bucket === activeBucket }"
          @click="activeBucket = b.bucket"
        >
          <code class="rail-name">{{ b.bucket }}</code>
          <span class="rail-count">{{ b.values }}</span>
        </button>
      </div>
      <template v-else>
        <div v-for="group in groups" :key="group.family" class="rail-group">
          <p class="rail-eyebrow">{{ group.label }}</p>
          <button
            v-for="b in group.buckets"
            :key="b.bucket"
            type="button"
            class="rail-item"
            :class="{ active: b.bucket === activeBucket }"
            @click="activeBucket = b.bucket"
          >
            <span class="rail-name">{{ contextOf(b.bucket) }}</span>
            <span class="rail-count">{{ b.values }}</span>
          </button>
        </div>
      </template>
      <p v-if="!buckets.length" class="lab-muted rail-empty">No KV buckets registered yet.</p>
    </aside>

    <!-- Detail -->
    <section v-if="activeBucket" class="detail">
      <header class="detail-head">
        <div class="detail-title">
          <code class="bucket-name">{{ activeBucket }}</code>
          <Tag :severity="tenantConnected ? 'success' : 'danger'" :value="tenantConnected ? 'watching' : 'off'" />
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
          <p v-if="!feed.length" class="lab-muted feed-empty">
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
.rail {
  width: 220px;
  flex-shrink: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding-right: 0.25rem;
  border-right: 1px solid var(--lab-panel-border);
}
.rail-mode-toggle {
  flex-shrink: 0;
  display: flex;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
}
.rail-mode-btn {
  all: unset;
  box-sizing: border-box;
  flex: 1;
  text-align: center;
  cursor: pointer;
  padding: 4px 0;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-muted-color);
}
.rail-mode-btn:hover {
  color: var(--p-text-color);
}
.rail-mode-btn.active {
  background: var(--lab-panel-border);
  color: var(--lab-accent);
}
.rail-mode-btn:focus-visible {
  outline: 2px solid var(--lab-accent);
  outline-offset: -2px;
}
.rail-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.rail-eyebrow {
  margin: 0 0 2px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-muted-color);
}
.rail-item {
  all: unset;
  box-sizing: border-box;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 5px 8px;
  border-radius: 4px;
  border-left: 2px solid transparent;
  font-size: 12px;
  color: var(--p-text-muted-color);
}
.rail-item:hover {
  background: var(--lab-panel-border);
  color: var(--p-text-color);
}
.rail-item.active {
  background: var(--lab-panel-border);
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
    flex-direction: row;
    flex-wrap: wrap;
  }
}
</style>
