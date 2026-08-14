<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'

import StreamView from './StreamView.vue'
import { listStreams } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'

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
// .account-dot below): whether the browser could watch that account live at
// all, which is a property of the NATS account boundary, not of any one
// stream.
const REFRESH_MS = 15000

const streams = ref([]) // [{ stream, account, subjects, messages, bytes, firstSeq, lastSeq, consumers }]
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

// SHIPPING first within its account (the stream most demos revolve around),
// then alphabetical — purely a display convention, not a registry concept.
// The server already sorts by (account, stream); this only reorders within a
// group, so the rail still never reshuffles between polls.
function sortStreams(list) {
  return [...list].sort((a, b) => {
    if (a.stream === 'SHIPPING') return -1
    if (b.stream === 'SHIPPING') return 1
    return a.stream.localeCompare(b.stream)
  })
}

const groupedByAccount = computed(() => {
  const groups = new Map()
  for (const s of streams.value) {
    if (!groups.has(s.account)) groups.set(s.account, [])
    groups.get(s.account).push(s)
  }
  return [...groups.entries()].map(([account, list]) => [account, sortStreams(list)])
})

async function refresh() {
  let list
  try {
    const res = await listStreams()
    list = res?.streams ?? []
  } catch {
    return // Best-effort — keep showing whatever was last known.
  }
  streams.value = list
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

// Re-fetch immediately on tenant (re)connect rather than waiting up to
// REFRESH_MS. The rail no longer depends on the active tenant for WHICH
// streams show, but a switch can bring a previously-unseen tenant's resources
// into existence server-side (ensureTenantResources), which does change the
// list — and a fresh connection is worth a re-check regardless.
const { connected: tenantConnected, tenant } = useNatsConnection()
watch(tenantConnected, (isConnected) => {
  if (isConnected) refresh()
})

const hasStreams = computed(() => streams.value.length > 0)
</script>

<template>
  <div class="jetstream-view">
    <aside class="rail" aria-label="Streams, grouped by account">
      <div class="rail-summary">
        <strong>{{ streams.length }}</strong> streams
        <span class="lab-muted">· {{ groupedByAccount.length }} accounts</span>
      </div>

      <div v-for="[account, accountStreams] in groupedByAccount" :key="account" class="account-group">
        <button
          type="button"
          class="account-head"
          :class="{ collapsed: collapsedAccounts.has(account) }"
          :aria-expanded="!collapsedAccounts.has(account)"
          @click="toggleAccount(account)"
        >
          <span class="caret">▶</span>
          <span class="account-dot" :class="{ ro: account !== tenant }"></span>
          <span class="account-name">{{ account }}</span>
          <span class="account-kind">{{ account === 'platform' ? 'read-only' : 'tenant' }}</span>
          <span class="account-count lab-muted">{{ accountStreams.length }}</span>
        </button>
        <div v-if="!collapsedAccounts.has(account)" class="account-body">
          <button
            v-for="s in accountStreams"
            :key="account + '::' + s.stream"
            type="button"
            class="rail-item"
            :class="{ active: s.account === activeAccount && s.stream === activeStream }"
            @click="selectStream(s.account, s.stream)"
          >
            <code class="rail-name">{{ s.stream }}</code>
            <span class="rail-count">{{ s.messages }}</span>
          </button>
        </div>
      </div>

      <p v-if="!hasStreams" class="lab-muted rail-empty">No streams registered on the server yet.</p>
    </aside>

    <StreamView
      v-if="activeStream"
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
/* Green = the account the browser's own NATS connection is authenticated as,
   the only one whose notify.* live tail it could ever subscribe to; gray =
   every other account, which this panel reads backend-mediated snapshots
   from. Same distinction StreamView's header tag makes, shown one level up. */
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
