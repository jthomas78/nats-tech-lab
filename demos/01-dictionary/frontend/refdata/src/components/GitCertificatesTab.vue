<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import MultiSelect from 'primevue/multiselect'
import Tag from 'primevue/tag'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'

import {
  approveComplianceDocument,
  downloadComplianceDocumentFile,
  listGitCertificates,
  listItems,
  registerGitCertificateWithFile,
  rejectComplianceDocument,
  setGitCertificateExpiry,
  updateGitCertificate,
} from '../api'
import { codeFor, labelFor } from '../itemFields'
import { carriesGitCover, gitCertificateActions, gitDisplayStatus } from '../gitCertificateUi'

const props = defineProps({
  context: { type: String, required: true },
  organizationId: { type: String, required: true },
})
const emit = defineEmits(['changed'])
const toast = useToast()

const certificates = ref([])
const coverByGoodsType = ref({})
const goodsOptions = ref([])
const loading = ref(false)
const busyId = ref('')
const error = ref('')

async function load() {
  if (!props.context || !props.organizationId) return
  loading.value = true
  error.value = ''
  try {
    const [history, goods] = await Promise.all([
      listGitCertificates(props.context, props.organizationId),
      listItems(props.context, 'goods-type', { locale: 'en' }),
    ])
    certificates.value = history.documents ?? []
    coverByGoodsType.value = history.coverByGoodsType ?? {}
    goodsOptions.value = (goods.items ?? []).map((entry) => ({
      code: codeFor(entry), label: labelFor(entry) || codeFor(entry),
    }))
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(() => [props.context, props.organizationId], load, { immediate: true })

const coverEntries = computed(() => Object.entries(coverByGoodsType.value).sort(([a], [b]) => a.localeCompare(b)))
const goodsLabels = computed(() => Object.fromEntries(goodsOptions.value.map((item) => [item.code, item.label])))

const fileInput = ref(null)
const pendingFile = ref(null)
const registerOpen = ref(false)
const registerSaving = ref(false)
// No document name on the form: Phase 40 takes it from the dropped file, and
// it is never editable — the name identifies the bytes.
const registerForm = reactive({ goodsTypes: [], coverageRand: null, expiryDate: '' })

function chooseRegistrationFile() {
  if (!fileInput.value) return
  fileInput.value.value = ''
  fileInput.value.click()
}

function acceptRegistrationFile(file) {
  error.value = ''
  if (!file) return
  if (file.size <= 0) {
    error.value = `${file.name} is empty.`
    return
  }
  if (file.size > 10 * 1024 * 1024) {
    error.value = `${file.name} is larger than the 10 MB limit.`
    return
  }
  // BR-TP74: a name already used by this organization is refused server-side,
  // but catching it here saves the operator a round trip through the dialog.
  if (certificates.value.some((item) => (item.documentName || '') === file.name)) {
    error.value = `${file.name} has already been registered for this organization. Rename the file or drop a different one.`
    return
  }
  pendingFile.value = file
  Object.assign(registerForm, { goodsTypes: [], coverageRand: null, expiryDate: '' })
  registerOpen.value = true
}

function onFileChosen(event) {
  acceptRegistrationFile(event.target.files?.[0])
}

function onDrop(event) {
  acceptRegistrationFile(event.dataTransfer?.files?.[0])
}

function unixFromDate(value) {
  if (!value) return null
  const time = new Date(`${value}T23:59:59`).getTime()
  return Number.isFinite(time) ? Math.floor(time / 1000) : null
}

function dateFromUnix(value) {
  if (!value) return ''
  const date = new Date(value * 1000)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 10)
}

async function registerCertificate() {
  if (!pendingFile.value || !registerForm.goodsTypes.length) return
  registerSaving.value = true
  error.value = ''
  try {
    const doc = await registerGitCertificateWithFile(props.context, props.organizationId, {
      goodsTypes: [...registerForm.goodsTypes],
      coverageCents: registerForm.coverageRand == null ? null : Math.round(registerForm.coverageRand * 100),
      expiresAt: unixFromDate(registerForm.expiryDate),
    }, pendingFile.value)
    // The upload response is the FOR_REVIEW row. Insert it directly rather
    // than polling or re-reading: registration writes its row synchronously.
    certificates.value = [doc, ...certificates.value.filter((item) => item.id !== doc.id)]
    registerOpen.value = false
    pendingFile.value = null
    emit('changed')
    toast.add({ severity: 'success', summary: 'GIT certificate registered', detail: doc.documentName, life: 3000 })
  } catch (e) {
    error.value = e.message
  } finally {
    registerSaving.value = false
  }
}

const editing = ref(null)
const editSaving = ref(false)
const editError = ref('')
const editForm = reactive({
  goodsTypes: [], coverageRand: null, expiryDate: '',
  insurerName: '', insuranceContactName: '', insuranceContactNumber: '',
})

function openEdit(doc) {
  editing.value = doc
  Object.assign(editForm, {
    goodsTypes: [...(doc.goodsTypes ?? [])],
    coverageRand: doc.coverageCents == null ? null : doc.coverageCents / 100,
    expiryDate: dateFromUnix(doc.expiresAt),
    insurerName: doc.insurerName || '',
    insuranceContactName: doc.insuranceContactName || '',
    insuranceContactNumber: doc.insuranceContactNumber || '',
  })
  editError.value = ''
}

function cancelEdit() {
  editing.value = null
  editError.value = ''
}

async function saveEdit() {
  const doc = editing.value
  if (!doc) return
  editSaving.value = true
  editError.value = ''
  try {
    const expiresAt = unixFromDate(editForm.expiryDate)
    const coverageCents = editForm.coverageRand == null ? null : Math.round(editForm.coverageRand * 100)
    const detailsChanged = doc.status !== 'SUPERSEDED' && (
      JSON.stringify(editForm.goodsTypes) !== JSON.stringify(doc.goodsTypes ?? []) ||
      coverageCents !== doc.coverageCents ||
      editForm.insurerName !== (doc.insurerName || '') ||
      editForm.insuranceContactName !== (doc.insuranceContactName || '') ||
      editForm.insuranceContactNumber !== (doc.insuranceContactNumber || '')
    )
    if (detailsChanged) {
      await updateGitCertificate(props.context, props.organizationId, doc.id, {
        goodsTypes: [...editForm.goodsTypes],
        coverageCents,
        insurerName: editForm.insurerName,
        insuranceContactName: editForm.insuranceContactName,
        insuranceContactNumber: editForm.insuranceContactNumber,
      })
    }
    if (expiresAt !== doc.expiresAt) {
      await setGitCertificateExpiry(props.context, props.organizationId, doc.id, expiresAt)
    }
    await load()
    editing.value = null
    emit('changed')
    toast.add({ severity: 'success', summary: 'Certificate saved', life: 3000 })
  } catch (e) {
    editError.value = e.message
  } finally {
    editSaving.value = false
  }
}

const approveOpen = ref(false)
const approveDoc = ref(null)
const approveSaving = ref(false)
const approveError = ref('')
const approveForm = reactive({ insurerName: '', insuranceContactName: '', insuranceContactNumber: '' })

function openApprove(doc) {
  approveDoc.value = doc
  Object.assign(approveForm, {
    insurerName: doc.insurerName || '',
    insuranceContactName: doc.insuranceContactName || '',
    insuranceContactNumber: doc.insuranceContactNumber || '',
  })
  approveError.value = ''
  approveOpen.value = true
}

async function confirmApprove() {
  if (!approveDoc.value) return
  approveSaving.value = true
  approveError.value = ''
  try {
    await approveComplianceDocument(props.context, props.organizationId, approveDoc.value.id, { ...approveForm })
    approveOpen.value = false
    await load()
    emit('changed')
  } catch (e) {
    approveError.value = e.message
  } finally {
    approveSaving.value = false
  }
}

async function runRowAction(action, doc) {
  if (action === 'approve') {
    openApprove(doc)
    return
  }
  busyId.value = doc.id
  error.value = ''
  try {
    await rejectComplianceDocument(props.context, props.organizationId, doc.id)
    await load()
    emit('changed')
  } catch (e) {
    error.value = e.message
  } finally {
    busyId.value = ''
  }
}

async function download(doc) {
  busyId.value = doc.id
  try {
    const blob = await downloadComplianceDocumentFile(props.context, props.organizationId, doc.id)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = doc.documentName
    link.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    error.value = e.message
  } finally {
    busyId.value = ''
  }
}

function formatMoney(cents) {
  if (cents == null) return '—'
  return new Intl.NumberFormat('en-ZA', { style: 'currency', currency: 'ZAR' }).format(cents / 100)
}

function formatDate(seconds) {
  if (!seconds) return '—'
  return new Date(seconds * 1000).toLocaleDateString([], { dateStyle: 'medium' })
}

function formatInstant(value) {
  if (!value) return '—'
  return new Date(value).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
}

function statusSeverity(status) {
  if (status === 'APPROVED') return 'success'
  if (status === 'FOR_REVIEW') return 'info'
  if (status === 'EXPIRED') return 'warn'
  if (status === 'REJECTED') return 'danger'
  return 'secondary'
}

function statusLabel(status) {
  return ({ FOR_REVIEW: 'For review', SUPERSEDED: 'Superseded', APPROVED: 'Approved', REJECTED: 'Rejected', EXPIRED: 'Expired' })[status] || status
}

function rowClass(doc) {
  return carriesGitCover(doc) ? 'git-cover-row' : ''
}
</script>

<template>
  <div class="git-certificates">
    <template v-if="!editing">
      <input ref="fileInput" type="file" accept="application/pdf,image/*" class="hidden-file-input" @change="onFileChosen">
      <button class="git-drop-zone" type="button" @click="chooseRegistrationFile" @dragover.prevent @drop.prevent="onDrop">
        <i class="pi pi-upload" />
        <span class="drop-title">Drop a GIT certificate here to register it</span>
        <span class="drop-copy">PDF or image, up to 10 MB. Registration is always open; cover changes only on approval.</span>
      </button>

      <p v-if="error" class="git-error"><i class="pi pi-exclamation-triangle" /> {{ error }}</p>

      <div class="git-table-head">
        <div>
          <strong>GIT Certificates ({{ certificates.length }})</strong>
          <span>Newest registration first · every certificate retained</span>
        </div>
        <Button icon="pi pi-refresh" text rounded aria-label="Refresh GIT certificates" :loading="loading" @click="load" />
      </div>
      <div v-if="coverEntries.length" class="reported-cover">
        <strong>Reported cover:</strong>
        <span v-for="([code, cents], index) in coverEntries" :key="code">
          {{ goodsLabels[code] || code }} {{ formatMoney(cents) }}<template v-if="index < coverEntries.length - 1"> · </template>
        </span>
      </div>

      <DataTable :value="certificates" :row-class="rowClass" size="small" class="sub-table git-table" :loading="loading">
        <template #empty><span class="lab-muted">No GIT certificates registered yet.</span></template>
        <Column header="Status" style="width: 9rem">
          <template #body="{ data: doc }">
            <div class="status-cell">
              <Tag :severity="statusSeverity(gitDisplayStatus(doc))" :value="statusLabel(gitDisplayStatus(doc))" />
              <span v-if="carriesGitCover(doc)" class="cover-marker">Carries cover</span>
            </div>
          </template>
        </Column>
        <Column header="Document Name" style="min-width: 13rem">
          <template #body="{ data: doc }">
            <Button :label="doc.documentName" text size="small" class="certificate-link" @click="openEdit(doc)" />
            <span class="cell-sub">registered {{ formatInstant(doc.createdAt) }}</span>
          </template>
        </Column>
        <Column header="Goods types" style="min-width: 14rem">
          <template #body="{ data: doc }">
            <div class="goods-tags"><Tag v-for="code in doc.goodsTypes" :key="code" severity="secondary" :value="goodsLabels[code] || code" /></div>
          </template>
        </Column>
        <Column header="Cover"><template #body="{ data: doc }">{{ formatMoney(doc.coverageCents) }}</template></Column>
        <Column header="Expiry date"><template #body="{ data: doc }">{{ formatDate(doc.expiresAt) }}</template></Column>
        <Column field="insurerName" header="Insurer"><template #body="{ data: doc }">{{ doc.insurerName || '—' }}</template></Column>
        <Column header="Last updated"><template #body="{ data: doc }">{{ formatInstant(doc.updatedAt) }}</template></Column>
        <Column header="" style="width: 16rem">
          <template #body="{ data: doc }">
            <div class="row-actions">
              <Button v-if="gitCertificateActions(doc).edit" label="Edit" text size="small" @click="openEdit(doc)" />
              <Button v-else label="View" text size="small" @click="openEdit(doc)" />
              <Button v-if="gitCertificateActions(doc).approve" label="Approve" text size="small" @click="runRowAction('approve', doc)" />
              <Button v-if="gitCertificateActions(doc).reject" label="Reject" text size="small" severity="danger" :loading="busyId === doc.id" @click="runRowAction('reject', doc)" />
            </div>
          </template>
        </Column>
      </DataTable>
    </template>

    <template v-else>
      <div class="edit-head">
        <div>
          <Button label="GIT Certificates" icon="pi pi-angle-left" text size="small" @click="cancelEdit" />
          <strong>{{ editing.documentName }}</strong>
          <Tag :severity="statusSeverity(gitDisplayStatus(editing))" :value="statusLabel(gitDisplayStatus(editing))" />
        </div>
        <div class="edit-actions">
          <Button label="Cancel" outlined size="small" :disabled="editSaving" @click="cancelEdit" />
          <Button label="Save certificate" size="small" :loading="editSaving" :disabled="editing.status !== 'SUPERSEDED' && !editForm.goodsTypes.length" @click="saveEdit" />
        </div>
      </div>

      <div v-if="editing.status === 'SUPERSEDED'" class="lock-note">
        <i class="pi pi-lock" /> This certificate is superseded. Its details are read-only; only historical expiry correction remains available.
      </div>
      <p v-if="editError" class="git-error">{{ editError }}</p>

      <div class="edit-grid">
        <section class="edit-section">
          <h4>Cover</h4>
          <div class="form-field full-field">
            <label for="git-document-name">Document Name</label>
            <!-- Read-only in every state (Phase 40): the name is the dropped
                 file's own, so changing it here would describe bytes that
                 exist under a different name. -->
            <InputText id="git-document-name" :model-value="editing.documentName" readonly disabled />
          </div>
          <div class="form-field full-field">
            <label for="git-goods">Goods types</label>
            <MultiSelect id="git-goods" v-model="editForm.goodsTypes" :options="goodsOptions" option-label="label" option-value="code" display="chip" :disabled="editing.status === 'SUPERSEDED'" />
          </div>
          <div class="form-field">
            <label for="git-cover">Cover amount (ZAR)</label>
            <InputNumber id="git-cover" v-model="editForm.coverageRand" :min="0" :max-fraction-digits="2" :disabled="editing.status === 'SUPERSEDED'" />
          </div>
          <div class="form-field">
            <label for="git-expiry">Expiry date</label>
            <input id="git-expiry" v-model="editForm.expiryDate" type="date" class="native-input">
          </div>
        </section>
        <section class="edit-section">
          <h4>Insurance</h4>
          <div class="form-field">
            <label for="git-insurer">Insurance company</label>
            <InputText id="git-insurer" v-model="editForm.insurerName" :disabled="editing.status === 'SUPERSEDED'" />
          </div>
          <div class="form-field">
            <label for="git-contact">Contact person</label>
            <InputText id="git-contact" v-model="editForm.insuranceContactName" :disabled="editing.status === 'SUPERSEDED'" />
          </div>
          <div class="form-field">
            <label for="git-number">Contact number</label>
            <InputText id="git-number" v-model="editForm.insuranceContactNumber" :disabled="editing.status === 'SUPERSEDED'" />
          </div>
          <div class="file-record">
            <div><span>File</span><strong>{{ editing.documentName }}</strong></div>
            <Button label="Download" icon="pi pi-download" text size="small" :loading="busyId === editing.id" @click="download(editing)" />
          </div>
        </section>
      </div>
    </template>

    <Dialog v-model:visible="registerOpen" header="Register GIT certificate" modal :style="{ width: '36rem' }">
      <p class="lab-muted dialog-intro">{{ pendingFile?.name }} · choose the goods types this policy covers before the upload starts.</p>
      <div class="form-field"><label for="git-reg-goods">Goods types</label><MultiSelect id="git-reg-goods" v-model="registerForm.goodsTypes" :options="goodsOptions" option-label="label" option-value="code" display="chip" /></div>
      <div class="form-grid">
        <div class="form-field"><label for="git-reg-cover">Cover amount (ZAR)</label><InputNumber id="git-reg-cover" v-model="registerForm.coverageRand" :min="0" :max-fraction-digits="2" /></div>
        <div class="form-field"><label for="git-reg-expiry">Expiry date</label><input id="git-reg-expiry" v-model="registerForm.expiryDate" type="date" class="native-input"></div>
      </div>
      <template #footer>
        <Button label="Cancel" text @click="registerOpen = false" />
        <Button label="Register & upload" :loading="registerSaving" :disabled="!registerForm.goodsTypes.length" @click="registerCertificate" />
      </template>
    </Dialog>

    <Dialog v-model:visible="approveOpen" header="Approve GIT certificate" modal :style="{ width: '32rem' }">
      <p class="lab-muted dialog-intro">Approval makes this certificate carry cover and supersedes every earlier certificate.</p>
      <div class="form-field"><label for="git-approve-insurer">Insurance company</label><InputText id="git-approve-insurer" v-model="approveForm.insurerName" /></div>
      <div class="form-grid">
        <div class="form-field"><label for="git-approve-contact">Contact person</label><InputText id="git-approve-contact" v-model="approveForm.insuranceContactName" /></div>
        <div class="form-field"><label for="git-approve-number">Contact number</label><InputText id="git-approve-number" v-model="approveForm.insuranceContactNumber" /></div>
      </div>
      <p v-if="approveError" class="git-error">{{ approveError }}</p>
      <template #footer>
        <Button label="Cancel" text @click="approveOpen = false" />
        <Button label="Approve" :loading="approveSaving" :disabled="!approveForm.insurerName || !approveForm.insuranceContactName || !approveForm.insuranceContactNumber" @click="confirmApprove" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.git-certificates { display: flex; flex-direction: column; gap: 0.75rem; }
.hidden-file-input { display: none; }
.git-drop-zone { min-height: 7rem; border: 1px dashed var(--lab-border, #4a515b); border-radius: 4px; background: transparent; color: var(--lab-text, #dee0e3); display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 0.35rem; cursor: pointer; font: inherit; }
.git-drop-zone:hover, .git-drop-zone:focus-visible { border-color: var(--lab-accent); outline: none; background: color-mix(in srgb, var(--lab-accent) 5%, transparent); }
.git-drop-zone > i, .drop-title { color: var(--lab-accent); }
.git-drop-zone > i { font-size: 1.35rem; }
.drop-title { font-weight: 600; }
.drop-copy, .cell-sub { color: var(--lab-text-muted, #b7bcc2); font-size: 0.78rem; }
.git-table-head, .edit-head, .git-table-head > div, .edit-head > div, .row-actions, .goods-tags, .status-cell, .edit-actions { display: flex; align-items: center; gap: 0.4rem; }
.git-table-head, .edit-head { justify-content: space-between; }
.git-table-head > div { flex-direction: column; align-items: flex-start; gap: 0.1rem; }
.git-table-head span { color: var(--lab-text-muted, #b7bcc2); font-size: 0.75rem; }
.reported-cover { font-size: 0.78rem; color: var(--lab-text-muted, #b7bcc2); }
.cover-marker { color: var(--p-green-400, #27c07f); font-size: 0.7rem; font-weight: 600; white-space: nowrap; }
.status-cell { align-items: flex-start; flex-direction: column; }
.goods-tags, .row-actions { flex-wrap: wrap; }
.certificate-link { padding: 0; }
.certificate-link :deep(.p-button-label) { max-width: 13rem; overflow: hidden; text-overflow: ellipsis; }
.cell-sub { display: block; margin-top: 0.15rem; }
.git-table :deep(.git-cover-row > td) { background: color-mix(in srgb, var(--p-green-500, #27c07f) 8%, transparent); }
.git-table :deep(.git-cover-row > td:first-child) { box-shadow: inset 3px 0 var(--p-green-500, #27c07f); }
.git-error, .lock-note { margin: 0; padding: 0.55rem 0.65rem; border-radius: 4px; font-size: 0.8rem; }
.git-error { color: var(--p-red-400, #f87171); border: 1px solid var(--p-red-500, #f87171); }
.lock-note { color: var(--lab-text-muted, #b7bcc2); border: 1px solid var(--lab-border, #4a515b); }
.edit-grid { display: grid; grid-template-columns: minmax(0, 1.4fr) minmax(18rem, 1fr); gap: 1rem; }
.edit-section { border: 1px solid var(--lab-border, #4a515b); border-radius: 4px; padding: 0.85rem; display: grid; grid-template-columns: 1fr 1fr; gap: 0 0.75rem; align-content: start; }
.edit-section h4 { grid-column: 1 / -1; margin: 0 0 0.75rem; color: var(--lab-accent); text-transform: uppercase; font-size: 0.78rem; }
.full-field, .file-record { grid-column: 1 / -1; }
.form-field { display: flex; flex-direction: column; gap: 0.25rem; margin-bottom: 0.75rem; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 0.75rem; }
.native-input { min-height: 2.5rem; border: 1px solid var(--p-form-field-border-color, var(--lab-border)); border-radius: var(--p-form-field-border-radius, 4px); background: var(--p-form-field-background, transparent); color: var(--p-form-field-color, var(--lab-text)); padding: 0.5rem 0.65rem; color-scheme: dark; }
.file-record { display: flex; justify-content: space-between; align-items: center; border-top: 1px solid var(--lab-border, #4a515b); padding-top: 0.7rem; }
.file-record > div { display: flex; flex-direction: column; gap: 0.2rem; }
.file-record span, .dialog-intro { font-size: 0.78rem; }
.dialog-intro { margin-top: 0; }
.sub-table { width: 100%; --p-datatable-header-cell-background: color-mix(in srgb, var(--lab-nested-bg) 95%, var(--lab-accent) 5%); }
@media (max-width: 1000px) { .edit-grid { grid-template-columns: 1fr; } }
</style>
