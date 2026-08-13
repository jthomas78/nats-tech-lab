<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Menu from 'primevue/menu'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useToast } from 'primevue/usetoast'
import { computed, onMounted, reactive, ref } from 'vue'

import { createAccount, createBusinessUnit, getAccountsUsage, listAccounts, listBusinessUnits, reactivateAccount, suspendAccount, updateAccountLimits, updateBusinessUnit } from '../api'
import { useTenantStore } from '../stores/tenant'

// Phase 14c — dynamic tenant provisioning via accounts-service. Distinct
// from the topbar tenant selector (stores/tenant.js): that picks which
// *existing* account shipping-service connects as; this page creates and
// revokes the accounts themselves.

const tenantStore = useTenantStore()
const toast = useToast()

const accounts = ref([])
const loading = ref(false)
const error = ref('')

const createOpen = ref(false)
const creating = ref(false)
const createError = ref('')
const form = reactive({
  name: '',
  jsMaxMem: 256 * 1024 * 1024,
  jsMaxFile: 1024 * 1024 * 1024,
  jsMaxStreams: 10,
  jsMaxConsumers: 20,
})

const credsOpen = ref(false)
const mintedCreds = ref('')
const mintedName = ref('')

const usage = ref({}) // keyed by account name — JSUsage from GET /api/accounts/usage

const editOpen = ref(false)
const editSaving = ref(false)
const editError = ref('')
const editAccount = ref(null)
const editForm = reactive({ jsMaxMem: 0, jsMaxFile: 0, jsMaxStreams: 0, jsMaxConsumers: 0 })

// Phase 22: Business unit management
const expandedRows = ref([])
const busByAccount = ref({}) // accountName → BusinessUnit[]
const buLoading = ref({})   // accountName → bool

// New-BU form state (BR-AC26): Name is the free-text display label; Context
// is the immutable {context} subject token, auto-derived from Name but
// editable up until submit. addBUContextTouched tracks whether the operator
// has hand-edited Context, so typing further into Name stops overwriting it —
// the same "slug follows title until you touch the slug" pattern used for
// URL-slug fields elsewhere.
const addBUOpen = ref(false)
const addBUAccount = ref('')
const addBUName = ref('')
const addBUContext = ref('')
const addBUContextTouched = ref(false)
const addBUSaving = ref(false)
const addBUError = ref('')

// Mirrors accounts-service's contextPattern (BR-AC27) — lowercase letters,
// digits and hyphens, alphanumeric at both ends. Kept in sync by hand since
// this is the one validation rule duplicated client-side for instant
// feedback; the server is still the source of truth and re-validates.
const CONTEXT_PATTERN = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/
const CONTEXT_MAX_LEN = 48

function slugify(text) {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

// Mirrors accounts-service's DeriveContext: tenant-prefixed unless the name
// already leads with the tenant, so "Acme Pacific Fleet" under tenant "acme"
// doesn't come out "acme-acme-pacific-fleet".
function deriveContext(tenant, name) {
  const tenantSlug = slugify(tenant)
  const nameSlug = slugify(name)
  if (!nameSlug) return tenantSlug
  if (!tenantSlug) return nameSlug
  if (nameSlug === tenantSlug || nameSlug.startsWith(tenantSlug + '-')) return nameSlug
  return `${tenantSlug}-${nameSlug}`
}

const addBUContextError = computed(() => {
  if (!addBUContext.value) return ''
  if (addBUContext.value.length > CONTEXT_MAX_LEN) return `Must be ${CONTEXT_MAX_LEN} characters or fewer`
  if (!CONTEXT_PATTERN.test(addBUContext.value)) {
    return 'Lowercase letters, digits and hyphens only — must start and end with a letter or digit'
  }
  return ''
})

function onAddBUNameInput() {
  if (!addBUContextTouched.value) {
    addBUContext.value = deriveContext(addBUAccount.value, addBUName.value)
  }
}

function onAddBUContextInput() {
  addBUContextTouched.value = true
}

// Hide-default-BU confirmation dialog (BR-AC17)
const hideDefaultBUOpen = ref(false)
const hideDefaultBUAccount = ref('')
const hideDefaultBUContext = ref('')
const hideDefaultBUSaving = ref(false)

async function loadBUs(accountName, silent = false) {
  if (!silent) buLoading.value = { ...buLoading.value, [accountName]: true }
  try {
    busByAccount.value = { ...busByAccount.value, [accountName]: await listBusinessUnits(accountName) }
  } catch {
    /* best-effort */
  } finally {
    if (!silent) buLoading.value = { ...buLoading.value, [accountName]: false }
  }
}

async function onRowExpand(event) {
  const name = event.data.name
  if (!isReserved(name)) await loadBUs(name)
}

function openAddBU(accountName) {
  addBUAccount.value = accountName
  addBUName.value = ''
  addBUContext.value = ''
  addBUContextTouched.value = false
  addBUError.value = ''
  addBUOpen.value = true
}

async function submitAddBU() {
  if (!addBUName.value || addBUContextError.value) return
  addBUSaving.value = true
  addBUError.value = ''
  try {
    await createBusinessUnit(addBUAccount.value, { name: addBUName.value, context: addBUContext.value })
    addBUOpen.value = false
    await loadBUs(addBUAccount.value)
    // Show the hide-default confirmation if this is the first real BU
    const bus = busByAccount.value[addBUAccount.value] ?? []
    const realBUs = bus.filter((b) => !b.isDefault)
    if (realBUs.length === 1) {
      const defaultBU = bus.find((b) => b.isDefault)
      hideDefaultBUAccount.value = addBUAccount.value
      hideDefaultBUContext.value = defaultBU?.context ?? ''
      hideDefaultBUOpen.value = true
    }
    toast.add({ severity: 'success', summary: 'Business unit registered', detail: addBUName.value, life: 3000 })
  } catch (e) {
    addBUError.value = e.message
  } finally {
    addBUSaving.value = false
  }
}

async function toggleBUVisible(accountName, bu) {
  const snapshot = busByAccount.value[accountName] ?? []
  // Optimistic: flip the flag immediately so the icon updates without a flicker
  busByAccount.value = {
    ...busByAccount.value,
    [accountName]: snapshot.map(b => b.context === bu.context ? { ...b, visible: !b.visible } : b),
  }
  try {
    await updateBusinessUnit(accountName, bu.context, { visible: !bu.visible })
    await loadBUs(accountName, true) // silent — no loading spinner, list already looks right
  } catch (e) {
    busByAccount.value = { ...busByAccount.value, [accountName]: snapshot } // revert on error
    toast.add({ severity: 'error', summary: 'Failed to update visibility', detail: e.message, life: 5000 })
  }
}

async function hideDefaultBU() {
  hideDefaultBUSaving.value = true
  try {
    await updateBusinessUnit(hideDefaultBUAccount.value, hideDefaultBUContext.value, { visible: false })
    await loadBUs(hideDefaultBUAccount.value)
    hideDefaultBUOpen.value = false
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to hide the default business unit', detail: e.message, life: 5000 })
  } finally {
    hideDefaultBUSaving.value = false
  }
}

// Reserved accounts (platform, sys) are the fixed crosscutting accounts
// this deployment can't run without — surfacing them first orients the
// reader before the open-ended, growing list of tenant accounts below.
const sortedAccounts = computed(() =>
  [...accounts.value].sort((a, b) => Number(isReserved(b.name)) - Number(isReserved(a.name)))
)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [accs, usageList] = await Promise.all([
      listAccounts(),
      getAccountsUsage().catch(() => []),
    ])
    accounts.value = accs
    const map = {}
    for (const u of usageList) map[u.name] = u
    usage.value = map
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function openCreate() {
  createError.value = ''
  form.name = ''
  form.jsMaxMem = 256 * 1024 * 1024
  form.jsMaxFile = 1024 * 1024 * 1024
  form.jsMaxStreams = 10
  form.jsMaxConsumers = 20
  createOpen.value = true
}

async function submitCreate() {
  if (!form.name) return
  creating.value = true
  createError.value = ''
  try {
    const res = await createAccount({ ...form })
    createOpen.value = false
    mintedName.value = res.account.name
    mintedCreds.value = res.creds
    credsOpen.value = true
    await load()
    // The dropdown accounts-service just made switchable — refresh so an
    // operator doesn't need to reload the page to see it.
    await tenantStore.refresh()
    toast.add({ severity: 'success', summary: 'Account created', detail: res.account.name, life: 3000 })
  } catch (e) {
    createError.value = e.message
  } finally {
    creating.value = false
  }
}

async function suspend(name) {
  try {
    await suspendAccount(name)
    await load()
    await tenantStore.refresh()
    toast.add({ severity: 'success', summary: 'Account suspended', detail: name, life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to suspend account', detail: e.message, life: 5000 })
  }
}

// BR-AC04 (BUSINESS_RULES-ACCOUNTS.md): reactivating restores the account
// under its original public key/limits and, when the account has its own
// signing key on record, mints a fresh one-time .creds — accounts-service
// omits `creds` for the handful of seeded pre-existing accounts that have no
// stored signing key, so that case is surfaced as an info toast instead of
// the creds dialog.
async function reactivate(name) {
  try {
    const res = await reactivateAccount(name)
    await load()
    await tenantStore.refresh()
    if (res.creds) {
      mintedName.value = res.account.name
      mintedCreds.value = res.creds
      credsOpen.value = true
    } else {
      toast.add({ severity: 'success', summary: 'Account reactivated', detail: name, life: 3000 })
    }
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to reactivate account', detail: e.message, life: 5000 })
  }
}

function openEdit(acc) {
  editAccount.value = acc
  editForm.jsMaxMem = acc.jsMaxMem
  editForm.jsMaxFile = acc.jsMaxFile
  editForm.jsMaxStreams = acc.jsMaxStreams
  editForm.jsMaxConsumers = acc.jsMaxConsumers
  editError.value = ''
  editOpen.value = true
}

async function submitEdit() {
  editSaving.value = true
  editError.value = ''
  try {
    await updateAccountLimits(editAccount.value.name, { ...editForm })
    editOpen.value = false
    await load()
    toast.add({ severity: 'success', summary: 'Limits updated', detail: editAccount.value.name, life: 3000 })
  } catch (e) {
    editError.value = e.message
  } finally {
    editSaving.value = false
  }
}

// Mirrors accounts-service's reservedAccountNames (BR-AC06,
// BUSINESS_RULES-ACCOUNTS.md) — PLATFORM/SYS can never be minted through
// POST /api/accounts and are never switchable tenants, so the table marks
// them distinctly rather than presenting them as an ordinary customer row.
const RESERVED_NAMES = new Set(['platform', 'sys'])
function isReserved(name) {
  return RESERVED_NAMES.has(name?.toLowerCase())
}

// BR-AC16/BR-AC17: explains what the auto-created placeholder is and why it
// can disappear — the "reserved" tag alone doesn't say either.
const DEFAULT_BU_TOOLTIP =
  'Auto-created so this account always has at least one business unit. Add a real one and you can hide this placeholder.'

// JetStream Limits split (mem/file/streams/consumers) — each column shows
// live used/limit from GET /api/accounts/usage, same used/limit + threshold
// pattern the old single "Streams" column already used.
const USAGE_DIMS = {
  mem: { label: 'Memory', bytes: true },
  file: { label: 'Disk', bytes: true },
  streams: { label: 'Streams', bytes: false },
  consumers: { label: 'Consumers', bytes: false },
}

function usageLabel(name, dim) {
  const u = usage.value[name]
  if (!u) return '–'
  const c = u[dim]
  const fmt = USAGE_DIMS[dim].bytes ? formatBytes : (n) => n
  return `${fmt(c.used)} / ${fmt(c.limit)}`
}

function usageClass(name, dim) {
  const u = usage.value[name]
  if (!u) return 'usage-na'
  const c = u[dim]
  if (!c.limit) return 'usage-na'
  const ratio = c.used / c.limit
  if (ratio >= 1) return 'usage-crit'
  if (ratio >= 0.8) return 'usage-warn'
  return 'usage-ok'
}

function formatBytes(n) {
  if (!n) return '0'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v % 1 === 0 ? v : v.toFixed(1)} ${units[i]}`
}

function formatDate(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
}

onMounted(load)

// ── Per-row overflow menu: Edit Limits · Suspend/Reactivate (RefData's
// CategoryTypeList.vue pattern — standardizing row actions across the admin
// apps). Add stays a top-of-table button, unlike Edit/Suspend/Reactivate.
const rowMenu = ref()
const menuAccount = ref(null)

function openRowMenu(event, acc) {
  menuAccount.value = acc
  rowMenu.value.toggle(event)
}

const rowMenuItems = computed(() => {
  const acc = menuAccount.value
  if (!acc) return []
  const items = [{ label: 'Edit Limits', icon: 'pi pi-sliders-h', command: () => openEdit(acc) }]
  if (isReserved(acc.name)) return items
  if (acc.status === 'active') {
    items.push({ label: 'Suspend', icon: 'pi pi-ban', command: () => suspend(acc.name) })
  } else if (acc.status === 'suspended') {
    items.push({ label: 'Reactivate', icon: 'pi pi-play', command: () => reactivate(acc.name) })
  }
  return items
})
</script>

<template>
  <div class="lab-panel accounts-panel">
    <div class="panel-header">
      <span class="panel-title">Accounts</span>
      <div class="header-actions">
        <Button icon="pi pi-refresh" text rounded size="small" :loading="loading" aria-label="Refresh" @click="load" />
        <Button label="Create Account" icon="pi pi-plus" size="small" @click="openCreate" />
      </div>
    </div>

    <p class="lab-muted description">
      Mints a new NATS account at runtime via decentralized JWTs (Phase 14) — no <code>nats.conf</code> edit,
      no server restart. Appears in the tenant selector's dropdown immediately after creation.
    </p>

    <p v-if="error" class="error-text">{{ error }}</p>

    <DataTable
      v-model:expanded-rows="expandedRows"
      :value="sortedAccounts"
      size="small"
      paginator
      :rows="10"
      class="accounts-table"
      :row-class="(data) => ({ 'row-not-expandable': isReserved(data.name) })"
      @row-expand="onRowExpand"
    >
      <template #empty>
        <span class="lab-muted">No accounts yet.</span>
      </template>
      <Column expander style="width: 2.5rem" />
      <template #expansion="{ data }">
        <div v-if="isReserved(data.name)" class="bu-expansion lab-muted">
          Reserved accounts have no business units.
        </div>
        <div v-else class="bu-expansion">
          <div class="bu-header">
            <span class="bu-title">Business Units</span>
            <Button icon="pi pi-plus" label="Add" size="small" text @click="openAddBU(data.name)" />
          </div>
          <div v-if="buLoading[data.name]" class="lab-muted">Loading…</div>
          <DataTable
            v-else
            :value="busByAccount[data.name] ?? []"
            size="small"
            class="bu-table"
          >
            <template #empty><span class="lab-muted">No business units yet.</span></template>
            <Column header="Name" class="bu-col-name">
              <template #body="{ data: bu }">
                <span :class="bu.isDefault ? 'bu-reserved' : ''">{{ bu.name }}</span>
                <Tag
                  v-if="bu.isDefault"
                  v-tooltip.top="DEFAULT_BU_TOOLTIP"
                  severity="secondary"
                  value="reserved"
                  class="bu-reserved-tag"
                />
              </template>
            </Column>
            <Column header="Context" class="bu-col-context">
              <template #body="{ data: bu }"><span class="bu-context">{{ bu.context }}</span></template>
            </Column>
            <Column header="Visible" class="bu-col-visible">
              <template #body="{ data: bu }">
                <Button
                  :icon="bu.visible ? 'pi pi-eye' : 'pi pi-eye-slash'"
                  :severity="bu.visible ? 'success' : 'secondary'"
                  size="small"
                  text
                  :aria-label="bu.visible ? 'Visible — click to hide' : 'Hidden — click to show'"
                  @click="toggleBUVisible(data.name, bu)"
                />
              </template>
            </Column>
            <Column header="Registered" class="bu-col-registered">
              <template #body="{ data: bu }">{{ formatDate(bu.createdAt) }}</template>
            </Column>
          </DataTable>
        </div>
      </template>
      <Column header="Name">
        <template #body="{ data }">
          <span class="name-cell">
            {{ data.name }}
            <Tag
              v-if="isReserved(data.name)"
              severity="secondary"
              value="reserved"
              icon="pi pi-lock"
              title="Reserved for platform/system use — never mintable as a tenant name (BR-AC06)"
            />
          </span>
        </template>
      </Column>
      <Column header="Status">
        <template #body="{ data }">
          <Tag :severity="data.status === 'active' ? 'success' : 'danger'" :value="data.status" />
        </template>
      </Column>
      <Column header="Public Key">
        <template #body="{ data }">
          <code class="pubkey">{{ data.publicKey.slice(0, 12) }}…</code>
        </template>
      </Column>
      <Column
        v-for="(dim, key) in USAGE_DIMS"
        :key="key"
        :header="dim.label"
      >
        <template #body="{ data }">
          <span :class="usageClass(data.name, key)" class="streams-usage">
            {{ usageLabel(data.name, key) }}
          </span>
        </template>
      </Column>
      <Column header="Created At">
        <template #body="{ data }">{{ formatDate(data.createdAt) }}</template>
      </Column>
      <Column header="" style="width: 2.5rem">
        <template #body="{ data }">
          <Button
            icon="pi pi-ellipsis-v"
            text
            size="small"
            aria-label="Account actions"
            @click.stop="openRowMenu($event, data)"
          />
        </template>
      </Column>
    </DataTable>

    <Menu ref="rowMenu" :model="rowMenuItems" popup />

    <Dialog v-model:visible="createOpen" header="Create Account" modal :style="{ width: '28rem' }">
      <div class="form-field">
        <label for="acc-name">Name</label>
        <InputText id="acc-name" v-model="form.name" placeholder="e.g. initech" autofocus />
      </div>
      <div class="form-grid">
        <div class="form-field">
          <label for="acc-mem">Max Memory (bytes)</label>
          <InputNumber id="acc-mem" v-model="form.jsMaxMem" :use-grouping="false" />
        </div>
        <div class="form-field">
          <label for="acc-file">Max Disk (bytes)</label>
          <InputNumber id="acc-file" v-model="form.jsMaxFile" :use-grouping="false" />
        </div>
        <div class="form-field">
          <label for="acc-streams">Max Streams</label>
          <InputNumber id="acc-streams" v-model="form.jsMaxStreams" :use-grouping="false" />
        </div>
        <div class="form-field">
          <label for="acc-consumers">Max Consumers</label>
          <InputNumber id="acc-consumers" v-model="form.jsMaxConsumers" :use-grouping="false" />
        </div>
      </div>
      <p v-if="createError" class="error-text">{{ createError }}</p>
      <template #footer>
        <Button label="Cancel" text @click="createOpen = false" />
        <Button label="Create" :loading="creating" :disabled="!form.name" @click="submitCreate" />
      </template>
    </Dialog>

    <Dialog v-model:visible="credsOpen" header="Account created" modal :style="{ width: '34rem' }">
      <p class="lab-muted">
        <strong>{{ mintedName }}</strong>'s <code>.creds</code> file — shown once, not stored anywhere
        retrievable after this dialog closes (only accounts-service's Postgres row and the shared
        creds volume keep it from here on).
      </p>
      <Textarea :model-value="mintedCreds" readonly rows="12" class="creds-text" />
      <template #footer>
        <Button label="Close" @click="credsOpen = false" />
      </template>
    </Dialog>

    <!-- Phase 22b: Add business unit dialog — Name (BR-AC26 display label) and
         Context (the immutable {context} slug), the latter auto-derived from
         Name but editable up to the point of submit. -->
    <Dialog v-model:visible="addBUOpen" :header="`Add Business Unit — ${addBUAccount}`" modal :style="{ width: '28rem' }">
      <div class="form-field">
        <label for="bu-name">Name</label>
        <InputText id="bu-name" v-model="addBUName" placeholder="e.g. Pacific Fleet" autofocus @input="onAddBUNameInput" />
      </div>
      <div class="form-field">
        <label for="bu-context">Context</label>
        <InputText
          id="bu-context"
          v-model="addBUContext"
          placeholder="e.g. acme-pacific-fleet"
          class="bu-context-input"
          :invalid="!!addBUContextError"
          @input="onAddBUContextInput"
        />
      </div>
      <p v-if="addBUContextError" class="error-text">{{ addBUContextError }}</p>
      <p class="lab-muted" style="font-size: 0.8rem; margin: 0">
        The subject token every lookup and query will use — lowercase letters, digits and hyphens only.
        <strong>Cannot be changed once created.</strong> Name can be edited any time.
      </p>
      <p v-if="addBUError" class="error-text">{{ addBUError }}</p>
      <template #footer>
        <Button label="Cancel" text @click="addBUOpen = false" />
        <Button
          label="Register"
          :loading="addBUSaving"
          :disabled="!addBUName || !addBUContext || !!addBUContextError"
          @click="submitAddBU"
        />
      </template>
    </Dialog>

    <!-- Phase 22/22b: Confirm hiding the default BU after the first real BU is added -->
    <Dialog v-model:visible="hideDefaultBUOpen" header="Hide default placeholder?" modal :style="{ width: '26rem' }">
      <p>
        You've added the first real business unit for <strong>{{ hideDefaultBUAccount }}</strong>.
        Would you like to hide the <code>{{ hideDefaultBUContext }}</code> placeholder from the context selector?
      </p>
      <p class="lab-muted" style="font-size: 0.8rem; margin: 0">
        You can always show it again from the Business Units table.
      </p>
      <template #footer>
        <Button label="Keep visible" text @click="hideDefaultBUOpen = false" />
        <Button label="Hide it" severity="secondary" :loading="hideDefaultBUSaving" @click="hideDefaultBU" />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="editOpen"
      :header="editAccount ? `Edit Limits — ${editAccount.name}` : 'Edit Limits'"
      modal
      :style="{ width: '28rem' }"
    >
      <div class="form-grid">
        <div class="form-field">
          <label for="edit-mem">Max Memory (bytes)</label>
          <InputNumber id="edit-mem" v-model="editForm.jsMaxMem" :use-grouping="false" />
        </div>
        <div class="form-field">
          <label for="edit-file">Max Disk (bytes)</label>
          <InputNumber id="edit-file" v-model="editForm.jsMaxFile" :use-grouping="false" />
        </div>
        <div class="form-field">
          <label for="edit-streams">Max Streams</label>
          <InputNumber id="edit-streams" v-model="editForm.jsMaxStreams" :use-grouping="false" />
        </div>
        <div class="form-field">
          <label for="edit-consumers">Max Consumers</label>
          <InputNumber id="edit-consumers" v-model="editForm.jsMaxConsumers" :use-grouping="false" />
        </div>
      </div>
      <p v-if="editError" class="error-text">{{ editError }}</p>
      <template #footer>
        <Button label="Cancel" text @click="editOpen = false" />
        <Button label="Update" :loading="editSaving" @click="submitEdit" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.accounts-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.description {
  margin: 0;
  font-size: 0.85rem;
}
.error-text {
  margin: 0;
  color: var(--p-red-400, #f87171);
  font-size: 0.85rem;
}
.pubkey {
  font-size: 0.8rem;
}
.name-cell {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}
.form-field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  margin-bottom: 0.75rem;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 0.75rem;
}
.creds-text {
  width: 100%;
  font-family: monospace;
  font-size: 0.75rem;
}
.streams-usage {
  font-variant-numeric: tabular-nums;
  font-size: 0.85rem;
}
.usage-ok {
  color: var(--p-green-500, #22c55e);
}
.usage-warn {
  color: var(--p-amber-400, #fbbf24);
  font-weight: 600;
}
.usage-crit {
  color: var(--p-red-400, #f87171);
  font-weight: 600;
}
.usage-na {
  color: var(--p-surface-400, #94a3b8);
}
:global(tr.row-not-expandable .p-datatable-row-toggle-button) {
  visibility: hidden;
  pointer-events: none;
}
.bu-expansion {
  padding: 0.5rem 0.5rem 0.75rem 2.75rem;
  position: relative;
  /* Left of pin line (0→1.1rem): same as account row; right: nested-zone background */
  background: linear-gradient(to right, var(--lab-bg) 1.1rem, var(--lab-nested-bg) 1.1rem);
}
.bu-expansion::before {
  content: '';
  position: absolute;
  left: 1.1rem;
  top: 0;
  bottom: 0.25rem;
  width: 2px;
  background: rgba(0, 111, 255, 0.35);
  border-radius: 1px;
}
.bu-table :deep(.p-datatable-tbody > tr) {
  background-color: var(--lab-nested-bg);
}
.bu-table :deep(.p-datatable-tbody > tr:hover) {
  background-color: var(--lab-nested-bg-hover);
}
.bu-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 0.4rem;
}
.bu-title {
  font-size: 0.8rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--lab-accent);
}
.bu-table {
  /* Was capped at 36rem — cramped once Context joined Name/Visible/Registered
     (Phase 22b), squeezing four columns into a width sized for three. Full
     width of the expansion row, with a ceiling so it doesn't stretch
     edge-to-edge on very wide screens. */
  width: 100%;
  max-width: 64rem;
  --p-datatable-header-cell-background: color-mix(in srgb, var(--lab-nested-bg) 95%, var(--lab-accent) 5%);
}
.bu-table :deep(table) {
  table-layout: fixed;
}
.bu-table :deep(.p-datatable-thead > tr > th),
.bu-table :deep(.p-datatable-tbody > tr > td) {
  padding-block: 0.55rem;
  padding-inline: 0.85rem;
}
/* :deep() required — Column's `class` prop lands on the th/td PrimeVue
   renders internally, which never receive this SFC's scoped data-v attribute. */
.bu-table :deep(.bu-col-name) {
  width: 30%;
}
.bu-table :deep(.bu-col-context) {
  width: 32%;
}
.bu-table :deep(.bu-col-visible) {
  width: 12%;
  text-align: center;
}
.bu-table :deep(.bu-col-registered) {
  width: 26%;
}
.bu-reserved {
  font-style: italic;
  color: var(--p-surface-400, #94a3b8);
}
.bu-reserved-tag {
  margin-left: 0.4rem;
  font-size: 0.7rem;
}
.bu-context {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 0.75rem;
  color: var(--p-text-muted-color);
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}
.bu-context-input {
  font-family: var(--font-mono, ui-monospace, monospace);
}
</style>
