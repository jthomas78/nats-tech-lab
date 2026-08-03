<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useToast } from 'primevue/usetoast'
import { onMounted, reactive, ref } from 'vue'

import { createAccount, getAccountsUsage, listAccounts, reactivateAccount, suspendAccount, updateAccountLimits } from '../api'
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

function streamsLabel(name) {
  const u = usage.value[name]
  if (!u) return '–'
  return `${u.streams.used} / ${u.streams.limit}`
}

function streamsClass(name) {
  const u = usage.value[name]
  if (!u) return 'usage-na'
  const ratio = u.streams.used / u.streams.limit
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

    <DataTable :value="accounts" size="small" paginator :rows="10" class="accounts-table">
      <template #empty>
        <span class="lab-muted">No accounts yet.</span>
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
      <Column header="JetStream Limits">
        <template #body="{ data }">
          {{ formatBytes(data.jsMaxMem) }} mem · {{ formatBytes(data.jsMaxFile) }} disk ·
          {{ data.jsMaxStreams }} streams · {{ data.jsMaxConsumers }} consumers
        </template>
      </Column>
      <Column header="Streams">
        <template #body="{ data }">
          <span :class="streamsClass(data.name)" class="streams-usage">
            {{ streamsLabel(data.name) }}
          </span>
        </template>
      </Column>
      <Column header="Created At">
        <template #body="{ data }">{{ formatDate(data.createdAt) }}</template>
      </Column>
      <Column header="">
        <template #body="{ data }">
          <div class="row-actions">
            <Button
              label="Edit Limits"
              icon="pi pi-sliders-h"
              text
              size="small"
              @click="openEdit(data)"
            />
            <Button
              v-if="data.status === 'active' && !isReserved(data.name)"
              label="Suspend"
              severity="danger"
              text
              size="small"
              @click="suspend(data.name)"
            />
            <Button
              v-else-if="data.status === 'suspended'"
              label="Reactivate"
              severity="success"
              text
              size="small"
              @click="reactivate(data.name)"
            />
          </div>
        </template>
      </Column>
    </DataTable>

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
.row-actions {
  display: flex;
  align-items: center;
  gap: 0.125rem;
  flex-wrap: nowrap;
}
</style>
