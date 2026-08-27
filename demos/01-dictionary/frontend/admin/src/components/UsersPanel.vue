<script setup>
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import { computed, onMounted, onUnmounted, ref } from 'vue'

import { getNatsConnections, getNatsUser, listNatsUsers } from '../api'
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
const REFRESH_MS = 10000
const EXPIRING_MS = 60 * 60 * 1000

const users = ref([])
const connections = ref([])
const connPage = ref({ numConnections: 0, total: 0, offset: 0, limit: 0 })
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
  } catch {
    // Best-effort: /connz is the live-counts half. Losing it must not empty
    // the roster, which is the half that answers "who exists" — so the
    // columns it feeds fall back to zero and the rows stay.
    connections.value = []
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
function healthOf(user) {
  if (user.status === 'pending') return 'pending'
  if (!user.expiresAt) return 'valid'
  const ms = new Date(user.expiresAt).getTime() - now.value
  if (ms <= 0) return 'expired'
  if (ms <= EXPIRING_MS) return 'expiring'
  return 'valid'
}

// ── Summary ──────────────────────────────────────────────────────────────
const connectedCount = computed(() => users.value.filter((u) => liveFor(u).conns > 0).length)
const bearerCount = computed(() => users.value.filter((u) => u.bearer).length)
const expiringCount = computed(() => users.value.filter((u) => healthOf(u) === 'expiring').length)
const connectedFill = computed(() =>
  users.value.length ? Math.round((connectedCount.value / users.value.length) * 10000) / 100 : 0,
)

// ── Filter ───────────────────────────────────────────────────────────────
const searchText = ref('')
const kindOn = ref('all') // 'all' | 'credential' | 'session'

function rowMatches(u) {
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
      </div>
      <div class="summary-card">
        <div class="summary-label">Bearer</div>
        <div class="summary-value" :class="{ flagged: bearerCount > 0 }">{{ bearerCount }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">Expiring &lt; 1h</div>
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
    </DataTable>

    <section v-if="selectedKey" class="detail" data-testid="user-detail">
      <div class="detail-head">
        <span v-if="detail" class="kind-badge" :class="detail.kind">{{ detail.kind }}</span>
        <span class="user-name">{{ detail?.name || '…' }}</span>
        <span v-if="detail" class="meta lab-muted">
          · {{ detail.source }} · issued {{ formatStamp(detail.issuedAt) }}
          · {{ selectedLive.conns }} connection{{ selectedLive.conns === 1 ? '' : 's' }}
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
.health.expiring i,
.health.pending i {
  background: #f0b429;
}
.health.expired i {
  background: #e5484d;
}
.expires {
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--p-text-muted-color);
}
.expires.expiring {
  color: #f0b429;
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
.kv .v.expiring,
.kv .v.scoped {
  color: #f0b429;
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
</style>
