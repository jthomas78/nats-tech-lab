<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'

import StreamView from './StreamView.vue'
import { listStreams } from '../api'

// A rail of every stream actually registered across every NATS account this
// backend reaches — not a user-managed open set, and deliberately not scoped
// to the topbar's tenant selector, which only ever showed the active tenant's
// single SHIPPING stream and hid PLATFORM's REFDATA/RPCTRACE entirely.
// Grouped by account, mirroring KvInspector.vue's bucket rail.
//
// Stream names collide across accounts (every tenant provisions its own
// SHIPPING), so the rail keys selection on {account, stream}, not stream
// alone — see listStreams' doc comment server-side. Only the selected
// stream's StreamView is mounted: it holds a one-shot REST replay fetch
// (Phase 23) re-run on every mount, so switching the selection drops the old
// snapshot and fetches the new one fresh with nothing lost.
//
// No per-row "connected" state in the rail: a JetStream stream doesn't have a
// live-connection concept the way a NATS client does — it's either registered
// on the server or it isn't, and listStreams() already only returns
// registered ones. The account-level dot is a different claim (see
// .account-dot below): the owning account's own active/suspended lifecycle
// status from accounts-service, not anything about the browser's own
// connection.
const REFRESH_MS = 15000

const streams = ref([]) // [{ stream, account, subjects, messages, bytes, firstSeq, lastSeq, consumers }]
// Accounts is the authoritative account list (every account this backend
// knows about, including ones whose streams couldn't be listed — e.g. a
// suspended tenant, whose cross-account $JS.API access always fails). The
// rail is built from this, not from `streams`, so a suspended account with
// zero listable streams still gets a dimmed group header instead of
// disappearing entirely.
const accounts = ref([]) // [{ name, status }]
const activeAccount = ref(null)
const activeStream = ref(null)

// Which account groups are collapsed — absence means expanded, so every
// account starts open (the whole point of this rail is to see everything at
// once; collapsing is an opt-in way to tuck an account away, not a default
// that hides streams from view).
const collapsedAccounts = reactive(new Set())
function toggleAccount(account) {
  if (collapsedAccounts.has(account)) collapsedAccounts.delete(account)
  else collapsedAccounts.add(account)
}

// ── Kind (38e) ────────────────────────────────────────────────────────
// Every JetStream stream now arrives tagged "stream" | "kv" | "objstore"
// (see streams.go's streamKind). KV and Object Store rows used to be
// dropped server-side; they are shown here so the rail can answer "how
// much of this account's MaxStreams: 10 is spent", which ADR-048 budgets
// against and nothing else surfaces. They are opt-in rather than default
// because on an ordinary day the event streams are what you came to look
// at, and PLATFORM alone carries ~7 KV backing streams.
const showKinds = reactive({ kv: false, objstore: false })
function toggleKind(kind) {
  showKinds[kind] = !showKinds[kind]
}

// The kind tag renders only when the list is actually mixed. With both
// toggles off every visible row is a plain stream, so a "STREAM" tag on
// each would be decoration — same argument AccountsOverviewPanel.vue makes
// for not shipping a permanent "0 slow" tile.
const showKindTags = computed(() => showKinds.kv || showKinds.objstore)

// NATS' own backing-stream prefixes. Stripped for display only once the
// kind tag carries the same information — the raw `stream` field stays the
// selection key and the name $JS.API is addressed by.
function displayName(stream) {
  return stream.replace(/^KV_/, '').replace(/^OBJ_/, '')
}

const searchText = ref('')
const filtering = computed(() => searchText.value.trim().length > 0)

// Match against the displayed name, not the raw one: with the prefix
// hidden, typing "kv" and matching KV_* rows the user cannot see the
// prefix of would look like a bug.
function matches(s, q) {
  return displayName(s.stream).toLowerCase().includes(q)
}

const kindVisible = computed(() => streams.value.filter((s) => s.kind !== 'kv' && s.kind !== 'objstore' ? true : showKinds[s.kind]))
const visibleStreams = computed(() => {
  const q = searchText.value.trim().toLowerCase()
  return q ? kindVisible.value.filter((s) => matches(s, q)) : kindVisible.value
})

// What the toggles are currently withholding, named rather than silently
// absent — a rail that just gets shorter reads as data having gone away.
const hiddenNote = computed(() => {
  const parts = []
  for (const [kind, label] of [['kv', 'KV'], ['objstore', 'objstore']]) {
    if (showKinds[kind]) continue
    const n = streams.value.filter((s) => s.kind === kind).length
    if (n) parts.push(`${n} ${label}`)
  }
  return parts.length ? `${parts.join(' · ')} hidden` : ''
})

// Splits a name around the search match so the matched run can be marked.
function nameParts(stream) {
  const name = displayName(stream)
  const q = searchText.value.trim()
  if (!q) return [name, '', '']
  const at = name.toLowerCase().indexOf(q.toLowerCase())
  if (at < 0) return [name, '', '']
  return [name.slice(0, at), name.slice(at, at + q.length), name.slice(at + q.length)]
}

// SHIPPING first within its account (the stream most demos revolve around),
// then alphabetical — purely a display convention, not a registry concept.
// The server already sorts by (account, stream); this only reorders within a
// group, so the rail still never reshuffles between polls.
// Event streams sort above kv/objstore rows: those are infrastructure the
// toggles opted into, not the thing the panel is for.
const KIND_ORDER = { stream: 0, objstore: 1, kv: 2 }
function sortStreams(list) {
  return [...list].sort((a, b) => {
    const ka = KIND_ORDER[a.kind] ?? 0
    const kb = KIND_ORDER[b.kind] ?? 0
    if (ka !== kb) return ka - kb
    if (a.stream === 'SHIPPING') return -1
    if (b.stream === 'SHIPPING') return 1
    return a.stream.localeCompare(b.stream)
  })
}

const groupedByAccount = computed(() => {
  const byAccount = new Map()
  for (const s of visibleStreams.value) {
    if (!byAccount.has(s.account)) byAccount.set(s.account, [])
    byAccount.get(s.account).push(s)
  }
  const groups = accounts.value.map((a) => [a.name, a.status, sortStreams(byAccount.get(a.name) ?? [])])
  // While filtering, an account with no match drops out entirely rather
  // than sitting as an empty header: at 250px a column of empty groups
  // would crowd out the one match. Unfiltered, an empty account still gets
  // its header — a suspended tenant with zero listable streams is a fact
  // worth showing, not an absence.
  return filtering.value ? groups.filter(([, , list]) => list.length) : groups
})

// A collapsed account auto-expands while a filter is active, otherwise a
// search would silently hide its own results behind a caret. The user's
// own collapse choices survive in collapsedAccounts and come back the
// moment the search clears — filtering borrows the caret, never overwrites
// it.
function isCollapsed(account) {
  return !filtering.value && collapsedAccounts.has(account)
}

async function refresh() {
  let res
  try {
    res = await listStreams()
  } catch {
    return // Best-effort — keep showing whatever was last known.
  }
  const list = res?.streams ?? []
  streams.value = list
  accounts.value = res?.accounts ?? []
  const stillExists = list.some((s) => s.account === activeAccount.value && s.stream === activeStream.value)
  if (!activeStream.value || !stillExists) {
    // Prefer a SHIPPING stream as the opening view, else the first stream.
    const first = list.find((s) => s.stream === 'SHIPPING') ?? list[0]
    activeAccount.value = first?.account ?? null
    activeStream.value = first?.stream ?? null
  }
}

function selectStream(account, stream) {
  activeAccount.value = account
  activeStream.value = stream
}

const activeStatus = computed(() =>
  streams.value.find((s) => s.account === activeAccount.value && s.stream === activeStream.value),
)

let refreshTimer = null
onMounted(() => {
  refresh()
  refreshTimer = setInterval(refresh, REFRESH_MS)
})
onUnmounted(() => clearInterval(refreshTimer))

const hasAccounts = computed(() => accounts.value.length > 0)

const activeKind = computed(() => activeStatus.value?.kind ?? 'stream')

const totalVisibleKinds = computed(() => kindVisible.value.length)
</script>

<template>
  <div class="jetstream-view">
    <aside
      class="rail"
      aria-label="Streams, grouped by account"
    >
      <div
        class="rail-summary"
        :class="{ filtering }"
      >
        <template v-if="filtering">
          <strong>{{ visibleStreams.length }}</strong> of {{ totalVisibleKinds }} streams
          <span class="lab-muted">· {{ groupedByAccount.length }} of {{ accounts.length }} accounts</span>
        </template>
        <template v-else>
          <strong>{{ visibleStreams.length }}</strong> streams
          <span class="lab-muted">· {{ groupedByAccount.length }} accounts</span>
        </template>
      </div>

      <span class="search-box">
        <i class="pi pi-search" />
        <input
          v-model="searchText"
          type="text"
          placeholder="filter by stream name"
          aria-label="Filter streams by name"
        >
      </span>

      <!-- Two independent opt-in chips, not the joined segmented control
           TraceWaterfall.vue uses for rest/nats: that is one axis with three
           states, this is two additive booleans layered on always-visible
           event streams, where both-on is a meaningful fourth state. -->
      <div
        class="kind-toggles"
        role="group"
        aria-label="Include storage-backing streams"
      >
        <button
          v-for="[kind, label] in [['kv', 'kv'], ['objstore', 'objstore']]"
          :key="kind"
          type="button"
          class="chip"
          :data-k="kind"
          :class="{ on: showKinds[kind] }"
          :aria-pressed="showKinds[kind]"
          @click="toggleKind(kind)"
        >
          <i />{{ label }}
        </button>
      </div>

      <div
        v-for="[account, accountStatus, accountStreams] in groupedByAccount"
        :key="account"
        class="account-group"
      >
        <button
          type="button"
          class="account-head"
          :class="{ collapsed: isCollapsed(account) }"
          :aria-expanded="!isCollapsed(account)"
          @click="toggleAccount(account)"
        >
          <span class="caret">▶</span>
          <span
            class="account-dot"
            :class="{ ro: accountStatus !== 'active' }"
            :title="`account status: ${accountStatus}`"
          />
          <span class="account-name">{{ account }}</span>
          <span class="account-kind">{{ account === 'platform' ? 'read-only' : 'tenant' }}</span>
          <span class="account-count lab-muted">{{ accountStreams.length }}</span>
        </button>
        <div
          v-if="!isCollapsed(account)"
          class="account-body"
        >
          <button
            v-for="s in accountStreams"
            :key="account + '::' + s.stream"
            type="button"
            class="rail-item"
            :class="{ active: s.account === activeAccount && s.stream === activeStream, tagged: showKindTags }"
            @click="selectStream(s.account, s.stream)"
          >
            <span
              v-if="showKindTags"
              class="kind-tag"
              :class="s.kind"
            >{{ s.kind === 'objstore' ? 'obj' : s.kind }}</span>
            <code class="rail-name">{{ nameParts(s.stream)[0]
            }}<span
              v-if="nameParts(s.stream)[1]"
              class="mark"
            >{{ nameParts(s.stream)[1] }}</span>{{ nameParts(s.stream)[2] }}</code>
            <span class="rail-count">{{ s.messages }}</span>
          </button>
        </div>
      </div>

      <p
        v-if="hiddenNote"
        class="hidden-note"
      >
        {{ hiddenNote }}
      </p>
      <p
        v-if="filtering && !visibleStreams.length"
        class="hidden-note"
      >
        No stream name matches “{{ searchText.trim() }}”.
      </p>
      <p
        v-if="!hasAccounts"
        class="lab-muted rail-empty"
      >
        No streams registered on the server yet.
      </p>
    </aside>

    <!-- StreamView renders raw stream messages, which for a KV or Object
         Store backing stream is revision/chunk plumbing, not something a
         reader can interpret. Until those rows route to KvInspector and a
         real object listing, say so rather than showing a misleading
         message list. -->
    <div
      v-if="activeStream && activeKind !== 'stream'"
      class="detail kind-placeholder"
    >
      <code>{{ activeStream }}</code>
      <p>
        This is the backing stream for
        {{ activeKind === 'kv' ? 'a KV bucket' : 'an Object Store' }}, shown here so it counts
        toward the account's stream budget. Its messages are
        {{ activeKind === 'kv' ? 'key revisions' : 'file chunks' }} — inspect it via
        {{ activeKind === 'kv' ? 'the KV Buckets panel' : 'the owning service' }} instead.
      </p>
    </div>

    <StreamView
      v-else-if="activeStream"
      :key="activeAccount + '::' + activeStream"
      :account="activeAccount"
      :stream="activeStream"
      :status="activeStatus"
      class="detail"
    />
  </div>
</template>

<style scoped>
.jetstream-view {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 0.75rem;
}
/* ── Rail — matches KvInspector.vue's bucket rail exactly, so the two
   "pick one thing from a list, inspect it on the right" panels in this
   admin app read as one visual pattern. ── */
/* ── search — the .search-box idiom from ConnectionsPanel.vue / RpcPanel.vue
   (third copy of it in this app; worth hoisting into the shared theme if a
   fourth appears). ── */
.search-box {
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
  font: inherit;
  font-size: 11px;
}
.search-box .pi-search {
  font-size: 10px;
}
.kind-toggles {
  display: flex;
  gap: 5px;
}
.chip {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 9.5px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border: 1px solid var(--lab-panel-border);
  border-radius: 3px;
  background: transparent;
  color: var(--p-text-disabled-color);
  padding: 1px 7px 1px 5px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
}
.chip i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  opacity: 0.45;
}
.chip.on i {
  opacity: 1;
}
.chip.on[data-k='kv'] {
  color: #f0b429;
  background: rgba(240, 180, 41, 0.12);
  border-color: rgba(240, 180, 41, 0.4);
}
.chip.on[data-k='objstore'] {
  color: #a78bfa;
  background: rgba(167, 139, 250, 0.14);
  border-color: rgba(167, 139, 250, 0.45);
}
/* Same left-border-plus-tint vocabulary as TraceWaterfall.vue's rest/nats
   .kind-tag, so a kind marker reads identically wherever it appears.
   Colors avoid --lab-accent (means "selected" in this rail) and the
   account tags above are neutral-outlined, so the two never blur. */
.kind-tag {
  flex: none;
  display: inline-flex;
  align-items: center;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  padding: 1px 5px 1px 4px;
  border-radius: 2px;
}
.kind-tag.stream {
  color: #2dd4bf;
  background: rgba(45, 212, 191, 0.12);
  border-left: 2px solid #2dd4bf;
}
.kind-tag.objstore {
  color: #a78bfa;
  background: rgba(167, 139, 250, 0.14);
  border-left: 2px solid #a78bfa;
}
.kind-tag.kv {
  color: #f0b429;
  background: rgba(240, 180, 41, 0.12);
  border-left: 2px solid #f0b429;
}
.mark {
  color: var(--p-text-color);
  background: rgba(0, 111, 255, 0.22);
  border-radius: 2px;
}
.hidden-note {
  margin: 0;
  font-size: 10.5px;
  color: var(--p-text-disabled-color);
  padding: 1px 3px 0;
}
.rail-summary.filtering strong {
  color: var(--lab-accent);
}
.kind-placeholder {
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
.kind-placeholder code {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--p-text-color);
}
.kind-placeholder p {
  margin: 0;
  color: var(--p-text-muted-color);
  max-width: 62ch;
}
.rail {
  width: 250px;
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
.account-group {
  display: flex;
  flex-direction: column;
  /* The rail is a scrolling flex column, so a group MUST NOT shrink: without
     this, expanding every account (or widening the kind filter to KV +
     OBJSTORE) makes each group compress to fit the rail's height instead of
     overflowing it, and `overflow: hidden` below then clips the last rows
     with nothing to scroll to. */
  flex-shrink: 0;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  overflow: hidden;
}
/* The account band is the rail's one piece of visual weight: it separates
   "which NATS account" (a hard, server-enforced boundary) from "which
   stream" (a choice within it), so the two never read as one flat list. */
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
   own status, not anything about the browser's current connection — a
   suspended tenant still lists its streams here, just dimmed. */
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
/* With a kind tag present, the tag itself is the row's left anchor, so the
   22px indent that normally stands in for nesting is redundant — it just
   pushes every tag away from the rail edge. Pull the row back to the same
   inset as its right padding and tighten the tag-to-name gap. */
.rail-item.tagged {
  padding-left: 7px;
  gap: 6px;
}
.rail-item.tagged .rail-name {
  flex: 1;
  min-width: 0;
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
.detail {
  flex: 1;
  min-width: 0;
}
@media (max-width: 720px) {
  .jetstream-view {
    flex-direction: column;
  }
  .rail {
    width: auto;
    border-right: none;
    border-bottom: 1px solid var(--lab-panel-border);
  }
}
</style>
