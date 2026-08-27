<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import {
  getNatsClosedConnections,
  getNatsConnections,
  getNatsUser,
  listNatsUsers,
  revokeNatsUser,
} from '../api'
import NKey from './NKey.vue'

// Users panel (Phase 50c, BR-060) — the roster of NATS users this stack has,
// credentials and browser sessions alike, with a drill-in on one user's
// claim permissions.
//
// Two sources, deliberately different in kind:
//
//   · the ROSTER is accounts-service's user registry (BR-AC38), read over
//     api._platform.accounts.user.list.v1. It is the authority on who exists,
//     because nothing in an operator-mode NATS stack stores a user — the
//     resolver holds account JWTs only, and a user JWT is verified by
//     signature and never written down. Without the registry this panel
//     could not have rows at all.
//   · the CONNECTION COUNTS are /connz, the same snapshot the Connections
//     panel reads. They are joined onto a roster row by user NKey
//     (connz `userKey` — see BR-058), which is the only stable identity a
//     credential has: two users can share a `name`.
//
// The join lives here rather than in accounts-service on purpose (Phase 50b):
// the Admin UI already fetches /connz through observability-service's proxy,
// so joining in the browser costs nothing, while doing it server-side would
// give accounts-service an HTTP-monitor dependency it has no other use for.
//   · the LAST OUTCOME (Phase 51a, BR-062) is /connz's *closed* ring, joined
//     on the same NKey. It answers the question the live counts cannot: an
//     idle credential and a credential being refused every few seconds both
//     read `0` connections, and only the closed ring's `reason` separates
//     them.
const REFRESH_MS = 10000
// Two thresholds, not one (2026-08-27). A single "expiring" band an hour wide
// said the same thing about a token with 59 minutes left and one with 30
// seconds left, and in this stack the second is about to disappear while the
// first is simply doing its job — the browser sessions carry short TTLs and
// re-mint on their own, so an hour-wide warning fires on healthy rows all day
// and stops being read. `scheduled` is the "this will renew shortly" band and
// `expiring` is the "this is going now" one.
const SCHEDULED_MS = 15 * 60 * 1000
const EXPIRING_MS = 2 * 60 * 1000

const users = ref([])
const connections = ref([])
const connPage = ref({ numConnections: 0, total: 0, offset: 0, limit: 0 })
// Tracked separately from `connections` being empty, because the hide-idle
// filter below has to tell "nothing is connected" from "we do not know what is
// connected". An empty array is both, and acting on the second as if it were
// the first would empty the roster the moment /connz blinked.
const connsAvailable = ref(true)
const closedConnections = ref([])
const closedPage = ref({ numConnections: 0, total: 0, offset: 0, limit: 0 })
const loading = ref(true)
const errorMsg = ref('')

// `now` is reactive and re-stamped on every refresh, so the health column and
// the countdown in Expires move on their own rather than freezing at whatever
// the clock said when the panel mounted.
const now = ref(Date.now())

async function refresh() {
  now.value = Date.now()
  try {
    users.value = await listNatsUsers()
    errorMsg.value = ''
  } catch (err) {
    errorMsg.value = err.message || 'Failed to load users'
  } finally {
    loading.value = false
  }
  try {
    const res = await getNatsConnections()
    connections.value = res?.connections ?? []
    connPage.value = res?.page ?? { numConnections: 0, total: 0, offset: 0, limit: 0 }
    connsAvailable.value = true
  } catch {
    connsAvailable.value = false
    // Best-effort: /connz is the live-counts half. Losing it must not empty
    // the roster, which is the half that answers "who exists" — so the
    // columns it feeds fall back to zero and the rows stay.
    connections.value = []
  }
  try {
    const res = await getNatsClosedConnections()
    closedConnections.value = res?.connections ?? []
    closedPage.value = res?.page ?? { numConnections: 0, total: 0, offset: 0, limit: 0 }
  } catch {
    // Same best-effort contract as the live half above, and for a stronger
    // reason: the closed ring is an overlay on the roster, so losing it costs
    // one column rather than the table.
    closedConnections.value = []
  }
}

let timer = null
onMounted(() => {
  refresh()
  timer = setInterval(refresh, REFRESH_MS)
})
onUnmounted(() => clearInterval(timer))

// ── /connz join ──────────────────────────────────────────────────────────
// Keyed on the user NKey, never on the name (BR-058: two distinct users can
// carry the same name, and the browser session credentials mint a fresh NKey
// per session).
const connsByUserKey = computed(() => {
  const out = {}
  for (const c of connections.value) {
    if (!c.userKey) continue
    const e = (out[c.userKey] ||= { conns: 0, subs: 0, cids: [] })
    e.conns += 1
    e.subs += c.subscriptions || 0
    e.cids.push(c.cid)
  }
  return out
})
function liveFor(user) {
  return connsByUserKey.value[user.publicKey] || { conns: 0, subs: 0, cids: [] }
}

// /connz answers one PAGE, and its `limit` is that page's SIZE, not a server
// capacity (the connz_limit_is_page_size_not_capacity caveat, and the same
// reading ConnectionsPanel applies). When it pages, a row's Conns/Subs may be
// short — a connection on the unread page counts as zero here — so the panel
// says so rather than presenting a partial join as a complete one.
const connsPartial = computed(() => {
  const p = connPage.value
  const shown = p.numConnections || connections.value.length
  return (p.limit || 0) > 0 && (p.offset || 0) + shown < (p.total || 0)
})

// ── Last outcome join (BR-062) ───────────────────────────────────────────
// Same key as the live join above — the user NKey, never the name. The server
// returns the ring most-recently-stopped first, so the FIRST match for a key
// is that credential's latest outcome and no sort is needed here.
const lastClosedByUserKey = computed(() => {
  const out = {}
  for (const c of closedConnections.value) {
    if (!c.userKey || out[c.userKey]) continue
    out[c.userKey] = c
  }
  return out
})
function outcomeFor(user) {
  return lastClosedByUserKey.value[user.publicKey] || null
}
// A refusal is the case this column exists for, so it is the one that gets a
// colour. The reason string is the server's own and is never interpreted
// beyond this test — an unrecognised reason renders plainly rather than being
// mapped to a state the server did not report.
//
// `Authentication Expired` is excluded, and the exclusion has to come FIRST
// because it matches the refusal pattern textually (Phase 52, 2026-08-27). It
// is the server's word for a session whose own TTL ran out — the ordinary,
// expected end of every browser session in this stack, and by volume the
// commonest reason in the closed ring. Grouping it with `Authentication
// Failure` was wrong twice over: it coloured a routine expiry as a problem,
// and — because `isQuietlyIdle` treats a refusal as never-idle so that
// BR-062's rows survive the filter — it made `hide idle` unable to hide the
// single largest class of dead rows. That went unnoticed while `hide expired`
// defaulted ON and took those rows first; flipping that default (BR-AC44)
// exposed it as 41 visible rows where 8 were connected.
function isRefusal(reason) {
  const r = reason || ''
  if (/Expired/i.test(r)) return false
  return /Authentication|Authorization|Revoked/i.test(r)
}
// The closed ring pages exactly like the live list, and under-reporting an
// outcome is worse than under-reporting a count: "no recent failure" is the
// reassuring answer, and it must not be produced by a page boundary.
const closedPartial = computed(() => {
  const p = closedPage.value
  const shown = p.numConnections || closedConnections.value.length
  return (p.limit || 0) > 0 && (p.offset || 0) + shown < (p.total || 0)
})

// ── Health (BR-060) ──────────────────────────────────────────────────────
// Health reads the user's own record — its status and its expiry — and NEVER
// its connection count. A credential that nothing is currently using is not
// unhealthy; it is a valid credential sitting on disk, which is the normal
// resting state of most rows here. That is also why the design mockup's
// "unused" state is absent: "never connected" is a fact about /connz, and
// admitting it here would make Health mean two incompatible things at once.
// The Conns column already says zero.
//
// `pending` is the BR-AC38 state: the registry recorded the mint, and the
// signature's outcome is unknown to the issuer. It is shown, not hidden.
//
// `revoked` (Phase 51b, BR-AC43) outranks every other state, including
// `pending` and expiry: once a credential's key is in its account's
// revocation list, what its `exp` claim says has stopped mattering. It reads
// the registry row, not a connection, so it does not reintroduce the
// "liveness is not identity" mistake this rule's second clause forbids.
//
// The expiry bands are ordered narrowest-first because they nest: everything
// inside EXPIRING_MS is also inside SCHEDULED_MS, so testing the wide band
// first would swallow the narrow one and `expiring` would never be reached.
//
//   valid      no expiry, or more than 15m left
//   scheduled  15m or less
//   expiring   2m or less
//   expired    the moment `exp` passes
function healthOf(user) {
  if (user.revokedAt) return 'revoked'
  if (user.status === 'pending') return 'pending'
  if (!user.expiresAt) return 'valid'
  const ms = new Date(user.expiresAt).getTime() - now.value
  if (ms <= 0) return 'expired'
  if (ms <= EXPIRING_MS) return 'expiring'
  if (ms <= SCHEDULED_MS) return 'scheduled'
  return 'valid'
}
// Both live bands count as one number: the tile answers "is anything about to
// go away", and splitting it into two small numbers would make neither worth
// a glance.
const EXPIRY_SOON = ['scheduled', 'expiring']

// ── Summary ──────────────────────────────────────────────────────────────
const connectedCount = computed(() => users.value.filter((u) => liveFor(u).conns > 0).length)
const bearerCount = computed(() => users.value.filter((u) => u.bearer).length)
const expiringCount = computed(() => users.value.filter((u) => EXPIRY_SOON.includes(healthOf(u))).length)
const connectedFill = computed(() =>
  users.value.length ? Math.round((connectedCount.value / users.value.length) * 10000) / 100 : 0,
)

// ── Filter ───────────────────────────────────────────────────────────────
const searchText = ref('')
const kindOn = ref('all') // 'all' | 'credential' | 'session'

// Hide-expired defaults OFF as of Phase 52 (2026-08-27), reversing the default
// it shipped with earlier the same day.
//
// It defaulted ON because nearly every row was a dead browser session: short
// TTLs, nothing deleting them, accumulating for as long as the stack had been
// up — 44 expired out of 56 — so the unfiltered roster was mostly rubble with
// the six credentials that mattered buried in it. Phase 52's reaper removes
// the rubble at the source (BR-AC44), which bounds the expired set at one
// retention window rather than at the stack's whole uptime. At that size the
// expired rows are no longer noise: a session that died inside the last day is
// the row an operator is reading when they ask why a tab dropped, and hiding
// it by default would be hiding the useful part.
//
// The checkbox stays, because a window is a policy and 24h is not everyone's.
// It remains a filter and not a deletion: the summary tiles above still count
// every user, and the toggle says how many rows it is holding back.
const hideExpired = ref(false)

// Hide-idle also defaults ON, and unlike hide-expired it reads /connz — which
// makes it the one filter here that can be WRONG rather than merely selective,
// so it suspends itself whenever the count it filters on is not trustworthy:
//
//   · /connz failed        — every row reads 0 and the filter would empty the
//                            whole roster, breaking BR-060's "a /connz failure
//                            must not empty the roster" outright.
//   · /connz paged         — a connection on an unread page counts as 0 here,
//                            so a genuinely-connected row would be hidden with
//                            nothing on screen saying so. That is the exact lie
//                            the amber paged note exists to prevent, and
//                            hiding the row is a louder version of it.
//
// Suspended means suspended, not inverted: the checkbox stays checked and says
// why it is not acting, rather than silently unchecking itself.
const hideIdle = ref(true)
const idleFilterActive = computed(() => hideIdle.value && connsAvailable.value && !connsPartial.value)

// "Idle" means quiet, NOT merely zero — and the difference is the whole point
// of BR-062. A credential being refused every few seconds also reads `0`
// connections, and it is the single row an operator most needs to see; hiding
// it would make this checkbox cancel out the column next to it. So a row whose
// last retained outcome was a refusal is never idle, however few connections
// it has.
function isQuietlyIdle(u) {
  if (liveFor(u).conns > 0) return false
  return !isRefusal(outcomeFor(u)?.reason)
}

function rowMatches(u) {
  if (hideExpired.value && healthOf(u) === 'expired') return false
  if (idleFilterActive.value && isQuietlyIdle(u)) return false
  if (kindOn.value !== 'all' && u.kind !== kindOn.value) return false
  if (!searchText.value) return true
  const q = searchText.value.toLowerCase()
  return (
    (u.name || '').toLowerCase().includes(q) ||
    (u.account || '').toLowerCase().includes(q) ||
    (u.publicKey || '').toLowerCase().includes(q) ||
    (u.source || '').toLowerCase().includes(q)
  )
}
// Credentials first, then sessions, each alphabetical: the credential set is
// the stack's fixed furniture and the sessions churn under it, so a stable
// block on top is easier to read than one list re-ordering every 10s.
// Both counts are HELD-BACK counts, and each chip renders its own only while
// it is actually holding rows back (Phase 52). Showing "51" beside an
// unchecked box reads as "51 hidden" when nothing is hidden — the same lie in
// the other direction as a hidden row that says nothing about being hidden.
const expiredCount = computed(() => users.value.filter((u) => healthOf(u) === 'expired').length)
// Counted the way the filter actually acts — an expired row hidden by the
// other checkbox is not also "held back for being idle", or the two counts
// would sum to more than the rows missing from the table.
const idleCount = computed(() =>
  users.value.filter(
    (u) => isQuietlyIdle(u) && !(hideExpired.value && healthOf(u) === 'expired'),
  ).length,
)

const KIND_ORDER = { credential: 0, session: 1 }
const rows = computed(() =>
  users.value
    .filter(rowMatches)
    .slice()
    .sort(
      (a, b) =>
        (KIND_ORDER[a.kind] ?? 9) - (KIND_ORDER[b.kind] ?? 9) ||
        (a.name || '').localeCompare(b.name || ''),
    ),
)

// ── Selection / drill-in ────────────────────────────────────────────────
// The roster carries no permissions (BR-AC40 — a list is a roster), so
// selecting a row is a second call, not a lookup in what we already have.
const selectedKey = ref(null)
// `selection` has to be bound for a selected row to STAY highlighted:
// PrimeVue's DataTable keeps no selection state of its own — it only emits
// `update:selection` — so an unbound table renders nothing but :hover, which
// reads as the selection vanishing the moment the pointer moves into the
// detail pane below. Bound here to our own `selectedKey`, so the highlight
// and the pane are driven by one piece of state and clear together.
const selectedRosterRow = computed(() => rows.value.find((r) => r.publicKey === selectedKey.value) || null)
const detail = ref(null)
const detailError = ref('')
const detailLoading = ref(false)

async function selectRow(row) {
  const selection = window.getSelection()
  if (selection && selection.toString().length > 0) return
  if (selectedKey.value === row.publicKey) return closeDetail()
  selectedKey.value = row.publicKey
  detail.value = null
  detailError.value = ''
  detailLoading.value = true
  try {
    const view = await getNatsUser(row.publicKey)
    // Discard a reply that arrived after the operator moved on, rather than
    // painting it over the row they are now looking at.
    if (selectedKey.value === row.publicKey) detail.value = view
  } catch (err) {
    if (selectedKey.value === row.publicKey) detailError.value = err.message || 'Failed to load claims'
  } finally {
    detailLoading.value = false
  }
}
function closeDetail() {
  selectedKey.value = null
  detail.value = null
  detailError.value = ''
}
const selectedLive = computed(() => (detail.value ? liveFor(detail.value) : { conns: 0, subs: 0, cids: [] }))

// ── Claim permissions (BR-AC41) ─────────────────────────────────────────
// One flat list of {effect, dir, subject} rows, because that is how an
// operator reads a permission grid — by subject, not by which of four nested
// arrays it came out of.
function permRows(perms) {
  if (!perms) return []
  const out = []
  for (const [dir, p] of [
    ['pub', perms.pub],
    ['sub', perms.sub],
  ]) {
    for (const s of p?.allow ?? []) out.push({ effect: 'ALLOW', dir, subject: s })
    for (const s of p?.deny ?? []) out.push({ effect: 'DENY', dir, subject: s })
  }
  return out
}
const effectiveRows = computed(() => permRows(detail.value?.effective))
// Populated by the service ONLY when a scoped signing key discarded them
// (BR-AC41), so the panel never has to decide for itself whether the JWT's
// own grants apply — if they are here, they do not.
const discardedRows = computed(() => permRows(detail.value?.jwtGrants))

// `subs`/`data`/`payload` carry omitempty, so an absent field and an explicit
// -1 both mean unlimited; 0 is never a meaningful limit here.
function limitLabel(v) {
  return v === undefined || v === null || v < 0 ? 'unlimited' : v.toLocaleString()
}
function byteLabel(v) {
  if (v === undefined || v === null || v < 0) return 'unlimited'
  if (v >= 1024 * 1024) return `${Math.round((v / (1024 * 1024)) * 10) / 10} MiB`
  if (v >= 1024) return `${Math.round((v / 1024) * 10) / 10} KiB`
  return `${v} B`
}
const limits = computed(() => {
  const p = detail.value?.effective
  const resp = p?.resp
  return {
    subs: limitLabel(p?.subs),
    payload: byteLabel(p?.payload),
    data: byteLabel(p?.data),
    connTypes: (p?.allowed_connection_types ?? []).join(', ') || 'any',
    resp: resp ? `${resp.max} msgs · ${resp.ttl ? `${Math.round(resp.ttl / 1e9)}s` : 'no ttl'}` : 'none',
  }
})

// ── Revocation (Phase 51b, BR-AC43) ──────────────────────────────────────
// Offered for a long-lived credential that is not already revoked, and for
// nothing else: a session dies on its own TTL faster than an operator could
// find its row, so a control to revoke one would be a cluster-wide account
// JWT re-sign spent on something already expiring.
function canRevoke(user) {
  return !!user && user.kind === 'credential' && !user.revokedAt
}

// The pending confirmation, held as the row itself so the dialog can name the
// credential and read its live connection count.
const revokeTarget = ref(null)
const revoking = ref(false)
const revokeError = ref('')

function askRevoke(user) {
  if (!canRevoke(user)) return
  revokeError.value = ''
  revokeTarget.value = user
}
function cancelRevoke() {
  revokeTarget.value = null
  revokeError.value = ''
}
async function confirmRevoke() {
  const user = revokeTarget.value
  if (!user) return
  revoking.value = true
  revokeError.value = ''
  try {
    await revokeNatsUser(user.publicKey)
    revokeTarget.value = null
    // Re-read rather than patching the row locally: the registry is the
    // mirror of the account JWT (BR-AC42), and a panel that stamped its own
    // row would be asserting an outcome it did not observe.
    await refresh()
    if (detail.value?.publicKey === user.publicKey) {
      detail.value = await getNatsUser(user.publicKey)
    }
  } catch (err) {
    revokeError.value = err.message || 'Failed to revoke credential'
  } finally {
    revoking.value = false
  }
}

// ── Formatting ───────────────────────────────────────────────────────────
function formatStamp(iso) {
  if (!iso) return '—'
  return new Date(iso).toLocaleString([], { dateStyle: 'short', timeStyle: 'medium' })
}
// "never" is the honest word for a credential with no `exp` claim — it is not
// a missing value, it is a JWT that does not expire.
function expiresLabel(user) {
  if (!user.expiresAt) return 'never'
  const ms = new Date(user.expiresAt).getTime() - now.value
  if (ms <= 0) return 'expired'
  const mins = Math.floor(ms / 60000)
  if (mins < 60) return `in ${mins}m`
  const hrs = Math.floor(mins / 60)
  if (hrs < 48) return `in ${hrs}h ${mins % 60}m`
  return `in ${Math.floor(hrs / 24)}d`
}
</script>

<template>
  <div class="users-panel">
    <div class="summary-row">
      <div class="summary-card">
        <div class="summary-label">Users</div>
        <div class="summary-value">{{ users.length }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Connected</div>
        <div class="summary-value">{{ connectedCount }} <span class="of">/ {{ users.length }}</span></div>
        <div class="gauge">
          <div class="gauge-rail"><div class="gauge-fill" :style="{ width: connectedFill + '%' }" /></div>
        </div>
        <div v-if="connsPartial" class="paged-note" title="/connz answered one page of connections, so a row's Conns/Subs may be short — a connection on an unread page counts as zero here.">
          partial · /connz paged
        </div>
        <!-- BR-062: the same caveat on the closed ring, and it matters more —
             "no recent failure" is the reassuring answer, so it must not be
             produced by a page boundary. -->
        <div v-if="closedPartial" class="paged-note" data-testid="closed-paged" title="/connz answered one page of closed connections, so a row's last outcome may be missing — an outcome on an unread page reads as none here.">
          partial · closed ring paged
        </div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Bearer</div>
        <div class="summary-value" :class="{ flagged: bearerCount > 0 }">{{ bearerCount }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Expiring &lt; 15m</div>
        <div class="summary-value" :class="{ warn: expiringCount > 0 }">{{ expiringCount }}</div>
      </div>
    </div>

    <div class="users-toolbar">
      <span class="search-box">
        <i class="pi pi-search" />
        <input v-model="searchText" type="text" placeholder="filter by name, account, nkey, or source" />
      </span>
      <button
        v-for="opt in ['all', 'credential', 'session']"
        :key="opt"
        type="button"
        class="chip"
        :class="{ on: kindOn === opt }"
        @click="kindOn = opt"
      >{{ opt }}</button>
      <label class="chip chip-check chip-check--first" :class="{ on: hideExpired }">
        <input v-model="hideExpired" type="checkbox" data-testid="hide-expired" />
        hide expired
        <span v-if="hideExpired && expiredCount" class="chip-count" data-testid="hide-expired-count">{{ expiredCount }}</span>
      </label>
      <label
        class="chip chip-check"
        :class="{ on: idleFilterActive, suspended: hideIdle && !idleFilterActive }"
        :title="hideIdle && !idleFilterActive ? 'Not filtering: the live connection counts are unavailable or incomplete, so a row reading 0 may still be connected.' : null"
      >
        <input v-model="hideIdle" type="checkbox" data-testid="hide-idle" />
        hide idle
        <span v-if="hideIdle && !idleFilterActive" class="chip-count" data-testid="hide-idle-suspended">suspended</span>
        <span v-else-if="idleFilterActive && idleCount" class="chip-count" data-testid="hide-idle-count">{{ idleCount }}</span>
      </label>
    </div>

    <p v-if="errorMsg" class="err-line">{{ errorMsg }}</p>

    <DataTable
      :value="rows"
      :loading="loading"
      size="small"
      scrollable
      scroll-height="flex"
      class="users-table"
      data-key="publicKey"
      selectionMode="single"
      :metaKeySelection="false"
      :selection="selectedRosterRow"
      @row-click="selectRow($event.data)"
    >
      <template #empty>
        <span class="lab-muted">No users recorded.</span>
      </template>
      <Column header="Health" style="width:96px">
        <template #body="{ data }">
          <span class="health" :class="healthOf(data)" data-testid="health"><i />{{ healthOf(data) }}</span>
        </template>
      </Column>
      <Column header="Name" bodyClass="pair-cell" style="width:210px">
        <template #body="{ data }">
          <!-- The full user NKey used to hang off this cell's title. BR-061:
               it renders in the cell instead, elided — same fact, no longer 56
               characters one hover deep. -->
          <span class="user-name">{{ data.name || '(unnamed)' }}</span>
          <NKey :value="data.publicKey" class="cell-nkey" />
        </template>
      </Column>
      <Column header="Account" bodyClass="pair-cell" style="width:190px">
        <template #body="{ data }">
          <!-- The account NKey was on this cell's title too (BR-061). Same
               treatment as the Credential cell in ConnectionsPanel: the
               friendly name, with the key elided beneath it. -->
          <span v-if="data.account" class="tenant-label">{{ data.account }}</span>
          <NKey v-if="data.account && data.accountKey" :value="data.accountKey" class="cell-nkey" />
          <span v-else class="lab-muted">—</span>
        </template>
      </Column>
      <Column header="Kind" style="width:96px">
        <template #body="{ data }">
          <span class="kind-badge" :class="data.kind">{{ data.kind }}</span>
        </template>
      </Column>
      <Column header="Source" style="width:96px">
        <template #body="{ data }"><span class="src">{{ data.source || '—' }}</span></template>
      </Column>
      <!-- Bearer stays a column even though every row in this stack reads
           `no`: its whole job is to make a `yes` conspicuous if one appears,
           since a bearer JWT authenticates on its own with no NKey challenge. -->
      <Column header="Bearer" style="width:70px">
        <template #body="{ data }">
          <span v-if="data.bearer" class="bearer-yes">yes</span>
          <span v-else class="lab-muted">no</span>
        </template>
      </Column>
      <Column header="Expires" style="width:110px">
        <template #body="{ data }">
          <span
            class="expires"
            :class="healthOf(data)"
            :title="data.expiresAt ? formatStamp(data.expiresAt) : 'this credential carries no exp claim'"
          >{{ expiresLabel(data) }}</span>
        </template>
      </Column>
      <Column header="Conns" style="width:64px" bodyClass="num-cell">
        <template #body="{ data }">{{ liveFor(data).conns }}</template>
      </Column>
      <Column header="Subs" style="width:64px" bodyClass="num-cell">
        <template #body="{ data }">{{ liveFor(data).subs }}</template>
      </Column>
      <!-- Last outcome (BR-062). Shown only while the credential has no live
           connections: for a connected one its last closed connection is
           history, and the Conns column is the answer. -->
      <Column header="Last outcome" style="width:170px">
        <template #body="{ data }">
          <span v-if="liveFor(data).conns > 0" class="lab-muted">—</span>
          <template v-else-if="outcomeFor(data)">
            <span
              class="outcome"
              :class="{ refused: isRefusal(outcomeFor(data).reason) }"
              data-testid="outcome"
            >{{ outcomeFor(data).reason || 'closed' }}</span>
            <!-- The reason is worth showing for a session too — a session
                 being refused is the same bug as a credential being refused.
                 The TIMESTAMP is not: a session's last stop is a fact about a
                 15-minute mint that was always going to end. -->
            <span
              v-if="data.kind === 'credential' && outcomeFor(data).stop"
              class="outcome-stop"
              data-testid="outcome-stop"
            >{{ formatStamp(outcomeFor(data).stop) }}</span>
          </template>
          <span
            v-else
            class="lab-muted outcome-none"
            data-testid="outcome-none"
            title="The server keeps a bounded ring of recently-closed connections. No row for this credential means none is retained — not that it has never disconnected."
          >outside the retained window</span>
        </template>
      </Column>
    </DataTable>

    <section v-if="selectedKey" class="detail" data-testid="user-detail">
      <div class="detail-head">
        <span v-if="detail" class="kind-badge" :class="detail.kind">{{ detail.kind }}</span>
        <span class="user-name">{{ detail?.name || '…' }}</span>
        <span v-if="detail" class="meta lab-muted">
          · {{ detail.source }} · issued {{ formatStamp(detail.issuedAt) }}
          · {{ selectedLive.conns }} connection{{ selectedLive.conns === 1 ? '' : 's' }}
        </span>
        <!-- Revocation lives in the pane, not on the row: it is terminal and
             irreversible, so it should cost a deliberate drill-in rather than
             sit one stray click away in a dense table. -->
        <button
          v-if="detail && canRevoke(detail)"
          class="revoke-btn"
          data-testid="revoke"
          title="Write this credential's key into its account's revocation list. This cannot be undone."
          @click.stop="askRevoke(detail)"
        >
          Revoke
        </button>
        <span v-else-if="detail?.revokedAt" class="revoked-note" data-testid="revoked-note">
          revoked {{ formatStamp(detail.revokedAt) }}
        </span>
        <span class="close" title="Close" @click="closeDetail">✕</span>
      </div>
      <p v-if="detailError" class="err-line detail-err">{{ detailError }}</p>
      <p v-else-if="detailLoading" class="lab-muted detail-err">loading claims…</p>
      <div v-else-if="detail" class="panes">
        <div class="pane">
          <div class="pane-title">User</div>
          <div class="pane-body">
            <div class="kv">
              <div class="row"><span class="k">Name</span><span class="v">{{ detail.name }}</span></div>
              <div class="row">
                <span class="k">Account</span>
                <span class="v"><span v-if="detail.account" class="tenant-label">{{ detail.account }}</span><span v-else class="lab-muted">—</span></span>
              </div>
              <div class="row"><span class="k">Account NKey</span><span class="v"><NKey :value="detail.accountKey" copyable /></span></div>
              <div class="row"><span class="k">User NKey</span><span class="v"><NKey :value="detail.publicKey" copyable /></span></div>
              <!-- The issuer is a key too: an account signing key, or a scoped
                   signing key. Same rule, same treatment. -->
              <div class="row"><span class="k">Issuer</span><span class="v"><NKey :value="detail.issuerKey" copyable /></span></div>
              <div class="row"><span class="k">Status</span><span class="v">{{ detail.status }}</span></div>
              <div class="row"><span class="k">Issued</span><span class="v">{{ formatStamp(detail.issuedAt) }}</span></div>
              <div class="row">
                <span class="k">Expires</span>
                <span class="v" :class="healthOf(detail)">{{ detail.expiresAt ? formatStamp(detail.expiresAt) : 'never' }}</span>
              </div>
              <div class="row"><span class="k">Bearer</span><span class="v">{{ detail.bearer ? 'yes' : 'no' }}</span></div>
              <div class="row">
                <span class="k">Scoped key</span>
                <span class="v" :class="{ scoped: detail.scoped }">
                  {{ detail.scoped ? `yes — template applies${detail.scopeRole ? ` (${detail.scopeRole})` : ''}` : 'no — JWT permissions apply' }}
                </span>
              </div>
            </div>
          </div>
        </div>
        <div class="pane">
          <div class="pane-title">
            {{ detail.scoped ? 'Effective permissions' : 'Claim permissions' }}
            <span class="count">({{ effectiveRows.length }})</span>
          </div>
          <div class="pane-body">
            <!-- The one thing this pane must never do is present grants it
                 could not verify as the ones the server enforces. When
                 resolution failed, accounts-service says why (BR-AC41) and
                 the panel repeats it verbatim above the table. -->
            <p v-if="detail.unresolved" class="unresolved" data-testid="unresolved">{{ detail.unresolved }}</p>
            <p v-if="detail.scoped" class="override" data-testid="override">
              Enforced from the account signing key's template.
              <b>The JWT's own permissions are discarded by the server</b> — they are shown
              struck through below, so a mismatch is visible rather than silent.
            </p>
            <table v-if="effectiveRows.length || discardedRows.length" class="claims">
              <thead><tr><th>Effect</th><th>Dir</th><th>Subject</th></tr></thead>
              <tbody>
                <tr v-for="(p, i) in effectiveRows" :key="`e${i}`">
                  <td class="eff" :class="p.effect.toLowerCase()">{{ p.effect }}</td>
                  <td class="dir">{{ p.dir }}</td>
                  <td class="subject">{{ p.subject }}</td>
                </tr>
                <tr v-for="(p, i) in discardedRows" :key="`d${i}`" class="struck" data-testid="discarded">
                  <td class="eff">{{ p.effect }}</td>
                  <td class="dir">{{ p.dir }}</td>
                  <td class="subject">{{ p.subject }} <span class="lab-muted">— in JWT, not enforced</span></td>
                </tr>
              </tbody>
            </table>
            <span v-else class="lab-muted no-perms">no permissions recorded</span>
            <div v-if="effectiveRows.length" class="limits">
              <span>Max subs <b>{{ limits.subs }}</b></span>
              <span>Payload <b>{{ limits.payload }}</b></span>
              <span>Data <b>{{ limits.data }}</b></span>
              <span>Connection types <b>{{ limits.connTypes }}</b></span>
              <span>Response permissions <b>{{ limits.resp }}</b></span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Revocation confirmation (BR-AC43). It names the credential and its
         live connection count because those are the two facts that decide
         whether this is the right key: a name alone is not unique (BR-058),
         and "3 connections" is what tells an operator something is using it
         right now. -->
    <div v-if="revokeTarget" class="revoke-overlay" data-testid="revoke-confirm">
      <div class="revoke-dialog">
        <div class="revoke-title">Revoke this credential?</div>
        <div class="revoke-body">
          <div class="kv">
            <div class="row"><span class="k">Credential</span><span class="v"><span class="user-name">{{ revokeTarget.name || '(unnamed)' }}</span></span></div>
            <div class="row"><span class="k">Account</span><span class="v"><span v-if="revokeTarget.account" class="tenant-label">{{ revokeTarget.account }}</span><span v-else class="lab-muted">—</span></span></div>
            <div class="row"><span class="k">User NKey</span><span class="v"><NKey :value="revokeTarget.publicKey" /></span></div>
            <div class="row">
              <span class="k">Live now</span>
              <span class="v" :class="{ warn: liveFor(revokeTarget).conns > 0 }" data-testid="revoke-live">
                {{ liveFor(revokeTarget).conns }} connection{{ liveFor(revokeTarget).conns === 1 ? '' : 's' }}
              </span>
            </div>
          </div>
          <p class="revoke-warn">
            This writes the key into the account JWT's revocation list and pushes it to the
            server. <b>It cannot be undone</b> — recovery is minting a replacement credential,
            and anything still using this one stops working immediately.
          </p>
          <p v-if="revokeError" class="err-line" data-testid="revoke-error">{{ revokeError }}</p>
        </div>
        <div class="revoke-actions">
          <button class="btn-plain" :disabled="revoking" @click="cancelRevoke">Cancel</button>
          <button class="btn-danger" data-testid="revoke-confirm-btn" :disabled="revoking" @click="confirmRevoke">
            {{ revoking ? 'Revoking…' : 'Revoke credential' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.users-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

/* ── summary cards (ConnectionsPanel's idiom, unchanged) ── */
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
.summary-value .of {
  color: var(--p-text-disabled-color);
  font-size: 13px;
}
.summary-value.warn {
  color: #f0b429;
}
/* A non-zero bearer count is the one number in this row that is a finding
   rather than a reading — see the Bearer column's comment. */
.summary-value.flagged {
  color: #f0b429;
}
.gauge {
  margin-top: 6px;
}
.gauge-rail {
  height: 2px;
  border-radius: 1px;
  background: var(--lab-panel-border);
}
.gauge-fill {
  height: 100%;
  min-width: 4px;
  border-radius: 1px;
  background: var(--lab-accent);
}
.paged-note {
  margin-top: 5px;
  font-size: 10px;
  line-height: 12px;
  color: #f0b429;
  cursor: help;
}

/* ── toolbar ── */
.users-toolbar {
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
/* The hide-expired toggle is a chip so it sits in the same row as the kind
   filters without a second visual language — but it is a real checkbox, not a
   button pretending to be one, because it is the only control in the toolbar
   that is on by default and a checkbox is the affordance that says so. */
.chip-check {
  user-select: none;
}
.chip-check--first {
  margin-left: auto;
}
/* A suspended filter is neither on nor off, and must not look like either:
   the amber is the same tone the paged note uses, because it is the same
   fact — the join under this column is incomplete. */
.chip-check.suspended {
  border-color: #f0b429;
  color: #f0b429;
}
.chip-check input {
  width: 11px;
  height: 11px;
  margin: 0;
  accent-color: var(--lab-accent);
  cursor: pointer;
}
.chip-count {
  font-variant-numeric: tabular-nums;
  opacity: 0.7;
}
.err-line {
  flex: none;
  margin: 0;
  font-size: 12px;
  color: #e5484d;
}

/* ── table ── */
.users-table {
  flex: 1;
  min-height: 0;
}
.users-table :deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}

/* The selected row keeps the accent tint and the inset bar for as long as the
   detail pane below is open — it is the pane's anchor, not a hover state, so
   it must survive the pointer leaving the table. Explicit rather than left to
   Aura's default highlight, which is a flat fill with no left marker and does
   not read as "this is the row the pane is showing". */
.users-table :deep(.p-datatable-tbody > tr.p-datatable-row-selected > td) {
  background: rgba(0, 111, 255, 0.08);
}
.users-table :deep(.p-datatable-tbody > tr.p-datatable-row-selected > td:first-child) {
  box-shadow: inset 2px 0 0 var(--lab-accent, #006fff);
}
.users-table :deep(.p-datatable-tbody > tr > td) {
  padding-top: 3px;
  padding-bottom: 3px;
}
.users-table :deep(.num-cell) {
  font-variant-numeric: tabular-nums;
  color: var(--p-text-muted-color);
}
.user-name {
  font-weight: 500;
}
.tenant-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--lab-accent);
  background: rgba(0, 111, 255, 0.1);
  border-radius: 3px;
  padding: 1px 6px;
}
.src {
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.kind-badge {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  font-weight: 700;
  border-radius: 3px;
  padding: 0 5px;
  line-height: 15px;
  display: inline-block;
  color: #4cc2ff;
  background: rgba(56, 178, 255, 0.12);
}
.kind-badge.session {
  color: #b18cff;
  background: rgba(148, 101, 255, 0.13);
}
.bearer-yes {
  font-weight: 600;
  color: #f0b429;
}

/* ── health ── */
.health {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.health i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #27c07f;
}
/* Three warning tones, not one, so the bands are separable at a glance without
   reading the word: scheduled is the same yellow `pending` and the Expires
   column already use, expiring steps to orange on its way to expired's red. */
.health.scheduled i,
.health.pending i {
  background: #f0b429;
}
.health.expiring i {
  background: #f5820b;
}
.health.expiring {
  color: #f5820b;
}
.health.expired i {
  background: #e5484d;
}
/* Revoked shares the red the expired dot uses — both mean "this credential
   does not work" — but the label is dimmed to a struck-through weight so the
   two are distinguishable without reading the word. */
.health.revoked i {
  background: #e5484d;
}
.health.revoked {
  color: #e5484d;
}
.expires {
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--p-text-muted-color);
}
.expires.scheduled {
  color: #f0b429;
}
.expires.expiring {
  color: #f5820b;
}
.expires.expired {
  color: #e5484d;
}

/* ── detail split (ConnectionsPanel's pane geometry) ── */
.detail {
  flex: none;
  height: 44%;
  min-height: 220px;
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
.detail-err {
  padding: 8px 10px;
  font-size: 11px;
}
.panes {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.15fr);
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
  grid-template-columns: 104px 1fr;
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
.kv .v.scheduled,
.kv .v.scoped {
  color: #f0b429;
}
.kv .v.expiring {
  color: #f5820b;
}
.kv .v.expired {
  color: #e5484d;
}

/* ── claims ── */
.unresolved,
.override {
  margin: 4px 0 6px;
  font-size: 11px;
  line-height: 15px;
  color: #f0b429;
  background: rgba(240, 180, 41, 0.08);
  border-left: 2px solid #f0b429;
  padding: 4px 8px;
  border-radius: 0 3px 3px 0;
}
/* Beside the friendly value it identifies, not under it — see
   ConnectionsPanel's copy of this for the reasoning. .pair-cell keeps the two
   halves on one line; the columns carrying a pair are sized to hold both. */
.cell-nkey {
  margin-left: 6px;
}
:deep(td.pair-cell) {
  white-space: nowrap;
}
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
  vertical-align: top;
}
.claims tbody tr:nth-child(even) {
  background: rgba(255, 255, 255, 0.02);
}
.claims .eff {
  font-weight: 700;
  color: var(--p-text-disabled-color);
}
.claims .eff.allow {
  color: #27c07f;
}
.claims .eff.deny {
  color: #e5484d;
}
.claims .dir {
  color: var(--p-text-muted-color);
}
.claims .subject {
  overflow-wrap: anywhere;
}
/* A grant the server discards. Struck through AND dimmed — the strike alone
   is easy to miss at 11px, and this row means the opposite of the ones above
   it. */
.claims tr.struck td {
  text-decoration: line-through;
  color: var(--p-text-disabled-color);
}
.claims tr.struck .eff {
  color: var(--p-text-disabled-color);
}
.limits {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 8px;
  padding-top: 6px;
  border-top: 1px solid var(--lab-panel-border);
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.limits b {
  color: var(--p-text-color);
  font-weight: 600;
}
.no-perms {
  font-size: 11px;
}

/* ── last outcome (BR-062) ── */
.outcome {
  display: block;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
/* Only a refusal gets a colour. Every other reason is a normal end to a
   connection and colouring them all would leave the one that matters
   competing with a column of amber. */
.outcome.refused {
  color: #e5484d;
}
.outcome-stop {
  display: block;
  font-size: 9px;
  color: #8a9099;
  font-variant-numeric: tabular-nums;
}
.outcome-none {
  font-size: 11px;
}

/* ── revocation (BR-AC43) ── */
.revoke-btn {
  margin-left: auto;
  padding: 3px 10px;
  font: inherit;
  font-size: 11px;
  color: #e5484d;
  background: transparent;
  border: 1px solid rgba(229, 72, 77, 0.45);
  border-radius: 4px;
  cursor: pointer;
}
.revoke-btn:hover {
  background: rgba(229, 72, 77, 0.12);
}
.revoked-note {
  margin-left: auto;
  font-size: 11px;
  color: #e5484d;
}
.revoke-overlay {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.55);
}
.revoke-dialog {
  width: 460px;
  max-width: calc(100vw - 32px);
  background: #1a1e23;
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5);
}
.revoke-title {
  padding: 12px 16px;
  font-size: 13px;
  font-weight: 600;
  border-bottom: 1px solid var(--p-content-border-color);
}
.revoke-body {
  padding: 14px 16px;
}
.revoke-warn {
  margin: 12px 0 0;
  font-size: 11px;
  line-height: 17px;
  color: #f0b429;
}
.revoke-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--p-content-border-color);
}
.btn-plain,
.btn-danger {
  padding: 5px 12px;
  font: inherit;
  font-size: 12px;
  border-radius: 4px;
  cursor: pointer;
}
.btn-plain {
  color: var(--p-text-color);
  background: transparent;
  border: 1px solid var(--p-content-border-color);
}
.btn-danger {
  color: #fff;
  background: #c1373b;
  border: 1px solid #c1373b;
}
.btn-danger:disabled,
.btn-plain:disabled {
  opacity: 0.55;
  cursor: default;
}
.revoke-body .v.warn {
  color: #f0b429;
}
</style>
